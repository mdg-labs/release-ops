package auth_test

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
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
	seedSessionUser(t, db1, "user-123")
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

func openMigratedDBAt(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	storetest.Migrate(t, dbPath)

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

func seedSessionUser(t *testing.T, db *sql.DB, id string) {
	t.Helper()

	_, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES (?, ?, 'x', '2026-08-06T12:00:00.000Z', '2026-08-06T12:00:00.000Z')`,
		id, id+"@example.com",
	)
	if err != nil {
		t.Fatalf("seed user %s: %v", id, err)
	}
}
