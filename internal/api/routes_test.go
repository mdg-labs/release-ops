package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	_ "modernc.org/sqlite"
)

const routesTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

var expectedAPIRoutes = []string{
	"POST /api/v1/auth/login",
	"GET /api/v1/auth/session",
	"POST /api/v1/auth/logout",
	"GET /api/v1/status",
	"POST /api/v1/poll/trigger",
	"GET /api/v1/poll/runs",
	"GET /api/v1/poll/runs/{id}",
	"GET /api/v1/settings",
	"PATCH /api/v1/settings",
	"GET /api/v1/integrations",
	"POST /api/v1/integrations",
	"PATCH /api/v1/integrations/{id}",
	"DELETE /api/v1/integrations/{id}",
	"POST /api/v1/integrations/{id}/test",
	"GET /api/v1/ticket-projects",
	"POST /api/v1/ticket-projects",
	"PATCH /api/v1/ticket-projects/{id}",
	"DELETE /api/v1/ticket-projects/{id}",
	"GET /api/v1/repos",
	"POST /api/v1/repos",
	"PATCH /api/v1/repos/{id}",
	"DELETE /api/v1/repos/{id}",
	"GET /api/v1/notification-targets",
	"POST /api/v1/notification-targets",
	"PATCH /api/v1/notification-targets/{id}",
	"DELETE /api/v1/notification-targets/{id}",
	"POST /api/v1/notification-targets/{id}/test",
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
}

func TestIntegrationTestConnectionWithRealTester(t *testing.T) {
	t.Parallel()

	mockProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" {
			t.Fatalf("path = %q, want /api/v4/user", r.URL.Path)
		}
		if r.Header.Get("PRIVATE-TOKEN") != "glpat_route_test" {
			t.Fatalf("PRIVATE-TOKEN = %q", r.Header.Get("PRIVATE-TOKEN"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(mockProvider.Close)

	deps := newRoutesTestDeps(t)
	deps.IntegrationTester = integrationtester.New(mockProvider.Client())

	created, err := deps.Store.Integrations().Create(context.Background(), store.CreateIntegrationInput{
		Kind:    "gitlab",
		Name:    "GitLab Route Test",
		BaseURL: &mockProvider.URL,
		Secret:  []byte(`{"token":"glpat_route_test"}`),
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

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	return &ServerDeps{
		DB:      db,
		Session: sm,
		Queries: storedb.New(db),
		Store:   store.New(db, cipher),
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

func openRoutesTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	return openRoutesTestDBAt(t, dbPath)
}

func openRoutesTestDBAt(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	migrationsURL := routesTestMigrationSourceURL(t)
	m, err := newRoutesTestMigrator(t, dbPath, migrationsURL)
	if err != nil {
		t.Fatalf("newMigrator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = m.Close()
	})
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("ping sqlite: %v", err)
	}
	return db
}

func routesTestMigrationSourceURL(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return "file://" + filepath.Join(root, "migrations")
}

func newRoutesTestMigrator(t *testing.T, dbPath, migrationsURL string) (*migrate.Migrate, error) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return nil, err
	}

	return migrate.NewWithDatabaseInstance(migrationsURL, "sqlite", driver)
}
