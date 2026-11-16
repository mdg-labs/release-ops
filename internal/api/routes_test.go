package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	_ "modernc.org/sqlite"
)

const routesTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var expectedAPIRoutes = []string{
	"GET /api/v1/auth/session",
	"POST /api/v1/auth/login-link",
	"POST /api/v1/auth/logout",
	"GET /api/v1/status",
	"GET /api/v1/update-prs",
	"GET /api/v1/jobs",
	"GET /api/v1/jobs/{id}",
	"GET /api/v1/problems",
	"GET /api/v1/settings",
	"PATCH /api/v1/settings",
	"GET /api/v1/integrations",
	"POST /api/v1/integrations",
	"PATCH /api/v1/integrations/{id}",
	"DELETE /api/v1/integrations/{id}",
	"POST /api/v1/integrations/{id}/test",
	"GET /api/v1/integrations/{id}/ticket-metadata/workspaces",
	"GET /api/v1/integrations/{id}/ticket-metadata/projects",
	"GET /api/v1/integrations/{id}/ticket-metadata/statuses",
	"GET /api/v1/integrations/{id}/ticket-metadata/priorities",
	"GET /api/v1/integrations/{id}/ticket-metadata/issue-types",
	"GET /api/v1/ticket-projects",
	"POST /api/v1/ticket-projects",
	"PATCH /api/v1/ticket-projects/{id}",
	"DELETE /api/v1/ticket-projects/{id}",
	"GET /api/v1/users",
}

func TestRegisterAllListsExpectedPaths(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	handler := NewServerRouter(deps)
	mux, ok := handler.(chi.Router)
	if !ok {
		t.Fatal("server router is not chi.Router")
	}

	paths, err := ListRoutes(mux)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}

	for _, expected := range expectedAPIRoutes {
		if !slices.Contains(paths, expected) {
			t.Fatalf("registered paths = %v, missing %s", paths, expected)
		}
	}

	seen := make(map[string]int)
	for _, path := range paths {
		if strings.HasPrefix(path, "GET /api/v1/") ||
			strings.HasPrefix(path, "POST /api/v1/") ||
			strings.HasPrefix(path, "PATCH /api/v1/") ||
			strings.HasPrefix(path, "DELETE /api/v1/") {
			seen[path]++
		}
	}
	for path, count := range seen {
		if count > 1 {
			t.Fatalf("duplicate route mount: %s registered %d times", path, count)
		}
	}

	for _, path := range paths {
		if strings.Contains(path, "/auth/register") {
			t.Fatalf("unexpected register route: %s", path)
		}
	}
}

// The v1 local-account routes, the invitation, password and email-change flows, are not
// registered: a request to one is answered 404, with or without a session, and is no handler.
func TestRemovedLocalAccountRoutesAreNotRegistered(t *testing.T) {
	t.Parallel()

	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	anonymous := noRedirects(t)

	routes := []string{
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/accept-invitation",
		"POST /api/v1/auth/forgot-password",
		"POST /api/v1/auth/reset-password",
		"POST /api/v1/auth/confirm-email-change",
		"DELETE /api/v1/users/some-id",
		"POST /api/v1/users/me/email-change-request",
		"POST /api/v1/users/me/password-change",
		"GET /api/v1/users/invitations",
		"POST /api/v1/users/invitations",
		"DELETE /api/v1/users/invitations/some-id",
		"POST /api/v1/users/invitations/some-id/send-email",
	}
	for _, route := range routes {
		method, path, _ := strings.Cut(route, " ")
		for name, client := range map[string]*http.Client{"admin": admin, "anonymous": anonymous} {
			if code, body := s.send(t, client, method, path, `{}`); code != http.StatusNotFound {
				t.Errorf("%s %s = %d %s, want 404", name, route, code, body)
			}
		}
	}

	registered, err := ListRoutes(NewServerRouter(s.deps).(chi.Router))
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	for _, route := range registered {
		for _, removed := range []string{"/auth/login ", "invitation", "password", "email-change"} {
			if strings.Contains(route+" ", removed) {
				t.Errorf("route %s is registered, want no local-account route", route)
			}
		}
	}
}

func TestRemovedV1RoutesAreNotRegistered(t *testing.T) {
	t.Parallel()

	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	client := s.signIn(t)

	for _, route := range []string{
		"POST /api/v1/poll/trigger",
		"GET /api/v1/poll/runs",
		"GET /api/v1/poll/runs/run-1",
		"GET /api/v1/repos",
		"POST /api/v1/repos",
		"PATCH /api/v1/repos/repo-1",
		"DELETE /api/v1/repos/repo-1",
		"GET /api/v1/notification-targets",
		"POST /api/v1/notification-targets",
		"PATCH /api/v1/notification-targets/nt-1",
		"DELETE /api/v1/notification-targets/nt-1",
		"POST /api/v1/notification-targets/nt-1/test",
	} {
		method, path, _ := strings.Cut(route, " ")
		code, _ := s.send(t, client, method, path, "")
		if code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 404 or 405", route, code)
		}
	}

	var status map[string]any
	code, body := s.send(t, client, http.MethodGet, "/api/v1/status", "")
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if code != http.StatusOK || len(status) != 1 || status["version"] == nil {
		t.Fatalf("status = %d %v, want 200 with only version", code, status)
	}
}

// An update ticket references the ticket project it was created in, and the ticket
// project references its integration; deleting either through the API must answer 409,
// never 500.
func TestDeleteReferencedRowReturnsConflict(t *testing.T) {
	t.Parallel()

	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	deps := s.deps
	ctx := context.Background()

	integration, err := deps.Store.Integrations().Create(ctx, store.CreateIntegrationInput{
		Kind:    "jira",
		Name:    "Jira",
		BaseURL: new("https://jira.example"),
		Secret:  []byte(`{"email":"a@b.example","api_token":"tok"}`),
	})
	if err != nil {
		t.Fatalf("Create integration: %v", err)
	}
	project, err := deps.Store.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
		IntegrationID:     integration.ID,
		ExternalProjectID: "DEV",
		Name:              "Jira DEV",
		CreateConfig:      `{}`,
		StatusMapping:     `{}`,
	})
	if err != nil {
		t.Fatalf("Create ticket project: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'node', 'https://example.test/pr/1', 'Update dep', 'renovate/dep', 'open', 't', 't', 't')`,
		`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, created_at, updated_at)
		 VALUES ('ut-1', 'pr-1', '` + project.ID + `', 'pending', 't', 't')`,
	} {
		if _, err := deps.DB.Exec(stmt); err != nil {
			t.Fatalf("seed update ticket rows: %v", err)
		}
	}

	client := s.signIn(t)

	del := func(path string) int {
		t.Helper()
		code, _ := s.send(t, client, http.MethodDelete, path, "")
		return code
	}

	if got := del("/api/v1/ticket-projects/" + project.ID); got != http.StatusConflict {
		t.Fatalf("delete referenced ticket project status = %d, want %d", got, http.StatusConflict)
	}
	if _, err := deps.Store.TicketProjects().Get(ctx, project.ID); err != nil {
		t.Fatalf("ticket project missing after refused delete: %v", err)
	}
	if got := del("/api/v1/integrations/" + integration.ID); got != http.StatusConflict {
		t.Fatalf("delete referenced integration status = %d, want %d", got, http.StatusConflict)
	}
	if _, err := deps.Store.Integrations().Get(ctx, integration.ID); err != nil {
		t.Fatalf("integration missing after refused delete: %v", err)
	}
}

func TestProtectedRoutesRequireSession(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	protected := []string{
		"GET /api/v1/settings",
		"GET /api/v1/status",
		"POST /api/v1/auth/logout",
	}
	for _, route := range protected {
		parts := strings.SplitN(route, " ", 2)
		req, err := http.NewRequest(parts[0], srv.URL+parts[1], nil)
		if err != nil {
			t.Fatalf("new request %s: %v", route, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want %d", route, resp.StatusCode, http.StatusUnauthorized)
		}
	}
}

func TestPublicAuthRoutesAccessibleWithoutSession(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	sessionResp, err := http.Get(srv.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	_ = sessionResp.Body.Close()
	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, want %d", sessionResp.StatusCode, http.StatusOK)
	}
}

func TestLoginLinkRateLimitIgnoresClientSuppliedForwardedForHops(t *testing.T) {
	t.Parallel()

	handler := NewServerRouter(newRoutesTestDeps(t))

	// The proxy's own hop is 203.0.113.9; the hop in front of it is whatever the client
	// sent, and rotating it must not open a fresh bucket.
	var last int
	for i := 0; i < 11; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login-link", strings.NewReader(`{"token":"bad"}`))
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i)+", 203.0.113.9")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("11th login-link status = %d, want %d", last, http.StatusTooManyRequests)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login-link", strings.NewReader(`{"token":"bad"}`))
	req.RemoteAddr = "127.0.0.1:40000"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("a different proxy-supplied address shares the exhausted bucket")
	}
}

func TestIntegrationTestConnectionWithRealTester(t *testing.T) {
	t.Parallel()

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin@example.com:jira_route_test"))
	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Fatalf("path = %q, want /rest/api/3/myself", r.URL.Path)
		}
		if r.Header.Get("Authorization") != wantAuth {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(mockProvider.Close)

	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	deps := s.deps
	deps.IntegrationTester = integrationtester.New(mockProvider.Client())

	created, err := deps.Store.Integrations().Create(context.Background(), store.CreateIntegrationInput{
		Kind:    "jira",
		Name:    "Jira Route Test",
		BaseURL: &mockProvider.URL,
		Secret:  []byte(`{"email":"admin@example.com","api_token":"jira_route_test"}`),
	})
	if err != nil {
		t.Fatalf("Create integration: %v", err)
	}

	// The router captures the tester when it is built, so the signed-in server is rebuilt on it.
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	s = &rolesServer{srv: srv, deps: deps, fake: s.fake}
	client := s.signIn(t)

	code, raw := s.send(t, client, http.MethodPost, "/api/v1/integrations/"+created.ID+"/test", "")
	if code != http.StatusOK {
		t.Fatalf("test status = %d %s, want %d", code, raw, http.StatusOK)
	}
	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if !body.Success {
		t.Fatalf("success = false, message = %q", body.Message)
	}
}

func newRoutesTestDeps(t *testing.T) *ServerDeps {
	t.Helper()

	db := openRoutesTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	cipher, err := crypto.NewCipherFromHex(routesTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}

	sm := auth.NewSessionManager(db.Writer, strings.Repeat("s", 32), false)
	return &ServerDeps{
		DB:      db.Writer,
		Session: sm,
		Queries: storedb.New(db.Writer),
		Store:   store.New(db, cipher),
		Queue:   store.NewJobQueue(db, store.JobQueueOptions{}),
	}
}

func openRoutesTestDB(t *testing.T) *store.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	return openRoutesTestDBAt(t, dbPath)
}

func openRoutesTestDBAt(t *testing.T, dbPath string) *store.DB {
	t.Helper()

	storetest.Migrate(t, dbPath)

	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	return db
}

// GET /users is the read-only list of the users who have signed in with GitHub: id, login and
// creation time, nothing of the removed local accounts, and no user row without a GitHub
// identity.
func TestUsersListShowsTheGitHubUsersOnly(t *testing.T) {
	t.Parallel()

	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if _, err := s.deps.DB.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('orphan', 't', 't')`); err != nil {
		t.Fatalf("seed a user without an identity: %v", err)
	}

	code, body := s.send(t, admin, http.MethodGet, "/api/v1/users", "")
	if code != http.StatusOK {
		t.Fatalf("GET /users = %d %s, want 200", code, body)
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v in %s", err, body)
	}
	if len(resp.Items) != 1 || resp.Items[0]["login"] != "acme-owner" || resp.Items[0]["id"] == "orphan" {
		t.Fatalf("items = %v, want only acme-owner", resp.Items)
	}
	if len(resp.Items[0]) != 3 {
		t.Fatalf("item = %v, want exactly id, login and createdAt", resp.Items[0])
	}
}
