package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	_ "modernc.org/sqlite"
)

const routesTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var expectedAPIRoutes = []string{
	"POST /api/v1/auth/login",
	"GET /api/v1/auth/session",
	"POST /api/v1/auth/accept-invitation",
	"POST /api/v1/auth/confirm-email-change",
	"POST /api/v1/auth/logout",
	"GET /api/v1/status",
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
	"DELETE /api/v1/users/{id}",
	"POST /api/v1/users/me/email-change-request",
	"POST /api/v1/users/me/password-change",
	"GET /api/v1/users/invitations",
	"POST /api/v1/users/invitations",
	"DELETE /api/v1/users/invitations/{id}",
	"POST /api/v1/users/invitations/{id}/send-email",
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

func TestRemovedV1RoutesAreNotRegistered(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	seedRoutesTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedAppSettingsRow(t, deps.DB)
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	loginResp, err := client.Post(
		srv.URL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(`{"email":"admin@example.com","password":"secret-pass"}`),
	)
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}

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
		parts := strings.SplitN(route, " ", 2)
		req, err := http.NewRequest(parts[0], srv.URL+parts[1], nil)
		if err != nil {
			t.Fatalf("new request %s: %v", route, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s status = %d, want 404 or 405", route, resp.StatusCode)
		}
	}

	statusResp, err := client.Get(srv.URL + "/api/v1/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer func() { _ = statusResp.Body.Close() }()
	var status map[string]any
	if err := json.NewDecoder(statusResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if statusResp.StatusCode != http.StatusOK || len(status) != 1 || status["version"] == nil {
		t.Fatalf("status = %d %v, want 200 with only version", statusResp.StatusCode, status)
	}
}

// A v1 monitored repo still references a ticket project and a source integration until
// the v1 tables are dropped; deleting either through the API must answer 409, never 500.
func TestDeleteReferencedByLeftoverV1RowReturnsConflict(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	ctx := context.Background()
	seedRoutesTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedAppSettingsRow(t, deps.DB)

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
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at)
		 VALUES ('int-v1', 'github', 'GitHub v1', NULL, '', 't', 't')`,
		`INSERT INTO monitored_repos (id, source_kind, project_path, source_integration_id, ticket_project_id, created_at, updated_at)
		 VALUES ('repo-v1', 'github', 'org/repo', 'int-v1', '` + project.ID + `', 't', 't')`,
	} {
		if _, err := deps.DB.Exec(stmt); err != nil {
			t.Fatalf("seed v1 rows: %v", err)
		}
	}

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	loginResp, err := client.Post(
		srv.URL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(`{"email":"admin@example.com","password":"secret-pass"}`),
	)
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()

	del := func(path string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("DELETE %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if got := del("/api/v1/ticket-projects/" + project.ID); got != http.StatusConflict {
		t.Fatalf("delete referenced ticket project status = %d, want %d", got, http.StatusConflict)
	}
	if _, err := deps.Store.TicketProjects().Get(ctx, project.ID); err != nil {
		t.Fatalf("ticket project missing after refused delete: %v", err)
	}
	if got := del("/api/v1/integrations/int-v1"); got != http.StatusConflict {
		t.Fatalf("delete referenced source integration status = %d, want %d", got, http.StatusConflict)
	}
	if _, err := deps.Store.Integrations().Get(ctx, "int-v1"); err != nil {
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

	loginResp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(`{"email":"wrong@example.com","password":"bad"}`))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusUnauthorized)
	}

	acceptResp, err := http.Post(srv.URL+"/api/v1/auth/accept-invitation", "application/json", strings.NewReader(`{"token":"bad","password":"short"}`))
	if err != nil {
		t.Fatalf("POST accept-invitation: %v", err)
	}
	_ = acceptResp.Body.Close()
	if acceptResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("accept-invitation status = %d, want %d", acceptResp.StatusCode, http.StatusBadRequest)
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

	deps := newRoutesTestDeps(t)
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

	seedRoutesTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedAppSettingsRow(t, deps.DB)

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	loginResp, err := client.Post(
		srv.URL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(`{"email":"admin@example.com","password":"secret-pass"}`),
	)
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}

	testURL := srv.URL + "/api/v1/integrations/" + created.ID + "/test"
	testResp, err := client.Post(testURL, "application/json", nil)
	if err != nil {
		t.Fatalf("POST test: %v", err)
	}
	defer func() { _ = testResp.Body.Close() }()

	if testResp.StatusCode != http.StatusOK {
		t.Fatalf("test status = %d, want %d", testResp.StatusCode, http.StatusOK)
	}

	var body struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(testResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode test response: %v", err)
	}
	if !body.Success {
		t.Fatalf("success = false, message = %q", body.Message)
	}
}

func TestLoginThenGetSettingsReturns200(t *testing.T) {
	t.Parallel()

	deps := newRoutesTestDeps(t)
	seedRoutesTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedAppSettingsRow(t, deps.DB)

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	loginBody := `{"email":"admin@example.com","password":"secret-pass"}`
	loginResp, err := client.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}

	settingsResp, err := client.Get(srv.URL + "/api/v1/settings")
	if err != nil {
		t.Fatalf("GET settings: %v", err)
	}
	defer func() { _ = settingsResp.Body.Close() }()
	if settingsResp.StatusCode != http.StatusOK {
		t.Fatalf("settings status = %d, want %d", settingsResp.StatusCode, http.StatusOK)
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
		Mailer:  mail.NoopMailer{},
	}
}

func seedRoutesTestUser(t *testing.T, queries *storedb.Queries, email, password string) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
}

func seedAppSettingsRow(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, updated_at) VALUES (1, 360, ?)`,
		"2026-08-06T12:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("seed app_settings: %v", err)
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
