package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
)

const (
	defaultTimeout = 15 * time.Second
	// maxResponseBytes bounds a conversion response; the real one is a few kilobytes.
	maxResponseBytes = 1 << 20
	apiVersion       = "2022-11-28"
	userAgent        = "release-ops"
	webGitHubURL     = "https://github.com"
	apiGitHubURL     = "https://api.github.com"
)

// ErrCodeRejected is returned when GitHub does not accept a manifest code: it is unknown,
// expired or already converted (404), or validation failed (422).
var ErrCodeRejected = errors.New("github rejected the manifest code")

// ErrConversionFailed is returned when the conversion could not complete: GitHub was
// unreachable, timed out, answered with another status, or sent a response that is not a
// usable App. Nothing of GitHub's response is carried in the error.
var ErrConversionFailed = errors.New("github manifest conversion failed")

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidCode reports whether code has the shape of a manifest code, so it is safe in a URL path.
func ValidCode(code string) bool {
	return codePattern.MatchString(code)
}

// APIBaseURL returns the REST API base URL for a GitHub web base URL: api.github.com for
// github.com, otherwise the /api/v3 path of an enterprise server.
func APIBaseURL(webBaseURL string) string {
	base := strings.TrimRight(webBaseURL, "/")
	if base == webGitHubURL {
		return apiGitHubURL
	}
	return base + "/api/v3"
}

// Owner is the account that owns a GitHub App.
type Owner struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// ManifestConversion is the response of POST /app-manifests/{code}/conversions: the App and
// its credentials. The credentials are held in memory only; the type renders without them
// when printed or logged.
type ManifestConversion struct {
	ID            int64  `json:"id"`
	Slug          string `json:"slug"`
	HTMLURL       string `json:"html_url"`
	Owner         Owner  `json:"owner"`
	ClientID      string `json:"client_id"`
	ClientSecret  string `json:"client_secret"`
	WebhookSecret string `json:"webhook_secret"`
	PEM           string `json:"pem"`
}

// String omits the credentials.
func (m ManifestConversion) String() string {
	return fmt.Sprintf("github app %d (%s)", m.ID, m.Slug)
}

// GoString omits the credentials.
func (m ManifestConversion) GoString() string { return m.String() }

// LogValue omits the credentials.
func (m ManifestConversion) LogValue() slog.Value {
	return slog.GroupValue(slog.Int64("id", m.ID), slog.String("slug", m.Slug))
}

func (m ManifestConversion) valid() bool {
	return m.ID > 0 &&
		m.Slug != "" && m.HTMLURL != "" &&
		m.Owner.Login != "" && (m.Owner.Type == "User" || m.Owner.Type == "Organization") &&
		m.ClientID != "" && m.ClientSecret != "" && m.WebhookSecret != "" && m.PEM != ""
}

// Client calls GitHub's REST API at one base URL.
type Client struct {
	apiBase string
	http    *http.Client
}

// NewClient returns a client for the API at apiBaseURL. A zero timeout means 15 seconds.
func NewClient(apiBaseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		apiBase: strings.TrimRight(apiBaseURL, "/"),
		http: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ConvertManifest exchanges the temporary code GitHub redirected the browser back with for
// the App it created. The call carries no credential: the code is the proof.
func (c *Client) ConvertManifest(ctx context.Context, code string) (*ManifestConversion, error) {
	logging.RegisterFor(oauthAttemptLogTTL, code)
	if !ValidCode(code) {
		return nil, ErrCodeRejected
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+"/app-manifests/"+url.PathEscape(code)+"/conversions", nil)
	if err != nil {
		return nil, ErrConversionFailed
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request did not complete", ErrConversionFailed)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusCreated:
	case http.StatusNotFound, http.StatusUnprocessableEntity:
		return nil, ErrCodeRejected
	default:
		return nil, fmt.Errorf("%w: status %d", ErrConversionFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: response not readable", ErrConversionFailed)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: response too large", ErrConversionFailed)
	}
	var out ManifestConversion
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%w: response is not a GitHub App", ErrConversionFailed)
	}
	logging.Register(out.ClientSecret, out.WebhookSecret, out.PEM)
	if !out.valid() {
		return nil, fmt.Errorf("%w: response is not a GitHub App", ErrConversionFailed)
	}
	return &out, nil
}
