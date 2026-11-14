package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/store"
)

// LoginLinkTTL is how long a break-glass link stays valid.
const LoginLinkTTL = 15 * time.Minute

const (
	maxLoginLinkBody  = 1 << 10
	maxLoginLinkToken = 256
)

// CreateLoginLink mints a break-glass link for the bootstrap admin: a random token whose
// SHA-256 is stored with its expiry, returned as {appURL}/login/link?token=…. The token is
// shown to the caller once and is not recoverable. It returns store.ErrNoBootstrapAdmin
// while no bootstrap admin has signed in.
func CreateLoginLink(ctx context.Context, users store.SignInRepository, appURL string, now time.Time) (string, error) {
	raw, err := GenerateRawToken()
	if err != nil {
		return "", err
	}
	if err := users.CreateLoginLink(ctx, HashToken(raw), now.Add(LoginLinkTTL)); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/login/link?token=%s", strings.TrimRight(appURL, "/"), url.QueryEscape(raw)), nil
}

// LoginLinkHandlers serves POST /auth/login-link.
type LoginLinkHandlers struct {
	Sessions *scs.SessionManager
	Users    store.SignInRepository
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (h *LoginLinkHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

type loginLinkRequest struct {
	Token string `json:"token"`
}

// Redeem consumes the link with a guarded write before anything else happens and only then
// starts the bootstrap admin's session, so concurrent redemptions yield one session. An
// unknown, expired or used token, and a body without a token, are all refused (400) with one
// message.
func (h *LoginLinkHandlers) Redeem(w http.ResponseWriter, r *http.Request) {
	var req loginLinkRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLoginLinkBody)).Decode(&req); err != nil ||
		req.Token == "" || len(req.Token) > maxLoginLinkToken {
		writeInvalidLoginLink(w)
		return
	}

	admin, err := h.Users.RedeemLoginLink(r.Context(), HashToken(req.Token))
	switch {
	case errors.Is(err, store.ErrLoginLinkInvalid), errors.Is(err, store.ErrNoBootstrapAdmin):
		writeInvalidLoginLink(w)
		return
	case err != nil:
		slog.Error("login link: redeeming failed", "err", err)
		WriteError(w, "INTERNAL_ERROR", "Sign-in failed.", http.StatusInternalServerError)
		return
	}

	if err := StartGitHubSession(r.Context(), h.Sessions, GitHubSession{
		UserID: admin.UserID, GitHubID: admin.ID, Login: admin.Login, Role: RoleAdmin, EvaluatedAt: h.now(),
	}); err != nil {
		slog.Error("login link: starting the session failed", "err", err)
		WriteError(w, "INTERNAL_ERROR", "Sign-in failed.", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, githubSessionResponse{User: &githubUserResponse{ID: admin.ID, Login: admin.Login, Role: RoleAdmin}})
}

func writeInvalidLoginLink(w http.ResponseWriter) {
	WriteError(w, "INVALID_LINK", "This sign-in link is invalid, expired or already used.", http.StatusBadRequest)
}
