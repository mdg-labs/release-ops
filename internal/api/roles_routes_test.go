package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type fixedCreds struct {
	appID int64
	pem   []byte
}

func (c fixedCreds) SigningKey(context.Context) (int64, []byte, error) {
	return c.appID, slices.Clone(c.pem), nil
}

const (
	rolesOwnerID  int64 = 101
	rolesMemberID int64 = 102
)

// rolesServer is the production router with the real role evaluation over a fake GitHub API:
// organization "acme" has an owner and a plain member, and the instance has two repositories.
type rolesServer struct {
	srv  *httptest.Server
	deps *ServerDeps
	fake *githubtest.Fake
	// budget is the rate-limit budget shared by the app's GitHub client and its tokens.
	budget *ratelimit.Budget
}

func newRolesServer(t *testing.T, user github.User, accessible ...int64) *rolesServer {
	t.Helper()
	deps := newRoutesTestDeps(t)
	deps.AppURL = "https://release-ops.example.com"
	deps.GitHubBaseURL = "https://github.example.com"
	deps.GitHubOAuth = stubOAuth{user: user, repoIDs: accessible}
	storetest.SeedRepositories(t, deps.Store.DB(), 2)
	fake := githubtest.New(t, time.Now)
	app := sampleSignInApp()
	app.AppID, app.PrivateKey = githubtest.AppID, string(fake.PEM)
	if err := deps.Store.GitHubApp().Create(context.Background(), app); err != nil {
		t.Fatalf("store app: %v", err)
	}

	fake.AddInstallation(1, githubtest.AppID)
	fake.SetUser(rolesOwnerID, "acme-owner")
	fake.SetUser(rolesMemberID, "acme-member")
	fake.SetOrganizationMember("acme", "acme-owner", "active", "admin")
	fake.SetOrganizationMember("acme", "acme-member", "active", "member")
	budget := ratelimit.NewBudget(ratelimit.Options{})
	api := github.NewClient(fake.URL, 5*time.Second).UseRateLimits(budget)
	deps.GitHub = api
	deps.GitHubApp = github.NewAppClient(api, fixedCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Budget: budget})
	deps.Roles = &auth.GitHubRoles{
		Access: deps.Store.Access(),
		Users:  deps.Store.SignIn(),
		GitHub: github.NewAppClient(api, fixedCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{}),
	}

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	return &rolesServer{srv: srv, deps: deps, fake: fake, budget: budget}
}

// signIn runs the whole GitHub sign-in through the router and returns the signed-in client.
func (s *rolesServer) signIn(t *testing.T) *http.Client {
	t.Helper()
	c := noRedirects(t)
	resp, err := c.Get(s.srv.URL + "/api/v1/auth/github/login")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	_ = resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, err = c.Get(s.srv.URL + "/api/v1/auth/github/callback?code=abc&state=" + url.QueryEscape(loc.Query().Get("state")))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback = %d %q, want 302 to /", resp.StatusCode, resp.Header.Get("Location"))
	}
	return c
}

func (s *rolesServer) do(t *testing.T, c *http.Client, method, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// ageSession makes the role of every stored session look last evaluated at the given time.
func ageSession(t *testing.T, deps *ServerDeps, evaluatedAt time.Time) {
	t.Helper()
	codec := scs.GobCodec{}
	rows, err := deps.DB.Query(`SELECT token, data FROM sessions`)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	type row struct {
		token string
		data  []byte
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.token, &r.data); err != nil {
			t.Fatalf("scan: %v", err)
		}
		all = append(all, r)
	}
	_ = rows.Close()
	for _, r := range all {
		deadline, values, err := codec.Decode(r.data)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		values[auth.SessionRoleCheckedKey] = evaluatedAt.Unix()
		data, err := codec.Encode(deadline, values)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if _, err := deps.DB.Exec(`UPDATE sessions SET data = ? WHERE token = ?`, data, r.token); err != nil {
			t.Fatalf("update session: %v", err)
		}
	}
}

// The routes a viewer may call. Every other route of /api/v1 that needs a session must answer a
// viewer 403, so a route added without the admin guard fails TestViewerGets403OnEveryAdminRoute.
var viewerRoutes = []string{
	"GET /api/v1/status",
	"GET /api/v1/about",
	"GET /api/v1/update-prs",
	"GET /api/v1/repositories",
	"GET /api/v1/repositories/{id}",
	"GET /api/v1/jobs",
	"GET /api/v1/jobs/{id}",
	"GET /api/v1/problems",
	"POST /api/v1/auth/logout", // last: it ends the session
}

func isPublicRoute(route string) bool {
	_, path, _ := strings.Cut(route, " ")
	return slices.Contains(auth.PublicAuthPaths, path)
}

func TestViewerGets403OnEveryAdminRoute(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	routes, err := ListRoutes(NewServerRouter(s.deps).(chi.Router))
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	checked := 0
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		if !strings.HasPrefix(path, apiV1Prefix+"/") || isPublicRoute(route) {
			continue
		}
		if slices.Contains(viewerRoutes, route) {
			continue
		}
		checked++
		path = strings.ReplaceAll(path, "{id}", "some-id")
		if code, body := s.do(t, viewer, method, path); code != http.StatusForbidden || !strings.Contains(body, `"code":"FORBIDDEN"`) {
			t.Errorf("viewer %s = %d %s, want 403 FORBIDDEN", route, code, body)
		}
	}
	if checked < 20 {
		t.Fatalf("only %d admin routes were checked; the route walk is not seeing the protected API", checked)
	}

	if _, err := s.deps.DB.Exec(`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		VALUES ('job-1', 'renovate_run', 'repo-1', 'normal', 'queued', '2026-11-16T08:00:00.000Z', 'schedule', '2026-11-16T08:00:00.000Z')`); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	for _, route := range viewerRoutes {
		method, path, _ := strings.Cut(route, " ")
		id := "repo-1"
		if strings.Contains(path, "/jobs/") {
			id = "job-1"
		}
		path = strings.ReplaceAll(path, "{id}", id)
		if code, body := s.do(t, viewer, method, path); code != http.StatusOK {
			t.Errorf("viewer %s = %d %s, want 200", route, code, body)
		}
	}
}

func TestAdminReachesTheAdminRoutes(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	for _, path := range []string{"/api/v1/status", "/api/v1/ticket-projects", "/api/v1/integrations"} {
		if code, body := s.do(t, admin, http.MethodGet, path); code != http.StatusOK {
			t.Errorf("admin GET %s = %d %s, want 200", path, code, body)
		}
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM session_repositories`); n != 0 {
		t.Errorf("an admin session stored %d repositories, want none", n)
	}
}

func membershipCalls(f *githubtest.Fake) int {
	n := 0
	for _, req := range f.Requests() {
		if strings.Contains(req.Path, "/memberships/") {
			n++
		}
	}
	return n
}

func rowCount(t *testing.T, deps *ServerDeps, query string) int {
	t.Helper()
	var n int
	if err := deps.DB.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestViewerSignInThroughTheRouterStoresTheScopeAndReportsTheRole(t *testing.T) {
	t.Parallel()
	// The member can access repository 1 on GitHub, not repository 2.
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	code, body := s.do(t, viewer, http.MethodGet, "/api/v1/auth/session")
	var session struct {
		User struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
			Role  string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &session); err != nil || code != http.StatusOK || session.User.Role != "viewer" || session.User.ID != rolesMemberID {
		t.Fatalf("GET /auth/session = %d %s, want the viewer", code, body)
	}

	rows, err := s.deps.DB.Query(`SELECT repository_id FROM session_repositories`)
	if err != nil {
		t.Fatalf("read scope: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	if !slices.Equal(ids, []string{"repo-1"}) {
		t.Fatalf("stored scope = %v, want [repo-1]", ids)
	}
}

func TestRemovalFromTheOrganizationEndsTheSessionAfterFifteenMinutes(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	// Within the window GitHub is not asked, so the removal is not seen yet.
	s.fake.RemoveOrganizationMember("acme", "acme-member")
	asked := membershipCalls(s.fake)
	if code, _ := s.do(t, viewer, http.MethodGet, "/api/v1/status"); code != http.StatusOK {
		t.Fatalf("GET /status inside the window = %d, want 200", code)
	}
	if got := membershipCalls(s.fake); got != asked {
		t.Fatalf("GitHub was asked %d more times inside the 15-minute window, want none", got-asked)
	}

	ageSession(t, s.deps, time.Now().Add(-auth.RoleRecheckInterval-time.Minute))
	if code, _ := s.do(t, viewer, http.MethodGet, "/api/v1/status"); code != http.StatusUnauthorized {
		t.Fatalf("GET /status after the removal and 16 minutes = %d, want 401", code)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want the session deleted", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM session_repositories`); n != 0 {
		t.Errorf("session_repositories = %d, want the list deleted with the session", n)
	}
}

func TestAnUnreachableGitHubAnswers503AfterFifteenMinutes(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	// The owner is no bootstrap admin, so the role needs GitHub; it is down.
	s.fake.FailMemberships(http.StatusBadGateway)
	if code, _ := s.do(t, admin, http.MethodGet, "/api/v1/integrations"); code != http.StatusOK {
		t.Fatalf("inside the window = %d, want 200", code)
	}
	ageSession(t, s.deps, time.Now().Add(-auth.RoleRecheckInterval-time.Minute))
	if code, _ := s.do(t, admin, http.MethodGet, "/api/v1/integrations"); code != http.StatusServiceUnavailable {
		t.Fatalf("with GitHub down after the window = %d, want 503: a stale role is not served", code)
	}
	s.fake.FailMemberships(0)
	if code, _ := s.do(t, admin, http.MethodGet, "/api/v1/integrations"); code != http.StatusOK {
		t.Fatalf("after GitHub came back = %d, want 200", code)
	}
}
