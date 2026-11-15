package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/logging"
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
	github.BotStore
}

// InstallationStore records the instance's one installation.
type InstallationStore interface {
	LiveIDs(ctx context.Context) ([]int64, error)
	Record(ctx context.Context, in store.NewInstallation) (store.InstallationOutcome, error)
}

// ManifestConverter exchanges a manifest code for the App GitHub created, and reads the numeric
// id of the App's bot user once it is stored.
type ManifestConverter interface {
	ConvertManifest(ctx context.Context, code string) (*github.ManifestConversion, error)
	github.BotUserLooker
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
	errInvalidState   = "invalid_state"
	errRejected       = "conversion_rejected"
	errFailed         = "conversion_failed"
	errSessionExpired = "session_expired"
)

// Callback answers GET /setup/callback behind RequireSession, GitHub's redirect after the owner
// confirms the App, with the code and state in the query. The state must be the one bound to
// this setup session and is spent by the first request that presents it, so a replay is refused
// before GitHub is called. The code is then converted to the App, which is stored with its
// secrets encrypted, and the browser is redirected (302) to the App's installation page. A
// failure before the store leaves nothing behind and redirects to /setup with an error code;
// so does a missing or expired setup session (RequireCallbackSession). Once the App is stored
// its bot user's numeric id is read from GitHub; a lookup that fails is logged and neither
// undoes nor fails the App, and the server retries it in the background.
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

	if err := github.NewBotResolver(h.Converter, h.Apps).Ensure(ctx); err != nil {
		slog.Warn("setup callback: reading the App's bot user id failed; it is retried in the background", "err", err)
	}

	http.Redirect(w, r, InstallURL(h.GitHubBaseURL, conversion.Slug), http.StatusFound)
}

// InstallationFetcher confirms an installation with GitHub, authenticated as the App.
type InstallationFetcher interface {
	Installation(ctx context.Context, installationID int64) (*github.Installation, error)
}

var installationIDPattern = regexp.MustCompile(`^[0-9]{1,18}$`)

// Installed answers GET /setup/installed, GitHub's setup_url redirect after an installation,
// with installation_id and setup_action in the query. It is public and stays available after
// setup. The installation_id is untrusted: it is fetched from GitHub with the App JWT, and only
// what GitHub answers is stored. An id that is malformed, unknown to GitHub or of another App
// writes nothing and is answered with an error. The instance has one installation: the repeat
// of the recorded one, or any other installation while one is recorded, writes nothing and,
// like a recorded one, redirects (302) to /login; the other is logged as a warning.
func (h *Handlers) Installed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := r.URL.Query().Get("installation_id")
	if !installationIDPattern.MatchString(raw) {
		writeValidationError(w, "installation_id must be the numeric id GitHub sent.")
		return
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeValidationError(w, "installation_id must be the numeric id GitHub sent.")
		return
	}

	required, err := Required(ctx, h.Queries)
	if err != nil {
		writeInternalError(w)
		return
	}
	if required {
		writeNotFound(w)
		return
	}

	live, err := h.Installations.LiveIDs(ctx)
	if err != nil {
		slog.Error("setup installed: list installations failed", "err", err)
		writeInternalError(w)
		return
	}
	if len(live) > 0 {
		h.finishInstalled(w, r, id, live[0] == id)
		return
	}

	inst, err := h.Installer.Installation(ctx, id)
	switch {
	case errors.Is(err, github.ErrInstallationNotFound):
		auth.WriteError(w, "NOT_FOUND", "GitHub does not know this installation.", http.StatusNotFound)
		return
	case err != nil:
		slog.Error("setup installed: confirming the installation failed", "err", err)
		auth.WriteError(w, "GITHUB_UNAVAILABLE", "GitHub could not confirm the installation.", http.StatusBadGateway)
		return
	}

	status := "active"
	if inst.SuspendedAt != nil {
		status = "suspended"
	}
	permissions := ""
	if raw, err := json.Marshal(inst.Permissions); err == nil && inst.Permissions != nil {
		permissions = string(raw)
	}
	outcome, err := h.Installations.Record(ctx, store.NewInstallation{
		GitHubInstallationID: inst.ID,
		AccountID:            inst.Account.ID,
		AccountLogin:         inst.Account.Login,
		AccountType:          inst.Account.Type,
		Status:               status,
		RepositorySelection:  inst.RepositorySelection,
		Permissions:          permissions,
	})
	if err != nil {
		slog.Error("setup installed: recording the installation failed", "err", err)
		writeInternalError(w)
		return
	}
	h.finishInstalled(w, r, id, outcome != store.InstallationRefused)
}

// finishInstalled redirects to /login; an installation the instance did not take is logged.
func (h *Handlers) finishInstalled(w http.ResponseWriter, r *http.Request, id int64, accepted bool) {
	if !accepted {
		slog.Warn("setup installed: another installation is already recorded; ignoring", "installation_id", id)
	}
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
	state := base64.RawURLEncoding.EncodeToString(b)
	logging.RegisterFor(StateTTL, state)
	return state, nil
}

func writeValidationError(w http.ResponseWriter, message string) {
	auth.WriteError(w, "VALIDATION_ERROR", message, http.StatusBadRequest)
}
