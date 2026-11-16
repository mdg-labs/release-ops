package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
)

var (
	// ErrInstallationNotFound is returned when GitHub does not know the installation, or it
	// belongs to another App.
	ErrInstallationNotFound = errors.New("github installation not found")
	// ErrAppRejected is returned when GitHub refuses the App's credentials or the request
	// (401, 403).
	ErrAppRejected = errors.New("github rejected the app credentials")
	// ErrAppMismatch is returned when GitHub answers a JWT for this instance's App id with a
	// different App.
	ErrAppMismatch = errors.New("github answered for another app than this instance's")
	// ErrRepositoriesRejected is returned when GitHub refuses an installation token because
	// a requested repository is not covered by the installation (422).
	ErrRepositoriesRejected = errors.New("github refused the repositories requested for the token")
	// ErrRequestFailed is returned for every other failure: GitHub unreachable, timed out, a
	// 5xx, an unexpected status or a response that is not usable. Nothing of GitHub's response
	// is carried in the error.
	ErrRequestFailed = errors.New("github request failed")
)

// Account is the user or organization an App is installed on.
type Account struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

// App is GET /app: the App the JWT authenticates as.
type App struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
}

// Installation is GET /app/installations/{id}.
type Installation struct {
	ID                  int64             `json:"id"`
	AppID               int64             `json:"app_id"`
	Account             Account           `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
	SuspendedAt         *time.Time        `json:"suspended_at"`
}

func (i Installation) valid() bool {
	return i.ID > 0 && i.AppID > 0 &&
		i.Account.ID > 0 && i.Account.Login != "" && (i.Account.Type == "User" || i.Account.Type == "Organization") &&
		(i.RepositorySelection == "all" || i.RepositorySelection == "selected")
}

// InstallationToken is an installation access token. The value is held in memory only; the
// type renders without it when printed or logged.
type InstallationToken struct {
	value     string
	ExpiresAt time.Time
}

// Value returns the token.
func (t InstallationToken) Value() string { return t.value }

// String omits the token.
func (t InstallationToken) String() string { return "github installation token" }

// GoString omits the token.
func (t InstallationToken) GoString() string { return t.String() }

// LogValue omits the token.
func (t InstallationToken) LogValue() slog.Value {
	return slog.GroupValue(slog.Time("expires_at", t.ExpiresAt))
}

// GetApp fetches the App that appJWT authenticates as. It answers ErrAppRejected when GitHub
// does not accept the JWT.
func (c *Client) GetApp(ctx context.Context, appJWT string) (*App, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/app", appJWT, nil)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAppRejected
	default:
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out App
	if err := json.Unmarshal(body, &out); err != nil || out.ID <= 0 {
		return nil, fmt.Errorf("%w: response is not an app", ErrRequestFailed)
	}
	return &out, nil
}

// GetInstallation fetches an installation of the App that appJWT authenticates.
func (c *Client) GetInstallation(ctx context.Context, appJWT string, installationID int64) (*Installation, error) {
	status, body, err := c.do(ctx, http.MethodGet, "/app/installations/"+strconv.FormatInt(installationID, 10), appJWT, nil)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrInstallationNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAppRejected
	default:
		return nil, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out Installation
	if err := json.Unmarshal(body, &out); err != nil || !out.valid() {
		return nil, fmt.Errorf("%w: response is not an installation", ErrRequestFailed)
	}
	return &out, nil
}

// CreateInstallationToken mints an installation access token. With repositories (names
// without the owner) the token covers only those; without, every repository of the
// installation.
func (c *Client) CreateInstallationToken(ctx context.Context, appJWT string, installationID int64, repositories []string) (InstallationToken, error) {
	var payload io.Reader
	if len(repositories) > 0 {
		raw, err := json.Marshal(map[string][]string{"repositories": repositories})
		if err != nil {
			return InstallationToken{}, ErrRequestFailed
		}
		payload = bytes.NewReader(raw)
	}
	status, body, err := c.do(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", appJWT, payload)
	if err != nil {
		return InstallationToken{}, err
	}
	switch status {
	case http.StatusCreated:
	case http.StatusNotFound:
		return InstallationToken{}, ErrInstallationNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return InstallationToken{}, ErrAppRejected
	case http.StatusUnprocessableEntity:
		return InstallationToken{}, ErrRepositoriesRejected
	default:
		return InstallationToken{}, fmt.Errorf("%w: status %d", ErrRequestFailed, status)
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Token == "" || out.ExpiresAt.IsZero() {
		return InstallationToken{}, fmt.Errorf("%w: response is not an installation token", ErrRequestFailed)
	}
	logging.RegisterFor(max(time.Until(out.ExpiresAt), 0), out.Token)
	return InstallationToken{value: out.Token, ExpiresAt: out.ExpiresAt}, nil
}

// do sends one authenticated request and returns the status and the bounded body. The client
// never follows a redirect, so the bearer credential goes to the configured host only.
func (c *Client) do(ctx context.Context, method, path, bearer string, payload io.Reader) (int, []byte, error) {
	status, _, body, err := c.sendTo(ctx, method, c.apiBase+path, bearer, payload, maxResponseBytes)
	return status, body, err
}

// sendTo sends one authenticated request to rawURL, which the caller has built from the
// configured API base, and returns the status, the headers and the body read through limit.
func (c *Client) sendTo(ctx context.Context, method, rawURL, bearer string, payload io.Reader, limit int64) (int, http.Header, []byte, error) {
	return c.sendWith(ctx, method, rawURL, bearer, payload, limit, nil)
}

// sendWith is sendTo with extra request headers, such as a conditional request's
// If-None-Match.
func (c *Client) sendWith(ctx context.Context, method, rawURL, bearer string, payload io.Reader, limit int64, extra http.Header) (int, http.Header, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, payload)
	if err != nil {
		return 0, nil, nil, ErrRequestFailed
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", "Bearer "+bearer)
	for name, values := range extra {
		req.Header[name] = values
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%w: request did not complete", ErrRequestFailed)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("%w: response not readable", ErrRequestFailed)
	}
	if int64(len(body)) > limit {
		return 0, nil, nil, fmt.Errorf("%w: response too large", ErrRequestFailed)
	}
	return resp.StatusCode, resp.Header, body, nil
}

// ownerAndName splits owner/repo; ok is false for any other shape.
func ownerAndName(full string) (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(full, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", false
	}
	return owner, name, true
}
