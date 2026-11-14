package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// GitHubCallbackPath is where GitHub returns the browser after authorization, behind the
	// Next.js proxy. The App's manifest registers {APP_URL}{GitHubCallbackPath}.
	GitHubCallbackPath = "/api/go/api/v1/auth/github/callback"

	oauthStateKey    = "oauthState"
	oauthVerifierKey = "oauthVerifier"
	oauthExpiresKey  = "oauthExpiresAt"
	oauthRedirectKey = "oauthRedirect"

	// oauthAttemptTTL is how long a sign-in attempt may take between the redirect to GitHub
	// and the callback.
	oauthAttemptTTL = 10 * time.Minute
	oauthRandBytes  = 32
)

// Fixed error codes the callback hands /login in the "error" query parameter. No text of
// GitHub's is ever part of one.
const (
	LoginErrorDenied            = "denied"
	LoginErrorInvalidState      = "invalid_state"
	LoginErrorRateLimited       = "rate_limited"
	LoginErrorExchangeFailed    = "exchange_failed"
	LoginErrorGitHubUnavailable = "github_unavailable"
	LoginErrorNoRole            = "no_role"
	LoginErrorServer            = "server_error"
)

// OAuthProvider is the GitHub side of the sign-in.
type OAuthProvider interface {
	AuthorizeURL(clientID, redirectURI, state, codeChallenge string) string
	ExchangeCode(ctx context.Context, p github.ExchangeParams) (github.UserToken, error)
	User(ctx context.Context, t github.UserToken) (github.User, error)
	IsOrganizationOwner(ctx context.Context, t github.UserToken, org string) (bool, error)
}

// AppSignIn yields the stored App's sign-in credentials.
type AppSignIn interface {
	SignIn(ctx context.Context) (store.GitHubAppSignIn, error)
}

// GitHubSignInHandlers serves the GitHub OAuth sign-in (specs.html § 3.5).
type GitHubSignInHandlers struct {
	Sessions *scs.SessionManager
	Apps     AppSignIn
	Users    store.SignInRepository
	GitHub   OAuthProvider
	// AppURL is APP_URL; the redirect_uri sent to GitHub is built from it alone.
	AppURL string
	// BootstrapAdminLogin is BOOTSTRAP_ADMIN_GITHUB_LOGIN: the login of the first admin,
	// used only until a bootstrap admin is pinned.
	BootstrapAdminLogin string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

func (h *GitHubSignInHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *GitHubSignInHandlers) redirectURI() string {
	return strings.TrimRight(h.AppURL, "/") + GitHubCallbackPath
}

// Login answers GET /auth/github/login: it binds a fresh state and PKCE verifier to the
// pre-login session and redirects (302) to GitHub's authorize URL. It answers 503 while no
// App is configured.
func (h *GitHubSignInHandlers) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	app, err := h.Apps.SignIn(ctx)
	switch {
	case errors.Is(err, store.ErrGitHubAppNotFound):
		WriteError(w, "SETUP_REQUIRED", "Sign-in is not available until the instance is set up.", http.StatusServiceUnavailable)
		return
	case err != nil:
		slog.Error("github sign-in: reading the app failed", "err", err)
		WriteError(w, "INTERNAL_ERROR", "Sign-in is unavailable.", http.StatusInternalServerError)
		return
	}
	clear(app.ClientSecret)

	state, err := randomURLToken()
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "Sign-in is unavailable.", http.StatusInternalServerError)
		return
	}
	verifier, err := randomURLToken()
	if err != nil {
		WriteError(w, "INTERNAL_ERROR", "Sign-in is unavailable.", http.StatusInternalServerError)
		return
	}
	challenge := sha256.Sum256([]byte(verifier))

	h.Sessions.Put(ctx, oauthStateKey, state)
	h.Sessions.Put(ctx, oauthVerifierKey, verifier)
	h.Sessions.Put(ctx, oauthExpiresKey, h.now().Add(oauthAttemptTTL).Unix())
	h.Sessions.Put(ctx, oauthRedirectKey, SafeRedirectPath(r.URL.Query().Get("redirect")))

	http.Redirect(w, r, h.GitHub.AuthorizeURL(app.ClientID, h.redirectURI(), state, base64.RawURLEncoding.EncodeToString(challenge[:])), http.StatusFound)
}

// Callback answers GET /auth/github/callback. A missing, mismatched, expired or spent state
// is refused before GitHub is called, and the attempt is spent by the first request that
// presents it. The browser arrives here from GitHub, so every failure, that one included,
// redirects (302) to /login with a fixed error code and leaves no session. The user token lives in this function only.
func (h *GitHubSignInHandlers) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	query := r.URL.Query()

	verifier, redirect, ok := h.takeAttempt(r, query.Get("state"))
	if !ok {
		RedirectToLogin(w, r, LoginErrorInvalidState)
		return
	}
	if query.Get("error") != "" {
		RedirectToLogin(w, r, LoginErrorDenied)
		return
	}

	app, err := h.Apps.SignIn(ctx)
	if err != nil {
		slog.Error("github sign-in: reading the app failed", "err", err)
		RedirectToLogin(w, r, LoginErrorGitHubUnavailable)
		return
	}
	token, err := h.GitHub.ExchangeCode(ctx, github.ExchangeParams{
		ClientID:     app.ClientID,
		ClientSecret: string(app.ClientSecret),
		Code:         query.Get("code"),
		RedirectURI:  h.redirectURI(),
		CodeVerifier: verifier,
	})
	clear(app.ClientSecret)
	if err != nil {
		RedirectToLogin(w, r, exchangeErrorCode(err))
		return
	}

	user, err := h.GitHub.User(ctx, token)
	if err != nil {
		RedirectToLogin(w, r, exchangeErrorCode(err))
		return
	}

	pin, permitted, err := h.permit(ctx, token, user, app)
	if err != nil {
		RedirectToLogin(w, r, exchangeErrorCode(err))
		return
	}
	if !permitted {
		RedirectToLogin(w, r, LoginErrorNoRole)
		return
	}

	userID, err := h.Users.SignIn(ctx, store.GitHubUser{ID: user.ID, Login: user.Login}, pin)
	switch {
	case errors.Is(err, store.ErrBootstrapAdminTaken):
		RedirectToLogin(w, r, LoginErrorNoRole)
		return
	case err != nil:
		slog.Error("github sign-in: storing the user failed", "err", err)
		RedirectToLogin(w, r, LoginErrorServer)
		return
	}

	if err := StartGitHubSession(ctx, h.Sessions, userID, user.ID, user.Login, RoleAdmin); err != nil {
		slog.Error("github sign-in: starting the session failed", "err", err)
		RedirectToLogin(w, r, LoginErrorServer)
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// permit decides whether the user may sign in, and whether doing so pins them as the
// bootstrap admin. Until roles are derived from GitHub the bootstrap admin is the only user
// let in. With none pinned yet, the first sign-in of BOOTSTRAP_ADMIN_GITHUB_LOGIN is pinned;
// when that is unset, the App's owner (the owning user, or an owner of the owning
// organization) is. A user nobody permits gets no user row.
func (h *GitHubSignInHandlers) permit(ctx context.Context, token github.UserToken, user github.User, app store.GitHubAppSignIn) (pin, permitted bool, err error) {
	admin, pinned, err := h.Users.BootstrapAdmin(ctx)
	if err != nil {
		slog.Error("github sign-in: reading the bootstrap admin failed", "err", err)
		return false, false, errServer
	}
	if pinned {
		return false, admin.ID == user.ID, nil
	}

	if h.BootstrapAdminLogin != "" {
		return true, strings.EqualFold(user.Login, h.BootstrapAdminLogin), nil
	}
	if app.OwnerType == "Organization" {
		owner, err := h.GitHub.IsOrganizationOwner(ctx, token, app.OwnerLogin)
		if err != nil {
			return false, false, err
		}
		return true, owner, nil
	}
	return true, strings.EqualFold(user.Login, app.OwnerLogin), nil
}

var errServer = errors.New("server error")

func exchangeErrorCode(err error) string {
	switch {
	case errors.Is(err, errServer):
		return LoginErrorServer
	case errors.Is(err, github.ErrOAuthRejected), errors.Is(err, github.ErrUserTokenRejected):
		return LoginErrorExchangeFailed
	default:
		return LoginErrorGitHubUnavailable
	}
}

// takeAttempt spends the sign-in attempt bound to the session: the state, the verifier, the
// expiry and the requested path are removed by the first request that reaches it, match or
// not. It reports ok only for a present, unexpired attempt whose state equals the presented
// one, compared in constant time.
func (h *GitHubSignInHandlers) takeAttempt(r *http.Request, presented string) (verifier, redirect string, ok bool) {
	ctx := r.Context()
	stored := h.Sessions.GetString(ctx, oauthStateKey)
	verifier = h.Sessions.GetString(ctx, oauthVerifierKey)
	expires := h.Sessions.GetInt64(ctx, oauthExpiresKey)
	redirect = h.Sessions.GetString(ctx, oauthRedirectKey)
	h.Sessions.Remove(ctx, oauthStateKey)
	h.Sessions.Remove(ctx, oauthVerifierKey)
	h.Sessions.Remove(ctx, oauthExpiresKey)
	h.Sessions.Remove(ctx, oauthRedirectKey)

	if stored == "" || verifier == "" || presented == "" || expires == 0 || !h.now().Before(time.Unix(expires, 0)) {
		return "", "", false
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(stored)) != 1 {
		return "", "", false
	}
	return verifier, SafeRedirectPath(redirect), true
}

// RedirectToLogin sends the browser (302) to /login with a fixed error code.
func RedirectToLogin(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, "/login?error="+url.QueryEscape(code), http.StatusFound)
}

func randomURLToken() (string, error) {
	b := make([]byte, oauthRandBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SafeRedirectPath returns p when it is a same-origin relative path (one leading slash, no
// backslash or control character, no scheme or host), otherwise "/".
func SafeRedirectPath(p string) string {
	if p == "" || p[0] != '/' || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\") {
		return "/"
	}
	for _, c := range p {
		if c < 0x20 || c == 0x7f {
			return "/"
		}
	}
	u, err := url.Parse(p)
	if err != nil || u.IsAbs() || u.Host != "" || u.User != nil {
		return "/"
	}
	return p
}
