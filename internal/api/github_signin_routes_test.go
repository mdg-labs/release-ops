package api

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// stubOAuth is GitHub's side of the sign-in with one fixed user.
type stubOAuth struct {
	user github.User
	// repoIDs are the GitHub ids of the repositories the user can access.
	repoIDs []int64
}

func (stubOAuth) AuthorizeURL(clientID, redirectURI, state, challenge string) string {
	return "https://github.example/login/oauth/authorize?client_id=" + clientID + "&state=" + state + "&code_challenge=" + challenge
}

func (stubOAuth) ExchangeCode(context.Context, github.ExchangeParams) (github.UserToken, error) {
	return github.UserToken{}, nil
}

func (s stubOAuth) User(context.Context, github.UserToken) (github.User, error) { return s.user, nil }

func (stubOAuth) IsOrganizationOwner(context.Context, github.UserToken, string) (bool, error) {
	return false, nil
}

func (s stubOAuth) InstallationRepositoryIDs(context.Context, github.UserToken, int64) ([]int64, error) {
	return s.repoIDs, nil
}

func newSignInRoutesServer(t *testing.T) (*httptest.Server, *ServerDeps) {
	t.Helper()
	return newSignInRoutesServerWith(t, stubOAuth{user: github.User{ID: 583231, Login: "octocat"}})
}

func newSignInRoutesServerWith(t *testing.T, provider auth.OAuthProvider) (*httptest.Server, *ServerDeps) {
	t.Helper()
	deps := newRoutesTestDeps(t)
	deps.AppURL = "https://release-ops.example.com"
	deps.GitHubOAuth = provider
	deps.BootstrapAdminGitHubLogin = "octocat"
	if err := deps.Store.GitHubApp().Create(context.Background(), sampleSignInApp()); err != nil {
		t.Fatalf("store app: %v", err)
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	return srv, deps
}

func sampleSignInApp() store.NewGitHubApp {
	return store.NewGitHubApp{
		AppID: 1, Slug: "release-ops", ClientID: "client-id", HTMLURL: "https://github.com/apps/release-ops",
		OwnerLogin: "octocat", OwnerType: "User", PrivateKey: "pem", ClientSecret: "secret", WebhookSecret: "hook",
	}
}

func noRedirects(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func TestGitHubSignInRoutesAreRegistered(t *testing.T) {
	t.Parallel()
	srv, deps := newSignInRoutesServer(t)
	_ = srv
	paths, err := ListRoutes(NewServerRouter(deps).(chi.Router))
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	for _, want := range []string{"GET /api/v1/auth/github/login", "GET /api/v1/auth/github/callback", "POST /api/v1/auth/login-link"} {
		if !slices.Contains(paths, want) {
			t.Errorf("registered paths are missing %s", want)
		}
	}
}

// The whole sign-in through the production router: login start, callback, a session that opens
// a protected route, and logout.
func TestGitHubSignInThroughTheRouter(t *testing.T) {
	t.Parallel()
	srv, _ := newSignInRoutesServer(t)
	c := noRedirects(t)

	if resp, err := c.Get(srv.URL + "/api/v1/status"); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected route before sign-in = %v, %v; want 401", resp, err)
	}

	resp, err := c.Get(srv.URL + "/api/v1/auth/github/login?redirect=%2Fjobs")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if resp.StatusCode != http.StatusFound || loc.Host != "github.example" || loc.Query().Get("state") == "" {
		t.Fatalf("login start = %d %q, want 302 to the authorize URL with a state", resp.StatusCode, resp.Header.Get("Location"))
	}

	resp, err = c.Get(srv.URL + "/api/v1/auth/github/callback?code=abc&state=" + url.QueryEscape(loc.Query().Get("state")))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/jobs" {
		t.Fatalf("callback = %d %q, want 302 to /jobs", resp.StatusCode, resp.Header.Get("Location"))
	}

	if resp, err := c.Get(srv.URL + "/api/v1/status"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("protected route after sign-in = %v, %v; want 200", resp, err)
	}

	resp, err = c.Post(srv.URL+"/api/v1/auth/logout", "application/json", strings.NewReader("{}"))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("logout = %v, %v; want 200", resp, err)
	}
	if resp, err := c.Get(srv.URL + "/api/v1/status"); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected route after logout = %v, %v; want 401", resp, err)
	}
}

// Each sign-in endpoint allows 10 requests a minute per client address. X-Forwarded-For is
// honoured from the loopback peer (the bundled proxy) by its last hop only: hops the client
// put in front of it do not pick a bucket.
func TestGitHubSignInEndpointsAreRateLimitedPerClientAddress(t *testing.T) {
	t.Parallel()
	srv, _ := newSignInRoutesServer(t)

	do := func(method, path, forwardedFor string) *http.Response {
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(`{"token":"x"}`))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		resp, err := noRedirects(t).Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		_ = resp.Body.Close()
		if limited(resp) && resp.Header.Get("Retry-After") == "" {
			t.Errorf("%s %s was limited without Retry-After", method, path)
		}
		return resp
	}

	// The two endpoints a browser navigates to answer a limited request with a redirect to
	// /login, which shows the state; the break-glass POST is called with fetch and answers
	// 429 JSON.
	for _, ep := range []struct {
		method, path string
		navigation   bool
	}{
		{http.MethodGet, "/api/v1/auth/github/login", true},
		{http.MethodGet, "/api/v1/auth/github/callback?code=a&state=b", true},
		{http.MethodPost, "/api/v1/auth/login-link", false},
	} {
		for i := range 10 {
			// A different client-chosen first hop each time; the proxy's own hop is the last.
			if resp := do(ep.method, ep.path, "10.9.9."+strings.Repeat("1", i%3+1)+", 203.0.113.7"); limited(resp) {
				t.Fatalf("%s %s request %d was limited, want the first 10 allowed", ep.method, ep.path, i+1)
			}
		}
		resp := do(ep.method, ep.path, "198.51.100.1, 203.0.113.7")
		switch {
		case ep.navigation && (resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error=rate_limited"):
			t.Errorf("%s %s 11th request = %d to %q, want 302 to /login?error=rate_limited: a client-chosen leading hop must not pick a bucket",
				ep.method, ep.path, resp.StatusCode, resp.Header.Get("Location"))
		case !ep.navigation && resp.StatusCode != http.StatusTooManyRequests:
			t.Errorf("%s %s 11th request = %d, want 429", ep.method, ep.path, resp.StatusCode)
		}
		if resp := do(ep.method, ep.path, "203.0.113.99"); limited(resp) {
			t.Errorf("%s %s from another client address was limited", ep.method, ep.path)
		}
	}
}

// limited reports whether the response is the rate limiter's refusal for either kind of
// endpoint.
func limited(resp *http.Response) bool {
	return resp.StatusCode == http.StatusTooManyRequests || resp.Header.Get("Location") == "/login?error=rate_limited"
}

// A bad, expired or replayed state reaches the browser as a redirect to /login, not as JSON
// at the callback URL.
func TestGitHubCallbackRefusesABadStateWithARedirectThroughTheRouter(t *testing.T) {
	t.Parallel()
	srv, _ := newSignInRoutesServer(t)
	c := noRedirects(t)

	for name, path := range map[string]string{
		"no attempt":    "/api/v1/auth/github/callback?code=abc&state=anything",
		"missing state": "/api/v1/auth/github/callback?code=abc",
	} {
		resp, err := c.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/login?error=invalid_state" {
			t.Errorf("%s = %d to %q, want 302 to /login?error=invalid_state", name, resp.StatusCode, resp.Header.Get("Location"))
		}
	}
}

// slowOAuth holds the code exchange back, so every concurrent callback has read the pre-login
// session, still holding the state, before any of them finishes.
type slowOAuth struct {
	stubOAuth
	exchanges atomic.Int32
}

func (s *slowOAuth) ExchangeCode(ctx context.Context, p github.ExchangeParams) (github.UserToken, error) {
	s.exchanges.Add(1)
	time.Sleep(200 * time.Millisecond)
	return s.stubOAuth.ExchangeCode(ctx, p)
}

// Of concurrent callbacks that carry the same pre-login session and state, one signs in and
// reaches GitHub; the rest are refused with the invalid_state redirect.
func TestGitHubCallbackConcurrentRedemptionsLetOneWin(t *testing.T) {
	t.Parallel()
	gh := &slowOAuth{stubOAuth: stubOAuth{user: github.User{ID: 583231, Login: "octocat"}}}
	srv, _ := newSignInRoutesServerWith(t, gh)
	c := noRedirects(t)

	resp, err := c.Get(srv.URL + "/api/v1/auth/github/login")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatalf("login start = %d %q, want a state", resp.StatusCode, resp.Header.Get("Location"))
	}

	const requests = 8
	locations := make([]string, requests)
	statuses := make([]int, requests)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, err := c.Get(srv.URL + "/api/v1/auth/github/callback?code=abc&state=" + url.QueryEscape(state))
			if err != nil {
				return
			}
			_ = resp.Body.Close()
			statuses[i], locations[i] = resp.StatusCode, resp.Header.Get("Location")
		}()
	}
	close(start)
	wg.Wait()

	signedIn, refused := 0, 0
	for i := range requests {
		switch {
		case statuses[i] == http.StatusFound && locations[i] == "/":
			signedIn++
		case statuses[i] == http.StatusFound && locations[i] == "/login?error=invalid_state":
			refused++
		default:
			t.Errorf("callback %d = %d to %q", i, statuses[i], locations[i])
		}
	}
	if signedIn != 1 || refused != requests-1 {
		t.Errorf("%d callbacks signed in and %d were refused, want 1 and %d", signedIn, refused, requests-1)
	}
	if got := gh.exchanges.Load(); got != 1 {
		t.Errorf("GitHub exchanges = %d, want 1: only the winning callback may reach GitHub", got)
	}
}

// The other /api/v1 routes still need a session.
func TestSignInPathsDoNotOpenTheRestOfTheAPI(t *testing.T) {
	t.Parallel()
	srv, _ := newSignInRoutesServer(t)
	for _, route := range []string{"GET /api/v1/settings", "GET /api/v1/integrations", "GET /api/v1/users", "POST /api/v1/auth/logout"} {
		parts := strings.SplitN(route, " ", 2)
		req, _ := http.NewRequest(parts[0], srv.URL+parts[1], nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s = %d, want 401", route, resp.StatusCode)
		}
	}
}

// The callback the App registers at creation is the one the sign-in sends GitHub as
// redirect_uri, so the two cannot drift apart.
func TestManifestRegistersTheSignInCallback(t *testing.T) {
	t.Parallel()
	m := setup.BuildManifest("Release Ops", "https://release-ops.example.com", "")
	want := "https://release-ops.example.com" + auth.GitHubCallbackPath
	if len(m.CallbackURLs) != 1 || m.CallbackURLs[0] != want {
		t.Fatalf("manifest callback_urls = %v, want [%s]", m.CallbackURLs, want)
	}
}
