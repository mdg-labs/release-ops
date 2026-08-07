package handlers_test

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
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/mail"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	_ "modernc.org/sqlite"
)

const usersTestKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestDeleteUserRevokesSessions(t *testing.T) {
	t.Parallel()

	deps := newUsersTestDeps(t)
	adminID := seedUsersTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	targetID := seedUsersTestUser(t, deps.Queries, "target@example.com", "secret-pass")
	seedUsersAppSettings(t, deps.DB)

	srv := httptest.NewServer(api.NewServerRouter(deps))
	t.Cleanup(srv.Close)

	adminClient := loginUsersTestClient(t, srv.URL, "admin@example.com", "secret-pass")
	targetClient := loginUsersTestClient(t, srv.URL, "target@example.com", "secret-pass")

	var targetSessionsBefore int
	if err := deps.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, targetID).Scan(&targetSessionsBefore); err != nil {
		t.Fatalf("count target sessions before: %v", err)
	}
	if targetSessionsBefore == 0 {
		t.Fatal("expected target session row with user_id")
	}

	deleteURL := srv.URL + "/api/v1/users/" + targetID
	deleteReq, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
	if err != nil {
		t.Fatalf("new delete request: %v", err)
	}
	deleteResp, err := adminClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("DELETE user: %v", err)
	}
	_ = deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", deleteResp.StatusCode, http.StatusOK)
	}

	var targetSessionsAfter int
	if err := deps.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, targetID).Scan(&targetSessionsAfter); err != nil {
		t.Fatalf("count target sessions after: %v", err)
	}
	if targetSessionsAfter != 0 {
		t.Fatalf("target sessions after delete = %d, want 0", targetSessionsAfter)
	}

	settingsResp, err := targetClient.Get(srv.URL + "/api/v1/settings")
	if err != nil {
		t.Fatalf("target GET settings: %v", err)
	}
	_ = settingsResp.Body.Close()
	if settingsResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("target session status = %d, want %d", settingsResp.StatusCode, http.StatusUnauthorized)
	}

	selfDeleteURL := srv.URL + "/api/v1/users/" + adminID
	selfReq, err := http.NewRequest(http.MethodDelete, selfDeleteURL, nil)
	if err != nil {
		t.Fatalf("new self-delete request: %v", err)
	}
	selfResp, err := adminClient.Do(selfReq)
	if err != nil {
		t.Fatalf("DELETE self: %v", err)
	}
	_ = selfResp.Body.Close()
	if selfResp.StatusCode != http.StatusForbidden {
		t.Fatalf("self-delete status = %d, want %d", selfResp.StatusCode, http.StatusForbidden)
	}
}

func TestLoginSetsSessionUserID(t *testing.T) {
	t.Parallel()

	deps := newUsersTestDeps(t)
	userID := seedUsersTestUser(t, deps.Queries, "admin@example.com", "secret-pass")

	srv := httptest.NewServer(api.NewServerRouter(deps))
	t.Cleanup(srv.Close)

	loginResp, err := http.Post(
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

	var sessionCount int
	if err := deps.DB.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount == 0 {
		t.Fatal("expected sessions.user_id to be set after login")
	}
}

func TestAcceptInvitationCreatesUserAndSession(t *testing.T) {
	t.Parallel()

	deps := newUsersTestDeps(t)
	seedUsersTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedUsersAppSettings(t, deps.DB)

	srv := httptest.NewServer(api.NewServerRouter(deps))
	t.Cleanup(srv.Close)

	adminClient := loginUsersTestClient(t, srv.URL, "admin@example.com", "secret-pass")
	createResp, err := adminClient.Post(
		srv.URL+"/api/v1/users/invitations",
		"application/json",
		strings.NewReader(`{"email":"newuser@example.com"}`),
	)
	if err != nil {
		t.Fatalf("POST invitation: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create invitation status = %d, want %d", createResp.StatusCode, http.StatusCreated)
	}

	var created struct {
		InviteURL string `json:"inviteUrl"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invitation: %v", err)
	}
	token := strings.TrimPrefix(strings.Split(created.InviteURL, "token=")[1], "")

	acceptResp, err := http.Post(
		srv.URL+"/api/v1/auth/accept-invitation",
		"application/json",
		strings.NewReader(`{"token":"`+token+`","password":"new-pass-1"}`),
	)
	if err != nil {
		t.Fatalf("POST accept-invitation: %v", err)
	}
	defer func() { _ = acceptResp.Body.Close() }()
	if acceptResp.StatusCode != http.StatusOK {
		t.Fatalf("accept status = %d, want %d", acceptResp.StatusCode, http.StatusOK)
	}

	var acceptBody struct {
		User struct {
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(acceptResp.Body).Decode(&acceptBody); err != nil {
		t.Fatalf("decode accept: %v", err)
	}
	if acceptBody.User.Email != "newuser@example.com" {
		t.Fatalf("accepted email = %q", acceptBody.User.Email)
	}
}

func TestInvitationSendEmailReturns503WithoutSMTP(t *testing.T) {
	t.Parallel()

	deps := newUsersTestDeps(t)
	seedUsersTestUser(t, deps.Queries, "admin@example.com", "secret-pass")
	seedUsersAppSettings(t, deps.DB)

	srv := httptest.NewServer(api.NewServerRouter(deps))
	t.Cleanup(srv.Close)

	adminClient := loginUsersTestClient(t, srv.URL, "admin@example.com", "secret-pass")
	createResp, err := adminClient.Post(
		srv.URL+"/api/v1/users/invitations",
		"application/json",
		strings.NewReader(`{"email":"mail@example.com"}`),
	)
	if err != nil {
		t.Fatalf("POST invitation: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode invitation: %v", err)
	}

	sendResp, err := adminClient.Post(srv.URL+"/api/v1/users/invitations/"+created.ID+"/send-email", "application/json", nil)
	if err != nil {
		t.Fatalf("POST send-email: %v", err)
	}
	defer func() { _ = sendResp.Body.Close() }()
	if sendResp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("send-email status = %d, want %d", sendResp.StatusCode, http.StatusServiceUnavailable)
	}
}

func newUsersTestDeps(t *testing.T) *api.ServerDeps {
	t.Helper()

	db := openUsersTestDB(t)
	t.Cleanup(func() { _ = db.Close() })

	cipher, err := crypto.NewCipherFromHex(usersTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	return &api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: storedb.New(db),
		Store:   store.New(db, cipher),
		Mailer:  mail.NoopMailer{},
	}
}

func seedUsersTestUser(t *testing.T, queries *storedb.Queries, email, password string) string {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	id := uuid.NewString()
	if _, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           id,
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return id
}

func seedUsersAppSettings(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, invite_token_expiry_hours, password_reset_token_expiry_minutes, updated_at)
		 VALUES (1, 360, 168, 60, ?)
		 ON CONFLICT(id) DO NOTHING`,
		"2026-08-07T12:00:00.000Z",
	)
	if err != nil {
		t.Fatalf("seed app_settings: %v", err)
	}
}

func loginUsersTestClient(t *testing.T, baseURL, email, password string) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}
	body := `{"email":"` + email + `","password":"` + password + `"}`
	loginResp, err := client.Post(baseURL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	_ = loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.StatusCode, http.StatusOK)
	}
	return client
}

func openUsersTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	migrationsURL := usersTestMigrationSourceURL(t)
	m, err := newUsersTestMigrator(t, dbPath, migrationsURL)
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

func usersTestMigrationSourceURL(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return "file://" + filepath.Join(root, "migrations")
}

func newUsersTestMigrator(t *testing.T, dbPath, migrationsURL string) (*migrate.Migrate, error) {
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
