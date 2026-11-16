package auth_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// sessionRouter serves the session endpoints over a real session store, with routes that put a
// session in place the way a sign-in does (/seed/github) and the way the removed local login did
// (/seed/local: a user id and a role, and no GitHub identity).
func sessionRouter(t *testing.T) *httptest.Server {
	t.Helper()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })
	seedSessionUser(t, db, "user-1")

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	authorizer := &auth.Authorizer{Sessions: sm}
	h := &auth.Handlers{SessionManager: sm, Authorizer: authorizer}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Post("/seed/github", func(w http.ResponseWriter, r *http.Request) {
		err := auth.StartGitHubSession(r.Context(), sm, auth.GitHubSession{
			UserID: "user-1", GitHubID: 583231, Login: "octocat", Role: auth.RoleAdmin, EvaluatedAt: time.Now(),
		})
		if err != nil {
			t.Errorf("start session: %v", err)
		}
	})
	r.Post("/seed/local", func(w http.ResponseWriter, r *http.Request) {
		if err := sm.RenewToken(r.Context()); err != nil {
			t.Errorf("renew token: %v", err)
		}
		sm.Put(r.Context(), auth.SessionUserIDKey, "user-1")
		sm.Put(r.Context(), auth.SessionRoleKey, auth.RoleAdmin)
	})
	r.Get("/auth/session", h.Session)
	r.With(apimw.RequireSession(sm)).Post("/auth/logout", h.Logout)
	r.With(apimw.Authenticate(authorizer, nil)).Get("/protected", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func newJarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func readSession(t *testing.T, c *http.Client, base string) (status int, user map[string]any) {
	t.Helper()
	resp, err := c.Get(base + "/auth/session")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		User map[string]any `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil && resp.StatusCode == http.StatusOK {
		t.Fatalf("decode session response: %v", err)
	}
	return resp.StatusCode, body.User
}

func TestSessionReturnsNullWhenLoggedOut(t *testing.T) {
	t.Parallel()

	srv := sessionRouter(t)
	status, user := readSession(t, newJarClient(t), srv.URL)
	if status != http.StatusOK || user != nil {
		t.Fatalf("session = %d %v, want 200 with a null user", status, user)
	}
}

func TestSessionReturnsTheGitHubUserWhenSignedIn(t *testing.T) {
	t.Parallel()

	srv := sessionRouter(t)
	c := newJarClient(t)
	resp, err := c.Post(srv.URL+"/seed/github", "application/json", nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = resp.Body.Close()

	status, user := readSession(t, c, srv.URL)
	if status != http.StatusOK || user["login"] != "octocat" || user["role"] != auth.RoleAdmin || user["id"] != float64(583231) {
		t.Fatalf("session = %d %v, want the GitHub user octocat as admin", status, user)
	}
	if _, ok := user["email"]; ok {
		t.Fatalf("session user = %v, want no email", user)
	}
}

// A session that names a user and carries a role but has no GitHub identity, the shape the
// removed local login stored, is no session: it is not served, and it is destroyed.
func TestASessionWithoutAGitHubIdentityIsRefused(t *testing.T) {
	t.Parallel()

	srv := sessionRouter(t)
	c := newJarClient(t)
	resp, err := c.Post(srv.URL+"/seed/local", "application/json", nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = resp.Body.Close()

	protected, err := c.Get(srv.URL + "/protected")
	if err != nil {
		t.Fatalf("GET protected: %v", err)
	}
	_ = protected.Body.Close()
	if protected.StatusCode != http.StatusUnauthorized {
		t.Fatalf("protected route with a session without a GitHub identity = %d, want 401", protected.StatusCode)
	}

	// The refusal destroyed the session: a repeat request carries no user either.
	if status, user := readSession(t, c, srv.URL); status != http.StatusOK || user != nil {
		t.Fatalf("session after the refusal = %d %v, want 200 with a null user", status, user)
	}

	// The session endpoint refuses it on its own, too.
	c2 := newJarClient(t)
	resp, err = c2.Post(srv.URL+"/seed/local", "application/json", nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = resp.Body.Close()
	if status, user := readSession(t, c2, srv.URL); status != http.StatusOK || user != nil {
		t.Fatalf("session endpoint with a session without a GitHub identity = %d %v, want 200 with a null user", status, user)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	t.Parallel()

	srv := sessionRouter(t)
	c := newJarClient(t)
	resp, err := c.Post(srv.URL+"/seed/github", "application/json", nil)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	_ = resp.Body.Close()

	logout, err := c.Post(srv.URL+"/auth/logout", "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	_ = logout.Body.Close()
	if logout.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", logout.StatusCode, http.StatusOK)
	}

	if status, user := readSession(t, c, srv.URL); status != http.StatusOK || user != nil {
		t.Fatalf("session after logout = %d %v, want 200 with a null user", status, user)
	}
}

func TestProtectedRouteReturns401WithoutCookie(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	srv := httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: storedb.New(db),
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/v1/auth/logout", "application/json", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"code":"unauthorized"`) {
		t.Fatalf("body = %s, want unauthorized error code", body)
	}
}

func TestRequireSessionAllowsAuthenticatedRequests(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)

	var protectedCalled bool
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
			protectedCalled = true
			w.WriteHeader(http.StatusOK)
		})
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/protected")
	if err != nil {
		t.Fatalf("GET protected: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if protectedCalled {
		t.Fatal("protected handler should not run without session")
	}
}

// Authorize itself reports a session without a GitHub identity as ErrUnauthenticated.
func TestAuthorizeRefusesARoleWithoutAGitHubIdentity(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })
	seedSessionUser(t, db, "user-1")

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	authorizer := &auth.Authorizer{Sessions: sm}
	var got error
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Get("/check", func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), auth.SessionUserIDKey, "user-1")
		sm.Put(r.Context(), auth.SessionRoleKey, auth.RoleAdmin)
		_, got = authorizer.Authorize(r.Context())
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/check")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()
	if !errors.Is(got, auth.ErrUnauthenticated) {
		t.Fatalf("Authorize = %v, want ErrUnauthenticated", got)
	}
}
