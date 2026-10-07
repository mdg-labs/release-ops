package middleware_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
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

	seedSessionUser(t, db, "user-123")

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
	storetest.Migrate(t, dbPath)

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
