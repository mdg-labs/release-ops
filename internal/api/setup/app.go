package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// StateKey and StateExpiresKey are the scs session keys holding the state of the
	// manifest attempt in progress and when it ends (Unix seconds).
	StateKey        = "setupManifestState"
	StateExpiresKey = "setupManifestStateExpiresAt"

	// StateTTL is how long a manifest attempt lasts: GitHub's manifest code lives one hour.
	StateTTL = time.Hour

	ownerUser         = "user"
	ownerOrganization = "organization"

	stateBytes = 32
)

// AppStore stores the instance's GitHub App.
type AppStore interface {
	Create(ctx context.Context, app store.NewGitHubApp) error
}

// ManifestConverter exchanges a manifest code for the App GitHub created.
type ManifestConverter interface {
	ConvertManifest(ctx context.Context, code string) (*github.ManifestConversion, error)
}

type manifestRequest struct {
	Owner struct {
		Type  string `json:"type"`
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

type manifestResponse struct {
	URL      string   `json:"url"`
	Manifest Manifest `json:"manifest"`
}

// Manifest answers POST /setup/manifest behind RequireSession: it validates the owner and
// name, binds a fresh single-use state to the setup session, replacing any earlier attempt,
// and returns the registration URL and the manifest the page posts to GitHub.
func (h *Handlers) Manifest(w http.ResponseWriter, r *http.Request) {
	var req manifestRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeValidationError(w, "Invalid request body.")
		return
	}

	ownerType := strings.ToLower(strings.TrimSpace(req.Owner.Type))
	login := strings.TrimSpace(req.Owner.Login)
	switch ownerType {
	case ownerUser:
		login = ""
	case ownerOrganization:
		if !ValidLogin(login) {
			writeValidationError(w, "Enter the organization's GitHub name.")
			return
		}
	default:
		writeValidationError(w, "Owner type must be user or organization.")
		return
	}
	name, ok := NormalizeAppName(req.Name)
	if !ok {
		writeValidationError(w, "App name must be at most 34 characters without control characters.")
		return
	}

	state, err := newState()
	if err != nil {
		writeInternalError(w)
		return
	}
	ctx := r.Context()
	h.Sessions.Put(ctx, StateKey, state)
	h.Sessions.Put(ctx, StateExpiresKey, h.now().Add(StateTTL).Unix())

	writeJSON(w, http.StatusOK, manifestResponse{
		URL:      RegistrationURL(h.GitHubBaseURL, ownerType, login, state),
		Manifest: BuildManifest(name, h.AppURL, h.WebhookURL),
	})
}

// Error codes the callback hands the setup page in the "error" query parameter when it sends
// the browser back to /setup.
const (
	errInvalidState = "invalid_state"
	errRejected     = "conversion_rejected"
	errFailed       = "conversion_failed"
)

// Callback answers GET /setup/callback behind RequireSession, GitHub's redirect after the owner
// confirms the App, with the code and state in the query. The state must be the one bound to
// this setup session and is spent by the first request that presents it, so a replay is refused
// before GitHub is called. The code is then converted to the App, which is stored with its
// secrets encrypted, and the browser is redirected (302) to the App's installation page. A
// failure before the store leaves nothing behind and redirects to /setup with an error code.
func (h *Handlers) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()
	code := query.Get("code")
	if !h.takeState(r, query.Get("state")) || !github.ValidCode(code) {
		h.redirectToSetup(w, r, errInvalidState)
		return
	}

	conversion, err := h.Converter.ConvertManifest(ctx, code)
	switch {
	case errors.Is(err, github.ErrCodeRejected):
		h.redirectToSetup(w, r, errRejected)
		return
	case err != nil:
		h.redirectToSetup(w, r, errFailed)
		return
	}

	err = h.Apps.Create(ctx, store.NewGitHubApp{
		AppID:         conversion.ID,
		Slug:          conversion.Slug,
		ClientID:      conversion.ClientID,
		HTMLURL:       conversion.HTMLURL,
		OwnerLogin:    conversion.Owner.Login,
		OwnerType:     conversion.Owner.Type,
		WebhookActive: h.WebhookURL != "",
		PrivateKey:    conversion.PEM,
		ClientSecret:  conversion.ClientSecret,
		WebhookSecret: conversion.WebhookSecret,
	})
	switch {
	case errors.Is(err, store.ErrGitHubAppExists):
		writeNotFound(w)
		return
	case err != nil:
		h.redirectToSetup(w, r, errFailed)
		return
	}

	http.Redirect(w, r, InstallURL(h.GitHubBaseURL, conversion.Slug), http.StatusFound)
}

// Installed answers GET /setup/installed, GitHub's setup_url redirect after an installation. It
// is public and stays available after setup; for now it sends the browser to /login.
func (h *Handlers) Installed(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (h *Handlers) redirectToSetup(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/setup?error="+url.QueryEscape(code), http.StatusFound)
}

// takeState compares state with the attempt bound to the setup session. A match spends it;
// a mismatch leaves the attempt in place so a forged request cannot cancel the real one.
func (h *Handlers) takeState(r *http.Request, state string) bool {
	ctx := r.Context()
	stored := h.Sessions.GetString(ctx, StateKey)
	expires := h.Sessions.GetInt64(ctx, StateExpiresKey)
	if stored == "" || state == "" || expires == 0 || !h.now().Before(time.Unix(expires, 0)) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(stored)) != 1 {
		return false
	}
	h.Sessions.Remove(ctx, StateKey)
	h.Sessions.Remove(ctx, StateExpiresKey)
	return true
}

func newState() (string, error) {
	b := make([]byte, stateBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func writeValidationError(w http.ResponseWriter, message string) {
	auth.WriteError(w, "VALIDATION_ERROR", message, http.StatusBadRequest)
}
