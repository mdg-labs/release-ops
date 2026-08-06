package middleware_test

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	_ "modernc.org/sqlite"
)

func TestRequireSessionRejectsMissingSession(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/protected", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"code":"unauthorized"`) {
		t.Fatalf("body = %s, want unauthorized error code", body)
	}
}

func TestRequireSessionAllowsValidSession(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)

	var gotUserID string
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
			userID, ok := apimw.UserIDFromContext(r.Context())
			if !ok {
				t.Fatal("expected user ID in context")
			}
			gotUserID = userID
			w.WriteHeader(http.StatusOK)
		})
	})

	seedReq := httptest.NewRequest(http.MethodGet, "/seed", nil)
	seedRec := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), auth.SessionUserIDKey, "user-123")
	})).ServeHTTP(seedRec, seedReq)

	var cookie *http.Cookie
	for _, c := range seedRec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected session cookie")
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUserID != "user-123" {
		t.Fatalf("userID = %q, want user-123", gotUserID)
	}
}

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	migrationsURL := "file://" + filepath.Join(root, "migrations")

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	migrateDB, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open sqlite for migrate: %v", err)
	}

	driver, err := sqlite.WithInstance(migrateDB, &sqlite.Config{})
	if err != nil {
		_ = migrateDB.Close()
		t.Fatalf("sqlite driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsURL, "sqlite", driver)
	if err != nil {
		_ = migrateDB.Close()
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
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
