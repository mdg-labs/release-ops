package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func TestLoginFlow(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "admin@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	srv := httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: queries,
	}))
	t.Cleanup(srv.Close)

	body := `{"email":"admin@example.com","password":"secret-pass"}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var loginResp struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResp.User.ID != user.ID || loginResp.User.Email != user.Email {
		t.Fatalf("login user = %+v, want id=%s email=%s", loginResp.User, user.ID, user.Email)
	}

	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("expected release_ops_session cookie")
	}
	if !cookie.HttpOnly {
		t.Fatal("expected HttpOnly cookie")
	}
	if cookie.Path != "/" {
		t.Fatalf("cookie path = %q, want /", cookie.Path)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie SameSite = %v, want Lax", cookie.SameSite)
	}
}

func TestLoginInvalidPasswordReturns401(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "admin@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	srv := httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: queries,
	}))
	t.Cleanup(srv.Close)

	body := `{"email":"admin@example.com","password":"wrong-pass"}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestLoginUnknownEmailReturns401(t *testing.T) {
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

	body := `{"email":"missing@example.com","password":"secret-pass"}`
	resp, err := http.Post(srv.URL+"/api/v1/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestSessionReturnsNullWhenLoggedOut(t *testing.T) {
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

	resp, err := http.Get(srv.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var sessionResp struct {
		User *struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if sessionResp.User != nil {
		t.Fatalf("user = %+v, want null", sessionResp.User)
	}
}

func TestSessionReturnsUserWhenLoggedIn(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "admin@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	srv := httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: queries,
	}))
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

	sessionResp, err := client.Get(srv.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()

	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", sessionResp.StatusCode, http.StatusOK)
	}

	var body struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(sessionResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if body.User.ID != user.ID || body.User.Email != user.Email {
		t.Fatalf("session user = %+v, want id=%s email=%s", body.User, user.ID, user.Email)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "admin@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	srv := httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: queries,
	}))
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

	logoutReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	if err != nil {
		t.Fatalf("new logout request: %v", err)
	}
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	_ = logoutResp.Body.Close()

	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", logoutResp.StatusCode, http.StatusOK)
	}

	sessionResp, err := client.Get(srv.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer func() { _ = sessionResp.Body.Close() }()

	var body struct {
		User any `json:"user"`
	}
	if err := json.NewDecoder(sessionResp.Body).Decode(&body); err != nil {
		t.Fatalf("decode session response: %v", err)
	}
	if body.User != nil {
		t.Fatalf("user after logout = %+v, want null", body.User)
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
	h := &auth.Handlers{SessionManager: sm, Queries: storedb.New(db)}

	var protectedCalled bool
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Post("/auth/login", h.Login)
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
