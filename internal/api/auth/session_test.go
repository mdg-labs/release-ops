package auth_test

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mdguggenbichler/release-ops/internal/api/auth"
	_ "modernc.org/sqlite"
)

func TestNewSessionManagerCookieConfig(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() {
		_ = db.Close()
	})

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), true)
	if sm.Cookie.Name != auth.SessionCookieName {
		t.Fatalf("cookie name = %q, want %q", sm.Cookie.Name, auth.SessionCookieName)
	}
	if !sm.Cookie.HttpOnly {
		t.Fatal("expected HttpOnly cookie")
	}
	if sm.Cookie.Path != "/" {
		t.Fatalf("cookie path = %q, want /", sm.Cookie.Path)
	}
	if sm.Cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want Lax", sm.Cookie.SameSite)
	}
	if !sm.Cookie.Secure {
		t.Fatal("expected Secure cookie when secureCookies is true")
	}
}

func TestSessionPersistsAcrossRestart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	secret := strings.Repeat("s", 32)

	db1 := openMigratedDBAt(t, dbPath)
	sm1 := auth.NewSessionManager(db1, secret, false)

	var cookie *http.Cookie
	handler := sm1.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm1.Put(r.Context(), "userID", "user-123")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected release_ops_session cookie")
	}
	if err := db1.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	var sessionRows int
	dbCheck, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("reopen sqlite for row check: %v", err)
	}
	if err := dbCheck.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessionRows); err != nil {
		_ = dbCheck.Close()
		t.Fatalf("count sessions: %v", err)
	}
	if sessionRows == 0 {
		_ = dbCheck.Close()
		t.Fatal("expected session row in sessions table")
	}
	_ = dbCheck.Close()

	db2 := openMigratedDBAt(t, dbPath)
	sm2 := auth.NewSessionManager(db2, secret, false)

	var got string
	handler2 := sm2.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = sm2.GetString(r.Context(), "userID")
	}))
	rec2 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookie)
	handler2.ServeHTTP(rec2, req)

	if got != "user-123" {
		t.Fatalf("session userID = %q, want user-123", got)
	}
}

func migrationSourceURL(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return "file://" + filepath.Join(root, "migrations")
}

func newMigrator(t *testing.T, dbPath, migrationsURL string) (*migrate.Migrate, error) {
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

func openMigratedDBAt(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	migrationsURL := migrationSourceURL(t)
	m, err := newMigrator(t, dbPath, migrationsURL)
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

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	return openMigratedDBAt(t, dbPath)
}
