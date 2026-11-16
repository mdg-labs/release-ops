package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxPrivateKeyBody bounds the rotation request: a PEM RSA key is a few kilobytes, and JSON
// can at most double its size by escaping the line breaks.
const maxPrivateKeyBody = 64 << 10

// AppKeyVerifier checks a candidate private key against GitHub without storing it. It
// returns the App id the key was verified for.
type AppKeyVerifier interface {
	VerifyKey(ctx context.Context, privateKeyPEM []byte) (appID int64, err error)
}

// AppKeyStore replaces the stored private key.
type AppKeyStore interface {
	ReplacePrivateKey(ctx context.Context, appID int64, privateKeyPEM []byte) error
}

// GitHubAppHandlers serves the GitHub App's metadata and its key rotation. The metadata read
// checks the admin role itself as well as the router.
type GitHubAppHandlers struct {
	// Verifier and Apps serve the rotation; the metadata read does not use them.
	Verifier AppKeyVerifier
	Apps     AppKeyStore

	// Info, Installations, Config and RequiredPermissions serve the metadata read.
	Info                store.GitHubAppInfoRepository
	Installations       store.InstallationSettingsRepository
	Config              func() store.InstanceConfig
	RequiredPermissions map[string]string
}

type githubAppResponse struct {
	App          githubAppInfo         `json:"app"`
	Installation *installationResponse `json:"installation"`
	RoleMapping  roleMappingResponse   `json:"roleMapping"`
}

// githubAppInfo is the App without any secret: only whether each one is stored.
type githubAppInfo struct {
	AppID            int64  `json:"appId"`
	Slug             string `json:"slug"`
	ClientID         string `json:"clientId"`
	HTMLURL          string `json:"htmlUrl"`
	OwnerLogin       string `json:"ownerLogin"`
	OwnerType        string `json:"ownerType"`
	WebhookActive    bool   `json:"webhookActive"`
	HasPrivateKey    bool   `json:"hasPrivateKey"`
	HasClientSecret  bool   `json:"hasClientSecret"`
	HasWebhookSecret bool   `json:"hasWebhookSecret"`
}

type roleMappingResponse struct {
	AdminTeam  *string              `json:"adminTeam"`
	ViewerTeam *string              `json:"viewerTeam"`
	Problems   []roleMappingProblem `json:"problems"`
}

// roleMappingProblem is a configuration problem with ADMIN_TEAM or VIEWER_TEAM.
type roleMappingProblem struct {
	Variable string `json:"variable"`
	Code     string `json:"code"`
}

// Get handles GET /api/v1/github-app (specs.html section 7.0.1): the App's metadata, the
// recorded installation and the read-only role mapping. It answers 404 NOT_FOUND before an
// App is stored. No private key, client secret or webhook secret is read, so none can reach
// the response.
func (h *GitHubAppHandlers) Get(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	app, err := h.Info.Info(r.Context())
	if errors.Is(err, store.ErrGitHubAppNotFound) {
		auth.WriteError(w, "NOT_FOUND", "the GitHub App is not configured", http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("github app: reading failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load the GitHub App", http.StatusInternalServerError)
		return
	}
	items, err := h.Installations.List(r.Context())
	if err != nil {
		slog.Error("github app: listing installations failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to load the GitHub App", http.StatusInternalServerError)
		return
	}

	resp := githubAppResponse{App: githubAppInfo{
		AppID: app.AppID, Slug: app.Slug, ClientID: app.ClientID, HTMLURL: app.HTMLURL,
		OwnerLogin: app.OwnerLogin, OwnerType: app.OwnerType, WebhookActive: app.WebhookActive,
		HasPrivateKey: app.HasPrivateKey, HasClientSecret: app.HasClientSecret, HasWebhookSecret: app.HasWebhookSecret,
	}}
	var live *store.InstallationSummary
	for i := range items {
		if items[i].Status != "removed" {
			live = &items[i]
			break
		}
	}
	if live != nil {
		inst := newInstallationResponse(*live, h.RequiredPermissions)
		resp.Installation = &inst
	}
	resp.RoleMapping = roleMapping(h.Config(), live)
	writeRepositoryJSON(w, http.StatusOK, resp)
}

// roleMapping reports ADMIN_TEAM and VIEWER_TEAM and what is wrong with them for the
// installation: a team of another organization grants nothing or admits nobody, and on a
// personal account both are ignored (specs.html section 3.7). With no installation there is
// nothing to judge them against.
func roleMapping(cfg store.InstanceConfig, live *store.InstallationSummary) roleMappingResponse {
	out := roleMappingResponse{AdminTeam: emptyToNil(cfg.AdminTeam), ViewerTeam: emptyToNil(cfg.ViewerTeam), Problems: []roleMappingProblem{}}
	if live == nil {
		return out
	}
	for _, t := range []struct{ variable, team string }{{"ADMIN_TEAM", cfg.AdminTeam}, {"VIEWER_TEAM", cfg.ViewerTeam}} {
		if t.team == "" {
			continue
		}
		switch org, _, _ := strings.Cut(t.team, "/"); {
		case live.AccountType != "Organization":
			out.Problems = append(out.Problems, roleMappingProblem{Variable: t.variable, Code: "ignored_on_personal_account"})
		case !strings.EqualFold(org, live.AccountLogin):
			out.Problems = append(out.Problems, roleMappingProblem{Variable: t.variable, Code: "team_of_another_organization"})
		}
	}
	return out
}

type rotateKeyRequest struct {
	PrivateKey string `json:"privateKey"`
}

// keyRejectedMessage is the one message for every key that cannot be used, so a response does
// not tell which check failed.
const keyRejectedMessage = "the key could not be verified"

// RotatePrivateKey handles PUT /api/v1/github-app/private-key (specs.html §3.4). The new key
// replaces the stored one only once GitHub has accepted a JWT signed with it for this
// instance's App; any failure before that leaves the stored key unchanged. A key that is
// missing, not a key, rejected by GitHub or another App's answers 422 VALIDATION_ERROR with
// the same message. Neither the request's key nor the stored one appears in a response or a
// log line.
func (h *GitHubAppHandlers) RotatePrivateKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPrivateKeyBody)
	var req rotateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	}
	key := []byte(req.PrivateKey)
	req.PrivateKey = ""
	defer clear(key)
	if len(key) == 0 {
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	}

	appID, err := h.Verifier.VerifyKey(r.Context(), key)
	switch {
	case err == nil:
	case errors.Is(err, github.ErrInvalidPrivateKey), errors.Is(err, github.ErrAppRejected), errors.Is(err, github.ErrAppMismatch):
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	case errors.Is(err, store.ErrGitHubAppNotFound):
		auth.WriteError(w, "NOT_FOUND", "the GitHub App is not configured", http.StatusNotFound)
		return
	case errors.Is(err, github.ErrRequestFailed):
		auth.WriteError(w, "GITHUB_UNAVAILABLE", "GitHub could not be reached to check the key; try again", http.StatusBadGateway)
		return
	default:
		slog.Error("verify github app private key failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to verify the key", http.StatusInternalServerError)
		return
	}

	if err := h.Apps.ReplacePrivateKey(r.Context(), appID, key); err != nil {
		if errors.Is(err, store.ErrGitHubAppNotFound) {
			auth.WriteError(w, "NOT_FOUND", "the GitHub App is not configured", http.StatusNotFound)
			return
		}
		slog.Error("replace github app private key failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to store the key", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
