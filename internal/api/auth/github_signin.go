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
	"slices"
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
	// InstallationRepositoryIDs lists the GitHub ids of the installation's repositories the
	// token's user can access.
	InstallationRepositoryIDs(ctx context.Context, t github.UserToken, installationID int64) ([]int64, error)
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
	// Roles decides the role of a user the bootstrap rules do not cover. Without it only the
	// bootstrap admin can sign in. Access reads the installation and its repositories for a
	// viewer's scope; a viewer cannot sign in without it.
	Roles  RoleEvaluator
	Access store.AccessRepository
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

	role, err := h.decide(ctx, token, user, app)
	if err != nil {
		RedirectToLogin(w, r, exchangeErrorCode(err))
		return
	}
	if role.name == "" {
		RedirectToLogin(w, r, LoginErrorNoRole)
		return
	}
	repositoryIDs, err := h.viewerScope(ctx, token, role.name)
	if err != nil {
		RedirectToLogin(w, r, exchangeErrorCode(err))
		return
	}

	userID, err := h.Users.SignIn(ctx, store.GitHubUser{ID: user.ID, Login: user.Login}, role.pin)
	switch {
	case errors.Is(err, store.ErrBootstrapAdminTaken):
		RedirectToLogin(w, r, LoginErrorNoRole)
		return
	case err != nil:
		slog.Error("github sign-in: storing the user failed", "err", err)
		RedirectToLogin(w, r, LoginErrorServer)
		return
	}

	if err := StartGitHubSession(ctx, h.Sessions, GitHubSession{
		UserID: userID, GitHubID: user.ID, Login: user.Login, Role: role.name,
		EvaluatedAt: h.now(), RepositoryIDs: repositoryIDs,
	}); err != nil {
		slog.Error("github sign-in: starting the session failed", "err", err)
		RedirectToLogin(w, r, LoginErrorServer)
		return
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}

// decision is the outcome of the role decision at sign-in: the role ("" for no access) and
// whether the sign-in also pins the user as the bootstrap admin.
type decision struct {
	name string
	pin  bool
}

// decide gives the role of the user signing in. The pinned bootstrap admin is admin. With
// none pinned yet, the first sign-in of BOOTSTRAP_ADMIN_GITHUB_LOGIN is pinned; when that
// is unset, the App's owner (the owning user, or an owner of the owning organization) is.
// Everyone else gets the role GitHub's memberships give them, or none. A user nobody
// permits gets no user row.
func (h *GitHubSignInHandlers) decide(ctx context.Context, token github.UserToken, user github.User, app store.GitHubAppSignIn) (decision, error) {
	admin, pinned, err := h.Users.BootstrapAdmin(ctx)
	if err != nil {
		slog.Error("github sign-in: reading the bootstrap admin failed", "err", err)
		return decision{}, errServer
	}
	if pinned && admin.ID == user.ID {
		return decision{name: RoleAdmin}, nil
	}
	if !pinned {
		permitted, err := h.firstAdmin(ctx, token, user, app)
		if err != nil {
			return decision{}, err
		}
		if permitted {
			return decision{name: RoleAdmin, pin: true}, nil
		}
	}
	if h.Roles == nil {
		return decision{}, nil
	}
	name, err := h.Roles.Evaluate(ctx, user.ID)
	if err != nil {
		slog.Error("github sign-in: evaluating the role failed", "err", err)
		return decision{}, err
	}
	return decision{name: name}, nil
}

// firstAdmin reports whether the user is the one to pin as the bootstrap admin.
func (h *GitHubSignInHandlers) firstAdmin(ctx context.Context, token github.UserToken, user github.User, app store.GitHubAppSignIn) (bool, error) {
	if h.BootstrapAdminLogin != "" {
		return strings.EqualFold(user.Login, h.BootstrapAdminLogin), nil
	}
	if app.OwnerType == "Organization" {
		return h.GitHub.IsOrganizationOwner(ctx, token, app.OwnerLogin)
	}
	return strings.EqualFold(user.Login, app.OwnerLogin), nil
}

// viewerScope returns the row ids of the repositories a viewer may see: those the user can
// access on GitHub, with the user's own token, that are installed here. An admin has no
// list. Any failure is an error, never a shorter or wider list.
func (h *GitHubSignInHandlers) viewerScope(ctx context.Context, token github.UserToken, role string) ([]string, error) {
	if role != RoleViewer {
		return nil, nil
	}
	if h.Access == nil {
		slog.Error("github sign-in: a viewer signed in without an access repository")
		return nil, errServer
	}
	inst, ok, err := h.Access.LiveInstallation(ctx)
	if err != nil {
		slog.Error("github sign-in: reading the installation failed", "err", err)
		return nil, errServer
	}
	if !ok {
		return nil, errServer
	}
	accessible, err := h.GitHub.InstallationRepositoryIDs(ctx, token, inst.GitHubInstallationID)
	if err != nil {
		return nil, err
	}
	installed, err := h.Access.InstalledRepositoryIDs(ctx, inst.ID)
	if err != nil {
		slog.Error("github sign-in: reading the installed repositories failed", "err", err)
		return nil, errServer
	}
	seen := map[string]bool{}
	ids := []string{}
	for _, githubID := range accessible {
		if id, ok := installed[githubID]; ok && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, nil
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
