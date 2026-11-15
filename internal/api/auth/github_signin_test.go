package auth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	signInAppURL       = "https://release-ops.example.com"
	signInClientID     = "fixture-client-id"
	signInClientSecret = "fixture-client-secret-value"
	signInKeyHex       = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
)

// fakeGitHub answers the OAuth token endpoint and the user API like GitHub does, including
// the 200-with-an-error answer for a refused code and the PKCE check.
type fakeGitHub struct {
	srv *httptest.Server

	mu        sync.Mutex
	grants    map[string]grant
	tokens    map[string]github.User
	orgOwners map[string]bool
	seq       int

	// userRepos are the repository ids each user can access, by user id; repoBearers
	// records the token of every repository listing.
	userRepos   map[int64][]int64
	repoBearers []string
	repoStatus  atomic.Int32

	exchanges      atomic.Int32
	exchangeStatus atomic.Int32
	pkceVerified   atomic.Int32
}

type grant struct {
	user      github.User
	challenge string
	orgOwner  bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{grants: map[string]grant{}, tokens: map[string]github.User{}, orgOwners: map[string]bool{}, userRepos: map[int64][]int64{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", f.accessToken)
	mux.HandleFunc("GET /api/v3/user", f.user)
	mux.HandleFunc("GET /api/v3/user/memberships/orgs/{org}", f.membership)
	mux.HandleFunc("GET /api/v3/user/installations/{id}/repositories", f.installationRepositories)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// grantCode registers an authorization code GitHub would have issued for user at the
// authorize step that sent challenge.
func (f *fakeGitHub) grantCode(code string, user github.User, challenge string, orgOwner bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[code] = grant{user: user, challenge: challenge, orgOwner: orgOwner}
}

func (f *fakeGitHub) accessToken(w http.ResponseWriter, r *http.Request) {
	f.exchanges.Add(1)
	if status := int(f.exchangeStatus.Load()); status != 0 {
		w.WriteHeader(status)
		return
	}
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	refuse := func(code string) {
		_, _ = fmt.Fprintf(w, `{"error":%q,"error_description":"GitHub text that must never be reflected"}`, code)
	}

	if req["client_id"] != signInClientID || req["client_secret"] != signInClientSecret {
		refuse("incorrect_client_credentials")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.grants[req["code"]]
	if !ok {
		refuse("bad_verification_code")
		return
	}
	delete(f.grants, req["code"])
	sum := sha256.Sum256([]byte(req["code_verifier"]))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		refuse("bad_verification_code")
		return
	}
	if req["redirect_uri"] != signInAppURL+"/api/go/api/v1/auth/github/callback" {
		refuse("redirect_uri_mismatch")
		return
	}
	f.pkceVerified.Add(1)
	f.seq++
	token := fmt.Sprintf("fixture-user-token-%d", f.seq)
	f.tokens[token] = g.user
	f.orgOwners[token] = g.orgOwner
	_, _ = fmt.Fprintf(w, `{"access_token":%q,"token_type":"bearer","expires_in":28800,"refresh_token":"fixture-refresh-token-%d"}`, token, f.seq)
}

func (f *fakeGitHub) userFor(r *http.Request) (string, github.User, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.tokens[token]
	return token, u, ok
}

func (f *fakeGitHub) user(w http.ResponseWriter, r *http.Request) {
	_, u, ok := f.userFor(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": u.ID, "login": u.Login, "type": "User"})
}

func (f *fakeGitHub) installationRepositories(w http.ResponseWriter, r *http.Request) {
	token, u, ok := f.userFor(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	f.repoBearers = append(f.repoBearers, token)
	ids := f.userRepos[u.ID]
	f.mu.Unlock()
	if status := int(f.repoStatus.Load()); status != 0 {
		w.WriteHeader(status)
		return
	}
	if r.PathValue("id") != "1" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	repos := make([]map[string]any, len(ids))
	for i, id := range ids {
		repos[i] = map[string]any{"id": id, "full_name": fmt.Sprintf("acme/repo-%d", id), "default_branch": "main"}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"total_count": len(repos), "repositories": repos})
}

func (f *fakeGitHub) membership(w http.ResponseWriter, r *http.Request) {
	token, _, ok := f.userFor(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	owner := f.orgOwners[token]
	f.mu.Unlock()
	if !owner && r.PathValue("org") != "acme" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	role := "member"
	if owner {
		role = "admin"
	}
	_, _ = fmt.Fprintf(w, `{"state":"active","role":%q}`, role)
}

type signInHarness struct {
	t        *testing.T
	db       *store.DB
	store    *store.Store
	gh       *fakeGitHub
	srv      *httptest.Server
	handlers *auth.GitHubSignInHandlers
	clock    atomic.Int64
	next     int
}

type harnessOptions struct {
	bootstrapLogin string
	ownerType      string
	ownerLogin     string
	noApp          bool
	// roles decides the role of a user who is not the bootstrap admin; repositories seeds
	// the installation and that many repositories (github ids 1..n, row ids repo-1..repo-n).
	roles        auth.RoleEvaluator
	repositories int
}

func newSignInHarness(t *testing.T, opts harnessOptions) *signInHarness {
	t.Helper()

	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(signInKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	st := store.New(db, cipher)
	gh := newFakeGitHub(t)
	h := &signInHarness{t: t, db: db, store: st, gh: gh}
	h.clock.Store(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC).Unix())

	if !opts.noApp {
		ownerType, ownerLogin := opts.ownerType, opts.ownerLogin
		if ownerType == "" {
			ownerType, ownerLogin = "User", "app-owner"
		}
		if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
			AppID: 1234567, Slug: "release-ops-acme", ClientID: signInClientID,
			HTMLURL: "https://github.com/apps/release-ops-acme", OwnerLogin: ownerLogin, OwnerType: ownerType,
			PrivateKey: "fixture-private-key-body", ClientSecret: signInClientSecret, WebhookSecret: "fixture-webhook-secret-value",
		}); err != nil {
			t.Fatalf("store app: %v", err)
		}
	}

	sm := auth.NewSessionManager(db.Writer, strings.Repeat("s", 32), false)
	api := github.NewClient(github.APIBaseURL(gh.srv.URL), 0)
	h.handlers = &auth.GitHubSignInHandlers{
		Sessions:            sm,
		Apps:                st.GitHubApp(),
		Users:               st.SignIn(),
		GitHub:              github.NewOAuthClient(api, gh.srv.URL),
		AppURL:              signInAppURL,
		BootstrapAdminLogin: opts.bootstrapLogin,
		Now:                 func() time.Time { return time.Unix(h.clock.Load(), 0) },
	}
	if opts.repositories > 0 {
		storetest.SeedRepositories(t, db, opts.repositories)
	}
	h.handlers.Roles = opts.roles
	h.handlers.Access = st.Access()
	linkHandlers := &auth.LoginLinkHandlers{Sessions: sm, Users: st.SignIn()}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Get("/auth/github/login", h.handlers.Login)
	r.Get("/auth/github/callback", h.handlers.Callback)
	r.Post("/auth/login-link", linkHandlers.Redeem)
	r.Get("/auth/session", (&auth.Handlers{SessionManager: sm}).Session)
	r.With(apimw.RequireSession(sm)).Get("/protected", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h.srv = httptest.NewServer(r)
	t.Cleanup(h.srv.Close)
	return h
}

func (h *signInHarness) newClient() *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (h *signInHarness) get(c *http.Client, path string) (*http.Response, string) {
	h.t.Helper()
	resp, err := c.Get(h.srv.URL + path)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func (h *signInHarness) sessionCookie(c *http.Client) string {
	u, _ := url.Parse(h.srv.URL)
	for _, cookie := range c.Jar.Cookies(u) {
		if cookie.Name == auth.SessionCookieName {
			return cookie.Value
		}
	}
	return ""
}

// begin starts a sign-in and returns the state and PKCE challenge GitHub was sent.
func (h *signInHarness) begin(c *http.Client, redirect string) (state, challenge string) {
	h.t.Helper()
	path := "/auth/github/login"
	if redirect != "" {
		path += "?redirect=" + url.QueryEscape(redirect)
	}
	resp, _ := h.get(c, path)
	if resp.StatusCode != http.StatusFound {
		h.t.Fatalf("login start status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		h.t.Fatalf("parse Location: %v", err)
	}
	return loc.Query().Get("state"), loc.Query().Get("code_challenge")
}

func (h *signInHarness) nextCode() string {
	h.next++
	return fmt.Sprintf("code-%d", h.next)
}

// signIn runs a whole sign-in as user and returns the callback response.
func (h *signInHarness) signIn(c *http.Client, user github.User, redirect string, orgOwner bool) *http.Response {
	h.t.Helper()
	state, challenge := h.begin(c, redirect)
	code := h.nextCode()
	h.gh.grantCode(code, user, challenge, orgOwner)
	resp, _ := h.get(c, "/auth/github/callback?code="+code+"&state="+url.QueryEscape(state))
	return resp
}

func (h *signInHarness) sessionUser(c *http.Client) map[string]any {
	h.t.Helper()
	_, body := h.get(c, "/auth/session")
	var out struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		h.t.Fatalf("decode session %q: %v", body, err)
	}
	return out.User
}

func (h *signInHarness) count(table string) int {
	h.t.Helper()
	var n int
	if err := h.db.Writer.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		h.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func wantLoginError(t *testing.T, resp *http.Response, code string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error="+code {
		t.Fatalf("callback = %d Location %q, want 302 to /login?error=%s", resp.StatusCode, resp.Header.Get("Location"), code)
	}
}

func TestGitHubLoginRedirectsToAuthorizeWithStateAndPKCE(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{})
	c := h.newClient()

	resp, _ := h.get(c, "/auth/github/login?redirect=%2Fjobs")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Scheme+"://"+loc.Host != h.gh.srv.URL || loc.Path != "/login/oauth/authorize" {
		t.Errorf("redirect goes to %s%s, want the authorize endpoint of %s", loc.Host, loc.Path, h.gh.srv.URL)
	}
	q := loc.Query()
	if q.Get("client_id") != signInClientID {
		t.Errorf("client_id = %q, want %q", q.Get("client_id"), signInClientID)
	}
	if q.Get("redirect_uri") != signInAppURL+"/api/go/api/v1/auth/github/callback" {
		t.Errorf("redirect_uri = %q, want the callback built from APP_URL", q.Get("redirect_uri"))
	}
	if q.Get("code_challenge_method") != "S256" || len(q.Get("code_challenge")) != 43 {
		t.Errorf("code_challenge = %q (%s), want a 43-character S256 challenge", q.Get("code_challenge"), q.Get("code_challenge_method"))
	}
	state := q.Get("state")
	if len(state) < 40 {
		t.Errorf("state = %q, want at least 256 bits", state)
	}
	if strings.Contains(resp.Header.Get("Location"), signInClientSecret) || q.Get("client_secret") != "" {
		t.Error("the authorize URL carries the client secret")
	}

	// State and verifier live in the server-side session: the cookie holds neither.
	cookie := h.sessionCookie(c)
	if cookie == "" || strings.Contains(cookie, state) {
		t.Fatalf("session cookie %q must exist and must not contain the state", cookie)
	}
	var blob []byte
	if err := h.db.Writer.QueryRow(`SELECT data FROM sessions WHERE token = ?`, cookie).Scan(&blob); err != nil {
		t.Fatalf("read pre-login session: %v", err)
	}
	if !bytes.Contains(blob, []byte(state)) {
		t.Error("the pre-login session does not hold the state")
	}
	if !bytes.Contains(blob, []byte("/jobs")) {
		t.Error("the pre-login session does not hold the requested path")
	}
}

func TestGitHubLoginIsUnavailableBeforeSetup(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{noApp: true})
	resp, _ := h.get(h.newClient(), "/auth/github/login")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while no App exists", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("redirected to %q with no App", loc)
	}
}

func TestCallbackRefusesAnInvalidAttemptBeforeCallingGitHub(t *testing.T) {
	t.Parallel()
	admin := github.User{ID: 583231, Login: "octocat"}

	cases := []struct {
		name string
		run  func(t *testing.T, h *signInHarness) *http.Response
	}{
		{"no pre-login session", func(t *testing.T, h *signInHarness) *http.Response {
			resp, _ := h.get(h.newClient(), "/auth/github/callback?code=abc&state=anything")
			return resp
		}},
		{"missing state", func(t *testing.T, h *signInHarness) *http.Response {
			c := h.newClient()
			h.begin(c, "")
			resp, _ := h.get(c, "/auth/github/callback?code=abc")
			return resp
		}},
		{"wrong state", func(t *testing.T, h *signInHarness) *http.Response {
			c := h.newClient()
			_, challenge := h.begin(c, "")
			h.gh.grantCode("abc", admin, challenge, false)
			resp, _ := h.get(c, "/auth/github/callback?code=abc&state=not-the-state")
			return resp
		}},
		{"state of another browser's attempt", func(t *testing.T, h *signInHarness) *http.Response {
			other := h.newClient()
			otherState, _ := h.begin(other, "")
			c := h.newClient()
			h.begin(c, "")
			resp, _ := h.get(c, "/auth/github/callback?code=abc&state="+url.QueryEscape(otherState))
			return resp
		}},
		{"spent by a wrong attempt", func(t *testing.T, h *signInHarness) *http.Response {
			c := h.newClient()
			state, challenge := h.begin(c, "")
			h.gh.grantCode("abc", admin, challenge, false)
			if resp, _ := h.get(c, "/auth/github/callback?code=abc&state=wrong"); resp.StatusCode != http.StatusFound {
				t.Fatalf("wrong state status = %d, want 302", resp.StatusCode)
			}
			resp, _ := h.get(c, "/auth/github/callback?code=abc&state="+url.QueryEscape(state))
			return resp
		}},
		{"reused after a sign-in", func(t *testing.T, h *signInHarness) *http.Response {
			c := h.newClient()
			state, challenge := h.begin(c, "")
			h.gh.grantCode("abc", admin, challenge, false)
			path := "/auth/github/callback?code=abc&state=" + url.QueryEscape(state)
			if resp, _ := h.get(c, path); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
				t.Fatalf("first callback = %d %q, want a sign-in", resp.StatusCode, resp.Header.Get("Location"))
			}
			before := h.gh.exchanges.Load()
			resp, _ := h.get(c, path)
			if h.gh.exchanges.Load() != before {
				t.Error("the replay reached GitHub")
			}
			return resp
		}},
		{"expired", func(t *testing.T, h *signInHarness) *http.Response {
			c := h.newClient()
			state, challenge := h.begin(c, "")
			h.gh.grantCode("abc", admin, challenge, false)
			h.clock.Add(int64((11 * time.Minute).Seconds()))
			resp, _ := h.get(c, "/auth/github/callback?code=abc&state="+url.QueryEscape(state))
			return resp
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
			resp := tc.run(t, h)
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error=invalid_state" {
				t.Fatalf("status = %d to %q, want 302 to /login?error=invalid_state", resp.StatusCode, resp.Header.Get("Location"))
			}
			if tc.name != "reused after a sign-in" && h.gh.exchanges.Load() != 0 {
				t.Errorf("GitHub was called %d times before the state was valid", h.gh.exchanges.Load())
			}
			if tc.name != "reused after a sign-in" && h.count("users") != 0 {
				t.Errorf("a refused attempt created %d users", h.count("users"))
			}
		})
	}
}

func TestCallbackFailuresLeaveNoSession(t *testing.T) {
	t.Parallel()
	admin := github.User{ID: 583231, Login: "octocat"}

	t.Run("denied on GitHub", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
		c := h.newClient()
		state, _ := h.begin(c, "")
		resp, _ := h.get(c, "/auth/github/callback?error=access_denied&error_description=The+user+denied&state="+url.QueryEscape(state))
		wantLoginError(t, resp, auth.LoginErrorDenied)
		if h.sessionUser(c) != nil {
			t.Error("a denied sign-in produced a session")
		}
	})
	t.Run("code refused", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
		c := h.newClient()
		state, _ := h.begin(c, "")
		resp, _ := h.get(c, "/auth/github/callback?code=never-issued&state="+url.QueryEscape(state))
		wantLoginError(t, resp, auth.LoginErrorExchangeFailed)
		if h.sessionUser(c) != nil || h.count("users") != 0 {
			t.Error("a refused code produced a session or a user")
		}
	})
	t.Run("PKCE verifier mismatch", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
		c := h.newClient()
		state, _ := h.begin(c, "")
		h.gh.grantCode("code-x", admin, "a-challenge-of-another-verifier", false)
		resp, _ := h.get(c, "/auth/github/callback?code=code-x&state="+url.QueryEscape(state))
		wantLoginError(t, resp, auth.LoginErrorExchangeFailed)
		if h.sessionUser(c) != nil {
			t.Error("a code bound to another PKCE challenge produced a session")
		}
	})
	t.Run("GitHub unavailable", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
		c := h.newClient()
		state, _ := h.begin(c, "")
		h.gh.exchangeStatus.Store(http.StatusBadGateway)
		resp, _ := h.get(c, "/auth/github/callback?code=abc&state="+url.QueryEscape(state))
		wantLoginError(t, resp, auth.LoginErrorGitHubUnavailable)
		if h.sessionUser(c) != nil {
			t.Error("an unavailable GitHub produced a session")
		}
	})
}

func TestSuccessfulSignInStartsARenewedSessionForTheBootstrapAdmin(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "OctoCat"})
	c := h.newClient()

	state, challenge := h.begin(c, "/jobs?view=problems")
	preLogin := h.sessionCookie(c)
	h.gh.grantCode("code-1", github.User{ID: 583231, Login: "octocat"}, challenge, false)
	resp, _ := h.get(c, "/auth/github/callback?code=code-1&state="+url.QueryEscape(state))

	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/jobs?view=problems" {
		t.Fatalf("callback = %d %q, want 302 to the requested path", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h.gh.pkceVerified.Load() != 1 {
		t.Error("the exchange did not carry the PKCE verifier of the login start")
	}
	if after := h.sessionCookie(c); after == "" || after == preLogin {
		t.Errorf("session token after sign-in = %q, before = %q; want a new token", after, preLogin)
	}
	var n int
	if err := h.db.Writer.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token = ?`, preLogin).Scan(&n); err != nil || n != 0 {
		t.Errorf("pre-login session still stored = %d, %v; want it discarded", n, err)
	}

	user := h.sessionUser(c)
	if user["id"] != float64(583231) || user["login"] != "octocat" || user["role"] != "admin" {
		t.Errorf("session user = %v, want id 583231, login octocat, role admin", user)
	}
	if resp, _ := h.get(c, "/protected"); resp.StatusCode != http.StatusNoContent {
		t.Errorf("protected route with the session = %d, want 204", resp.StatusCode)
	}
	if resp, _ := h.get(h.newClient(), "/protected"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("protected route without a session = %d, want 401", resp.StatusCode)
	}

	admin, ok, err := h.store.SignIn().BootstrapAdmin(context.Background())
	if err != nil || !ok || admin.ID != 583231 {
		t.Fatalf("bootstrap admin = %+v, %v, %v; want the pinned numeric id 583231", admin, ok, err)
	}
	var userID string
	if err := h.db.Writer.QueryRow(`SELECT user_id FROM sessions WHERE token = ?`, h.sessionCookie(c)).Scan(&userID); err != nil || userID != admin.UserID {
		t.Errorf("sessions.user_id = %q, %v; want %q", userID, err, admin.UserID)
	}
}

// The user token, the authorization code, the client secret and the PKCE verifier appear in no
// session blob, no byte of the database files and no log line.
func TestSignInNeverStoresOrLogsTheUserToken(t *testing.T) {
	var logs bytes.Buffer
	var logsMu sync.Mutex
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logsMu.Lock()
		defer logsMu.Unlock()
		return logs.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
	c := h.newClient()
	if resp := h.signIn(c, github.User{ID: 583231, Login: "octocat"}, "", false); resp.StatusCode != http.StatusFound {
		t.Fatalf("sign-in = %d, want 302", resp.StatusCode)
	}
	// A failed attempt as well: the error paths must not log either.
	c2 := h.newClient()
	st2, _ := h.begin(c2, "")
	h.get(c2, "/auth/github/callback?code=never-issued&state="+url.QueryEscape(st2))

	secrets := []string{"fixture-user-token-1", "fixture-refresh-token-1", "code-1", signInClientSecret}

	rows, err := h.db.Writer.Query(`SELECT data FROM sessions`)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			t.Fatalf("scan: %v", err)
		}
		for _, s := range secrets {
			if bytes.Contains(blob, []byte(s)) {
				t.Errorf("a session blob contains %q", s)
			}
		}
	}
	_ = rows.Close()

	var dbPath string
	if err := h.db.Writer.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&dbPath); err != nil {
		t.Fatalf("database path: %v", err)
	}
	for _, suffix := range []string{"", "-wal"} {
		raw, err := os.ReadFile(dbPath + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %s: %v", dbPath+suffix, err)
		}
		for _, s := range secrets[:3] {
			if bytes.Contains(raw, []byte(s)) {
				t.Errorf("the database file%s contains %q", suffix, s)
			}
		}
	}

	logsMu.Lock()
	defer logsMu.Unlock()
	for _, s := range secrets {
		if strings.Contains(logs.String(), s) {
			t.Errorf("logs contain %q: %s", s, logs.String())
		}
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestBootstrapAdminIsResolvedToTheNumericIDAtTheFirstMatchingSignIn(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
	ctx := context.Background()

	// A stranger is not let in while the bootstrap login is unresolved: no user, no session,
	// no public sign-up.
	stranger := h.newClient()
	wantLoginError(t, h.signIn(stranger, github.User{ID: 42, Login: "stranger"}, "", false), auth.LoginErrorNoRole)
	if h.sessionUser(stranger) != nil || h.count("users") != 0 || h.count("auth_identities") != 0 {
		t.Fatal("a user nobody permitted got a session or a row")
	}
	if _, ok, _ := h.store.SignIn().BootstrapAdmin(ctx); ok {
		t.Fatal("a refused sign-in pinned a bootstrap admin")
	}

	admin := h.newClient()
	if resp := h.signIn(admin, github.User{ID: 583231, Login: "octocat"}, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("first admin sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}

	// The failure scenario: the admin renames themselves. The same user is found by id, the
	// login follows, and the stored bootstrap admin stays pinned.
	renamed := h.newClient()
	if resp := h.signIn(renamed, github.User{ID: 583231, Login: "octocat-renamed"}, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("sign-in after a rename = %d %q, want a sign-in", resp.StatusCode, resp.Header.Get("Location"))
	}
	if h.count("users") != 1 || h.count("auth_identities") != 1 {
		t.Fatalf("users = %d, identities = %d after a rename; want 1 and 1", h.count("users"), h.count("auth_identities"))
	}
	if u := h.sessionUser(renamed); u["login"] != "octocat-renamed" || u["id"] != float64(583231) {
		t.Errorf("session after a rename = %v, want the new login on the same id", u)
	}
	pinned, ok, _ := h.store.SignIn().BootstrapAdmin(ctx)
	if !ok || pinned.ID != 583231 || pinned.Login != "octocat-renamed" {
		t.Fatalf("bootstrap admin = %+v, %v; want id 583231 with the new login", pinned, ok)
	}

	// The env value is ignored afterwards. Another user who takes over the old login name
	// is not admin and gets no user row...
	taker := h.newClient()
	wantLoginError(t, h.signIn(taker, github.User{ID: 999, Login: "octocat"}, "", false), auth.LoginErrorNoRole)
	if h.sessionUser(taker) != nil || h.count("users") != 1 {
		t.Fatal("a user who took over the bootstrap login got in")
	}
	// ...and the admin still signs in once the env value is changed or removed.
	for _, env := range []string{"someone-else", ""} {
		h.handlers.BootstrapAdminLogin = env
		again := h.newClient()
		if resp := h.signIn(again, github.User{ID: 583231, Login: "octocat-renamed"}, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
			t.Fatalf("admin sign-in with env %q = %d %q", env, resp.StatusCode, resp.Header.Get("Location"))
		}
		if h.count("users") != 1 {
			t.Fatalf("users = %d with env %q, want 1", h.count("users"), env)
		}
	}
}

func TestFirstAdminWithoutTheEnvValueIsTheAppOwner(t *testing.T) {
	t.Parallel()

	t.Run("user-owned App", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{ownerType: "User", ownerLogin: "App-Owner"})
		stranger := h.newClient()
		wantLoginError(t, h.signIn(stranger, github.User{ID: 42, Login: "stranger"}, "", false), auth.LoginErrorNoRole)
		if h.count("users") != 0 {
			t.Fatal("a stranger got a user row")
		}
		owner := h.newClient()
		if resp := h.signIn(owner, github.User{ID: 7, Login: "app-owner"}, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
			t.Fatalf("owner sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
		}
		if pinned, ok, _ := h.store.SignIn().BootstrapAdmin(context.Background()); !ok || pinned.ID != 7 {
			t.Fatalf("bootstrap admin = %+v, %v; want the owner's id 7", pinned, ok)
		}
		// Only while no admin exists: a later user with the owner's login name is refused.
		wantLoginError(t, h.signIn(h.newClient(), github.User{ID: 8, Login: "app-owner"}, "", false), auth.LoginErrorNoRole)
		if h.count("users") != 1 {
			t.Fatalf("users = %d, want 1", h.count("users"))
		}
	})

	t.Run("organization-owned App", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{ownerType: "Organization", ownerLogin: "acme"})
		member := h.newClient()
		wantLoginError(t, h.signIn(member, github.User{ID: 42, Login: "member"}, "", false), auth.LoginErrorNoRole)
		if h.count("users") != 0 {
			t.Fatal("an organization member who is no owner got a user row")
		}
		owner := h.newClient()
		if resp := h.signIn(owner, github.User{ID: 7, Login: "org-owner"}, "", true); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
			t.Fatalf("organization owner sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
		}
		// Once an admin exists, another owner is not let in through this item.
		wantLoginError(t, h.signIn(h.newClient(), github.User{ID: 8, Login: "second-owner"}, "", true), auth.LoginErrorNoRole)
		if h.count("users") != 1 {
			t.Fatalf("users = %d, want 1", h.count("users"))
		}
	})

	t.Run("the env login wins over the owner", func(t *testing.T) {
		t.Parallel()
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "chosen", ownerType: "User", ownerLogin: "app-owner"})
		wantLoginError(t, h.signIn(h.newClient(), github.User{ID: 7, Login: "app-owner"}, "", false), auth.LoginErrorNoRole)
		if resp := h.signIn(h.newClient(), github.User{ID: 9, Login: "chosen"}, "", false); resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
			t.Fatalf("env admin sign-in = %d %q", resp.StatusCode, resp.Header.Get("Location"))
		}
	})
}

func TestPostSignInRedirectIsAlwaysAFixedSameOriginPath(t *testing.T) {
	t.Parallel()
	for requested, want := range map[string]string{
		"/jobs?page=2":          "/jobs?page=2",
		"//evil.example":        "/",
		"/\\evil.example":       "/",
		"https://evil.example/": "/",
		"javascript:alert(1)":   "/",
		"evil":                  "/",
		"/ok\r\nSet-Cookie: x":  "/",
		"":                      "/",
	} {
		h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
		resp := h.signIn(h.newClient(), github.User{ID: 583231, Login: "octocat"}, requested, false)
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != want {
			t.Errorf("requested %q: Location = %q (%d), want %q", requested, resp.Header.Get("Location"), resp.StatusCode, want)
		}
	}
}

func TestLoginLinkSignsInTheBootstrapAdminExactlyOnce(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
	ctx := context.Background()
	users := h.store.SignIn()

	if _, err := auth.CreateLoginLink(ctx, users, signInAppURL, time.Now()); !errors.Is(err, store.ErrNoBootstrapAdmin) {
		t.Fatalf("link before any admin = %v, want ErrNoBootstrapAdmin", err)
	}
	h.signIn(h.newClient(), github.User{ID: 583231, Login: "octocat"}, "", false)

	link, err := auth.CreateLoginLink(ctx, users, signInAppURL, time.Now())
	if err != nil {
		t.Fatalf("CreateLoginLink: %v", err)
	}
	prefix := signInAppURL + "/login/link?token="
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link = %q, want it under %s", link, prefix)
	}
	raw := strings.TrimPrefix(link, prefix)

	var storedHash string
	if err := h.db.Writer.QueryRow(`SELECT token_hash FROM login_link_tokens`).Scan(&storedHash); err != nil {
		t.Fatalf("read link: %v", err)
	}
	if storedHash == raw || storedHash != auth.HashToken(raw) {
		t.Errorf("stored token = %q, want the SHA-256 of the raw token, never the raw token", storedHash)
	}
	var dbPath string
	_ = h.db.Writer.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&dbPath)
	for _, suffix := range []string{"", "-wal"} {
		if data, err := os.ReadFile(dbPath + suffix); err == nil && bytes.Contains(data, []byte(raw)) {
			t.Errorf("the database file%s contains the raw login link token", suffix)
		}
	}

	redeem := func(c *http.Client, token string) *http.Response {
		body, _ := json.Marshal(map[string]string{"token": token})
		resp, err := c.Post(h.srv.URL+"/auth/login-link", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST login-link: %v", err)
		}
		_ = resp.Body.Close()
		return resp
	}

	c := h.newClient()
	pre := h.sessionCookie(c)
	if resp := redeem(c, raw); resp.StatusCode != http.StatusOK {
		t.Fatalf("redeem = %d, want 200", resp.StatusCode)
	}
	if h.sessionCookie(c) == "" || h.sessionCookie(c) == pre {
		t.Error("redeeming did not issue a new session token")
	}
	if u := h.sessionUser(c); u["id"] != float64(583231) || u["role"] != "admin" {
		t.Errorf("session after redeeming = %v, want the bootstrap admin", u)
	}
	if resp := redeem(h.newClient(), raw); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("reused link = %d, want 400", resp.StatusCode)
	}

	expired, err := auth.CreateLoginLink(ctx, users, signInAppURL, time.Now().Add(-auth.LoginLinkTTL-time.Minute))
	if err != nil {
		t.Fatalf("CreateLoginLink: %v", err)
	}
	other := h.newClient()
	if resp := redeem(other, strings.TrimPrefix(expired, prefix)); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expired link = %d, want 400", resp.StatusCode)
	}
	if h.sessionUser(other) != nil {
		t.Error("an expired link produced a session")
	}
	for _, bad := range []string{"", "unknown-token", strings.Repeat("a", 300)} {
		if resp := redeem(h.newClient(), bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("token %q = %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestConcurrentLoginLinkRedemptionsStartOneSession(t *testing.T) {
	t.Parallel()
	h := newSignInHarness(t, harnessOptions{bootstrapLogin: "octocat"})
	h.signIn(h.newClient(), github.User{ID: 583231, Login: "octocat"}, "", false)
	link, err := auth.CreateLoginLink(context.Background(), h.store.SignIn(), signInAppURL, time.Now())
	if err != nil {
		t.Fatalf("CreateLoginLink: %v", err)
	}
	raw := strings.TrimPrefix(link, signInAppURL+"/login/link?token=")

	var ok, refused atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			c := h.newClient()
			body, _ := json.Marshal(map[string]string{"token": raw})
			resp, err := c.Post(h.srv.URL+"/auth/login-link", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Errorf("POST: %v", err)
				return
			}
			_ = resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusOK:
				if h.sessionUser(c) == nil {
					t.Error("a successful redemption left no session")
				}
				ok.Add(1)
			case http.StatusBadRequest:
				if h.sessionUser(c) != nil {
					t.Error("a refused redemption left a session")
				}
				refused.Add(1)
			default:
				t.Errorf("status = %d", resp.StatusCode)
			}
		})
	}
	wg.Wait()
	if ok.Load() != 1 || refused.Load() != 7 {
		t.Fatalf("sessions = %d, refused = %d; want 1 and 7", ok.Load(), refused.Load())
	}
}

func TestSafeRedirectPath(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"":                  "/",
		"/":                 "/",
		"/jobs":             "/jobs",
		"/jobs?view=a&b=c":  "/jobs?view=a&b=c",
		"/a#frag":           "/a#frag",
		"//evil.example":    "/",
		"/\\evil.example":   "/",
		"\\evil.example":    "/",
		"http://evil.test/": "/",
		"jobs":              "/",
		"/a\nb":             "/",
		"/a\x7fb":           "/",
		"/%0d%0aX":          "/%0d%0aX",
	} {
		if got := auth.SafeRedirectPath(in); got != want {
			t.Errorf("SafeRedirectPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPublicAuthPathsListTheGitHubSignInPaths(t *testing.T) {
	t.Parallel()
	for _, want := range []string{"/api/v1/auth/github/login", "/api/v1/auth/github/callback", "/api/v1/auth/login-link", "/api/v1/auth/session"} {
		found := false
		for _, p := range auth.PublicAuthPaths {
			found = found || p == want
		}
		if !found {
			t.Errorf("PublicAuthPaths is missing %s", want)
		}
	}
}
