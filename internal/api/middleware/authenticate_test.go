package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	testGitHubID int64 = 4242
	testUserID         = "user-4242"
)

// fakeRoles answers the role evaluation the way GitHub's memberships would, and counts the
// questions.
type fakeRoles struct {
	mu    sync.Mutex
	role  string
	err   error
	calls int
}

func (f *fakeRoles) Evaluate(_ context.Context, githubID int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if githubID != testGitHubID {
		return "", nil
	}
	return f.role, f.err
}

func (f *fakeRoles) set(role string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.role, f.err = role, err
}

func (f *fakeRoles) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type authRig struct {
	t      *testing.T
	db     *store.DB
	sm     *scs.SessionManager
	roles  *fakeRoles
	clock  atomic.Int64
	srv    *httptest.Server
	client *http.Client
	repos  []string
}

type repoItem struct{ ID string }

// newAuthRig serves a sign-in stand-in, the protected group of the production wiring
// (Authenticate, then RequireAdmin for the admin routes), the repository list a scoped
// endpoint would serve, logout and the session endpoint.
func newAuthRig(t *testing.T) *authRig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st := store.New(db, cipher)
	a := &authRig{t: t, db: db, roles: &fakeRoles{}}
	a.repos = storetest.SeedRepositories(t, db, 2)
	a.clock.Store(time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC).Unix())
	if _, err := db.Writer.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES (?, 'x', 'x')`, testUserID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	a.sm = auth.NewSessionManager(db.Writer, strings.Repeat("s", 32), false)
	authorizer := &auth.Authorizer{Sessions: a.sm, Roles: a.roles, Now: func() time.Time { return time.Unix(a.clock.Load(), 0) }}
	handlers := &auth.Handlers{SessionManager: a.sm, Authorizer: authorizer}

	r := chi.NewRouter()
	r.Use(a.sm.LoadAndSave)
	r.Post("/signin", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var ids []string
		if q.Get("repos") != "" {
			ids = strings.Split(q.Get("repos"), ",")
		}
		if err := auth.StartGitHubSession(r.Context(), a.sm, auth.GitHubSession{
			UserID: testUserID, GitHubID: testGitHubID, Login: "octocat", Role: q.Get("role"),
			EvaluatedAt: time.Unix(a.clock.Load(), 0), RepositoryIDs: ids,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	r.Post("/signin-raw", func(w http.ResponseWriter, r *http.Request) {
		// A session as the previous release stored it: a user and a GitHub id, no role.
		a.sm.Put(r.Context(), auth.SessionUserIDKey, testUserID)
		a.sm.Put(r.Context(), auth.SessionGitHubIDKey, testGitHubID)
		if role := r.URL.Query().Get("role"); role != "" {
			a.sm.Put(r.Context(), auth.SessionRoleKey, role)
		}
	})
	r.Get("/auth/session", handlers.Session)
	r.With(apimw.RequireSession(a.sm)).Post("/auth/logout", handlers.Logout)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.Authenticate(authorizer, st.Access()))
		protected.Get("/me", func(w http.ResponseWriter, r *http.Request) {
			p, _ := apimw.PrincipalFromContext(r.Context())
			_ = json.NewEncoder(w).Encode(map[string]any{"role": p.Role, "user": p.UserID})
		})
		protected.Get("/repositories", func(w http.ResponseWriter, r *http.Request) {
			all := []repoItem{{"repo-1"}, {"repo-2"}}
			visible := apimw.FilterByRepository(apimw.ScopeFromContext(r.Context()), all, func(i repoItem) string { return i.ID })
			ids := []string{}
			for _, v := range visible {
				ids = append(ids, v.ID)
			}
			_ = json.NewEncoder(w).Encode(ids)
		})
		protected.Group(func(admin chi.Router) {
			admin.Use(apimw.RequireAdmin)
			admin.Get("/admin", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
		})
	})
	r.With(apimw.RequireAdmin).Get("/admin-unguarded", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	a.srv = httptest.NewServer(r)
	t.Cleanup(a.srv.Close)
	jar, _ := cookiejar.New(nil)
	a.client = &http.Client{Jar: jar}
	return a
}

func (a *authRig) do(method, path string) (int, string) {
	a.t.Helper()
	req, err := http.NewRequest(method, a.srv.URL+path, nil)
	if err != nil {
		a.t.Fatalf("new request: %v", err)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func (a *authRig) signIn(role string, repos ...string) {
	a.t.Helper()
	path := "/signin?role=" + role
	if len(repos) > 0 {
		path += "&repos=" + strings.Join(repos, ",")
	}
	if code, body := a.do(http.MethodPost, path); code != http.StatusOK {
		a.t.Fatalf("sign-in = %d %s", code, body)
	}
	a.roles.set(role, nil)
}

func (a *authRig) advance(d time.Duration) { a.clock.Add(int64(d / time.Second)) }

func (a *authRig) count(query string, args ...any) int {
	a.t.Helper()
	var n int
	if err := a.db.Writer.QueryRow(query, args...).Scan(&n); err != nil {
		a.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (a *authRig) repositories() []string {
	a.t.Helper()
	code, body := a.do(http.MethodGet, "/repositories")
	if code != http.StatusOK {
		a.t.Fatalf("GET /repositories = %d %s", code, body)
	}
	var ids []string
	if err := json.Unmarshal([]byte(body), &ids); err != nil {
		a.t.Fatalf("decode %q: %v", body, err)
	}
	return ids
}

func TestViewerGets403OnAdminRoutesAndAdminDoesNot(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)

	a.signIn(auth.RoleViewer, "repo-1")
	if code, body := a.do(http.MethodGet, "/me"); code != http.StatusOK || !strings.Contains(body, `"role":"viewer"`) {
		t.Fatalf("viewer GET /me = %d %s, want 200", code, body)
	}
	code, body := a.do(http.MethodGet, "/admin")
	if code != http.StatusForbidden || !strings.Contains(body, `"code":"FORBIDDEN"`) {
		t.Fatalf("viewer GET /admin = %d %s, want 403 FORBIDDEN", code, body)
	}

	a.signIn(auth.RoleAdmin)
	if code, _ := a.do(http.MethodGet, "/admin"); code != http.StatusNoContent {
		t.Fatalf("admin GET /admin = %d, want 204", code)
	}
}

func TestRequireAdminWithoutAPrincipalIsUnauthenticated(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleAdmin)
	// A route mounted behind RequireAdmin alone never ran Authenticate: no principal, no entry.
	if code, _ := a.do(http.MethodGet, "/admin-unguarded"); code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: a role guard cannot trust a role nobody authenticated", code)
	}
}

func TestViewerSeesOnlyRepositoriesInTheirScope(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)

	a.signIn(auth.RoleViewer, "repo-1")
	if got := a.repositories(); !slices.Equal(got, []string{"repo-1"}) {
		t.Fatalf("viewer scoped to repo-1 sees %v, want only repo-1", got)
	}

	a.signIn(auth.RoleAdmin)
	if got := a.repositories(); !slices.Equal(got, []string{"repo-1", "repo-2"}) {
		t.Fatalf("admin sees %v, want both repositories", got)
	}
}

func TestScopeAllowsAndFiltersByRepository(t *testing.T) {
	t.Parallel()
	limited := apimw.RepositoryScope([]string{"a", "c"})
	for id, want := range map[string]bool{"a": true, "b": false, "c": true, "": false} {
		if got := limited.Allows(id); got != want {
			t.Errorf("limited.Allows(%q) = %v, want %v", id, got, want)
		}
	}
	if got := apimw.FilterByRepository(limited, []string{"a", "b", "c"}, func(s string) string { return s }); !slices.Equal(got, []string{"a", "c"}) {
		t.Errorf("filtered = %v, want [a c]", got)
	}
	if ids, ok := limited.IDs(); !ok || len(ids) != 2 {
		t.Errorf("IDs = %v, %v, want the two ids of a limited scope", ids, ok)
	}

	if !apimw.AllRepositories().Allows("anything") || !apimw.AllRepositories().All() {
		t.Error("the all-repositories scope must allow every repository")
	}
	if _, ok := apimw.AllRepositories().IDs(); ok {
		t.Error("the all-repositories scope has no id list to filter by")
	}

	var zero apimw.Scope
	if zero.Allows("a") || zero.All() || len(apimw.FilterByRepository(zero, []string{"a"}, func(s string) string { return s })) != 0 {
		t.Error("the zero scope must allow nothing")
	}
	if apimw.ScopeFromContext(context.Background()).Allows("a") {
		t.Error("a request that never got a scope must see nothing")
	}
}

// The failure scenario of the schema change: a viewer session with no stored list must see
// no repository, and the list must not outlive its session.
func TestAViewerSessionWithoutAListSeesNothingAndTheListDiesWithTheSession(t *testing.T) {
	t.Parallel()

	t.Run("no list means no repositories", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer)
		if got := a.repositories(); len(got) != 0 {
			t.Fatalf("a viewer without a stored list sees %v, want nothing", got)
		}
	})

	t.Run("the list is stored in the table and not in the session data", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1", "repo-2")
		if n := a.count(`SELECT COUNT(*) FROM session_repositories`); n != 2 {
			t.Fatalf("session_repositories rows = %d, want 2", n)
		}
		var blob []byte
		if err := a.db.Writer.QueryRow(`SELECT data FROM sessions`).Scan(&blob); err != nil {
			t.Fatalf("read session: %v", err)
		}
		if bytes.Contains(blob, []byte("repo-1")) || bytes.Contains(blob, []byte(auth.SessionScopeKey)) {
			t.Error("the session data holds the repository list; the table is its only place")
		}
	})

	t.Run("logout deletes the list", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1")
		if code, body := a.do(http.MethodPost, "/auth/logout"); code != http.StatusOK {
			t.Fatalf("logout = %d %s", code, body)
		}
		if n := a.count(`SELECT COUNT(*) FROM session_repositories`); n != 0 {
			t.Fatalf("session_repositories rows after logout = %d, want 0", n)
		}
	})

	t.Run("deleting an expired session deletes the list", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1", "repo-2")
		// The store's own cleanup is this statement.
		if _, err := a.db.Writer.Exec(`DELETE FROM sessions`); err != nil {
			t.Fatalf("delete sessions: %v", err)
		}
		if n := a.count(`SELECT COUNT(*) FROM session_repositories`); n != 0 {
			t.Fatalf("session_repositories rows after the session went = %d, want 0", n)
		}
	})

	t.Run("a repository that left the installation drops out of the list", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1", "repo-2")
		if _, err := a.db.Writer.Exec(`UPDATE repositories SET removed_at = '2026-11-14T12:00:00.000Z' WHERE id = 'repo-2'`); err != nil {
			t.Fatalf("remove repository: %v", err)
		}
		if got := a.repositories(); !slices.Equal(got, []string{"repo-1"}) {
			t.Fatalf("viewer sees %v, want only the repository still installed", got)
		}
	})

	t.Run("a session renewed at sign-in leaves nothing of the old token", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1")
		a.signIn(auth.RoleViewer, "repo-2")
		if n := a.count(`SELECT COUNT(DISTINCT session_token) FROM session_repositories`); n != 1 {
			t.Fatalf("sessions with a list = %d, want only the live one", n)
		}
		if got := a.repositories(); !slices.Equal(got, []string{"repo-2"}) {
			t.Fatalf("viewer sees %v, want the list of the latest sign-in only", got)
		}
	})

	t.Run("a later commit of the same session keeps the list", func(t *testing.T) {
		t.Parallel()
		a := newAuthRig(t)
		a.signIn(auth.RoleViewer, "repo-1")
		// This request re-evaluates and writes the session back without a new list.
		a.advance(auth.RoleRecheckInterval + time.Minute)
		if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusOK {
			t.Fatalf("GET /me = %d", code)
		}
		if got := a.repositories(); !slices.Equal(got, []string{"repo-1"}) {
			t.Fatalf("viewer sees %v after the session was written again, want the list kept", got)
		}
	})
}

func TestRoleIsReEvaluatedAtMostEveryFifteenMinutes(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleViewer, "repo-1")

	for range 3 {
		a.advance(4 * time.Minute)
		if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusOK {
			t.Fatalf("GET /me = %d", code)
		}
	}
	if n := a.roles.count(); n != 0 {
		t.Fatalf("GitHub was asked %d times in 12 minutes, want none: the role is fresh for 15", n)
	}

	a.advance(4 * time.Minute) // 16 minutes after sign-in
	if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusOK {
		t.Fatalf("GET /me = %d", code)
	}
	if n := a.roles.count(); n != 1 {
		t.Fatalf("after 16 minutes GitHub was asked %d times, want once", n)
	}

	a.advance(10 * time.Minute)
	a.do(http.MethodGet, "/me")
	if n := a.roles.count(); n != 1 {
		t.Fatalf("10 minutes after a re-check GitHub was asked %d times in total, want still once", n)
	}

	a.advance(6 * time.Minute)
	a.do(http.MethodGet, "/me")
	if n := a.roles.count(); n != 2 {
		t.Fatalf("16 minutes after a re-check GitHub was asked %d times in total, want twice", n)
	}
}

func TestLosingMembershipEndsTheSessionOnTheNextRequest(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleViewer, "repo-1")
	if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusOK {
		t.Fatalf("GET /me before = %d", code)
	}

	a.roles.set("", nil) // removed from the organization, or from VIEWER_TEAM
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusUnauthorized {
		t.Fatalf("GET /me after losing membership = %d, want 401", code)
	}
	if n := a.count(`SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want the session deleted", n)
	}
	if n := a.count(`SELECT COUNT(*) FROM session_repositories`); n != 0 {
		t.Errorf("session_repositories rows = %d, want the list deleted with the session", n)
	}

	// Membership coming back does not bring the session back.
	a.roles.set(auth.RoleViewer, nil)
	if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusUnauthorized {
		t.Fatalf("GET /me after the membership returned = %d, want 401: a new sign-in is needed", code)
	}
}

func TestAnUnreachableGitHubIsNeverServedAsAFreshRole(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleAdmin)
	a.roles.set("", errors.New("github unreachable"))

	a.advance(auth.RoleRecheckInterval + time.Minute)
	for i := range 2 {
		code, body := a.do(http.MethodGet, "/admin")
		if code != http.StatusServiceUnavailable {
			t.Fatalf("request %d with GitHub down = %d %s, want 503", i+1, code, body)
		}
	}
	if n := a.count(`SELECT COUNT(*) FROM sessions`); n != 1 {
		t.Errorf("sessions = %d, want the session kept: an outage is not a loss of membership", n)
	}

	// The role is served again as soon as it can be evaluated; the session is not extended.
	a.roles.set(auth.RoleAdmin, nil)
	if code, _ := a.do(http.MethodGet, "/admin"); code != http.StatusNoContent {
		t.Fatalf("GET /admin after GitHub came back = %d, want 204", code)
	}
}

func TestARoleChangeFromAdminToViewerTakesEffectWithoutANewSignIn(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleAdmin)
	if code, _ := a.do(http.MethodGet, "/admin"); code != http.StatusNoContent {
		t.Fatalf("admin GET /admin = %d", code)
	}

	a.roles.set(auth.RoleViewer, nil)
	a.advance(auth.RoleRecheckInterval + time.Minute)
	code, body := a.do(http.MethodGet, "/admin")
	if code != http.StatusForbidden {
		t.Fatalf("GET /admin after demotion = %d %s, want 403", code, body)
	}
	// No list was stored for the admin, and none is invented: the demoted user sees no
	// repository until they sign in again and GitHub gives their list.
	if got := a.repositories(); len(got) != 0 {
		t.Fatalf("demoted user sees %v, want nothing", got)
	}
}

func TestARoleChangeDropsAListFromAnotherTime(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleViewer, "repo-1")

	a.roles.set(auth.RoleAdmin, nil)
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if got := a.repositories(); len(got) != 2 {
		t.Fatalf("promoted user sees %v, want both repositories", got)
	}
	if n := a.count(`SELECT COUNT(*) FROM session_repositories`); n != 0 {
		t.Fatalf("session_repositories rows = %d, want the viewer list dropped on the change", n)
	}

	a.roles.set(auth.RoleViewer, nil)
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if got := a.repositories(); len(got) != 0 {
		t.Fatalf("user demoted again sees %v, want nothing, not the list from the first viewer session", got)
	}
}

func TestASessionWithoutAKnownRoleIsRefusedAndDestroyed(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"", "owner", "ADMIN", "root"} {
		a := newAuthRig(t)
		path := "/signin-raw"
		if role != "" {
			path += "?role=" + role
		}
		if code, _ := a.do(http.MethodPost, path); code != http.StatusOK {
			t.Fatalf("seed session: %d", code)
		}
		if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusUnauthorized {
			t.Errorf("role %q: GET /me = %d, want 401: a session whose role is not known is never admin", role, code)
		}
		if code, _ := a.do(http.MethodGet, "/admin"); code != http.StatusUnauthorized {
			t.Errorf("role %q: GET /admin = %d, want 401", role, code)
		}
		if n := a.count(`SELECT COUNT(*) FROM sessions`); n != 0 {
			t.Errorf("role %q: sessions = %d, want it destroyed", role, n)
		}
	}
}

func TestSessionEndpointReportsTheCurrentRoleAndEndsASessionThatLostAccess(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	a.signIn(auth.RoleViewer, "repo-1")

	code, body := a.do(http.MethodGet, "/auth/session")
	if code != http.StatusOK || !strings.Contains(body, `"role":"viewer"`) || !strings.Contains(body, `"id":4242`) {
		t.Fatalf("GET /auth/session = %d %s, want the viewer's id and role", code, body)
	}

	a.roles.set(auth.RoleAdmin, nil)
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if _, body := a.do(http.MethodGet, "/auth/session"); !strings.Contains(body, `"role":"admin"`) {
		t.Fatalf("GET /auth/session after promotion = %s, want role admin without a new sign-in", body)
	}

	a.roles.set("", nil)
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if code, body := a.do(http.MethodGet, "/auth/session"); code != http.StatusOK || !strings.Contains(body, `"user":null`) {
		t.Fatalf("GET /auth/session after losing access = %d %s, want user null", code, body)
	}
	if n := a.count(`SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want the session deleted", n)
	}

	a.signIn(auth.RoleViewer)
	a.roles.set("", errors.New("down"))
	a.advance(auth.RoleRecheckInterval + time.Minute)
	if code, _ := a.do(http.MethodGet, "/auth/session"); code != http.StatusServiceUnavailable {
		t.Fatalf("GET /auth/session with GitHub down = %d, want 503, not a stale role", code)
	}
}

func TestSessionLifetimeIsCappedAtEightHours(t *testing.T) {
	t.Parallel()
	a := newAuthRig(t)
	if a.sm.Lifetime != 8*time.Hour || auth.SessionLifetime != 8*time.Hour {
		t.Fatalf("lifetime = %v, want 8h", a.sm.Lifetime)
	}
	a.signIn(auth.RoleAdmin)

	remaining := func() float64 {
		var hours float64
		if err := a.db.Writer.QueryRow(`SELECT (expiry - julianday('now')) * 24 FROM sessions`).Scan(&hours); err != nil {
			t.Fatalf("read expiry: %v", err)
		}
		return hours
	}
	if h := remaining(); h <= 7.9 || h > 8.001 {
		t.Fatalf("session expires in %.3f hours, want 8", h)
	}

	// A re-evaluation writes the session again and must not move the end of its life.
	a.advance(auth.RoleRecheckInterval + time.Minute)
	before := remaining()
	if code, _ := a.do(http.MethodGet, "/me"); code != http.StatusOK {
		t.Fatalf("GET /me = %d", code)
	}
	if after := remaining(); after < before-0.01 || after > before+0.001 {
		t.Fatalf("expiry moved from %.5f to %.5f hours by a re-evaluation, want unchanged", before, after)
	}
}
