package auth_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/mail"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func TestForgotPasswordAlwaysReturns200(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	srv := newPasswordResetTestServer(t, db, queries, &recordingMailer{})
	t.Cleanup(srv.Close)

	for _, email := range []string{"known@example.com", "missing@example.com"} {
		resp, err := http.Post(
			srv.URL+"/api/v1/auth/forgot-password",
			"application/json",
			strings.NewReader(`{"email":"`+email+`"}`),
		)
		if err != nil {
			t.Fatalf("POST forgot-password (%s): %v", email, err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status for %s = %d, want %d", email, resp.StatusCode, http.StatusOK)
		}

		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Message != "If an account exists for that email, a reset link has been sent." {
			t.Fatalf("message = %q, want generic reset message", body.Message)
		}
	}
}

func TestForgotPasswordCreatesTokenOnlyForExistingUser(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	recorder := &recordingMailer{}
	srv := newPasswordResetTestServer(t, db, queries, recorder)
	t.Cleanup(srv.Close)

	resp, err := http.Post(
		srv.URL+"/api/v1/auth/forgot-password",
		"application/json",
		strings.NewReader(`{"email":"missing@example.com"}`),
	)
	if err != nil {
		t.Fatalf("POST forgot-password: %v", err)
	}
	_ = resp.Body.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'password_reset'`).Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 0 {
		t.Fatalf("token count for missing user = %d, want 0", count)
	}
	if len(recorder.messages) != 0 {
		t.Fatal("expected no email for missing user")
	}

	resp, err = http.Post(
		srv.URL+"/api/v1/auth/forgot-password",
		"application/json",
		strings.NewReader(`{"email":"known@example.com"}`),
	)
	if err != nil {
		t.Fatalf("POST forgot-password existing: %v", err)
	}
	_ = resp.Body.Close()

	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'password_reset' AND email = ? AND used_at IS NULL`, "known@example.com").Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("token count for existing user = %d, want 1", count)
	}
	if len(recorder.messages) != 1 {
		t.Fatalf("sent emails = %d, want 1", len(recorder.messages))
	}
	if recorder.messages[0].To != "known@example.com" {
		t.Fatalf("email to = %q, want known@example.com", recorder.messages[0].To)
	}
}

func TestForgotPasswordInvalidatesPriorResetTokens(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	srv := newPasswordResetTestServer(t, db, queries, &recordingMailer{})
	t.Cleanup(srv.Close)

	for i := 0; i < 2; i++ {
		resp, err := http.Post(
			srv.URL+"/api/v1/auth/forgot-password",
			"application/json",
			strings.NewReader(`{"email":"known@example.com"}`),
		)
		if err != nil {
			t.Fatalf("POST forgot-password %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	var active, used int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'password_reset' AND email = ? AND used_at IS NULL`, "known@example.com").Scan(&active); err != nil {
		t.Fatalf("count active tokens: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'password_reset' AND email = ? AND used_at IS NOT NULL`, "known@example.com").Scan(&used); err != nil {
		t.Fatalf("count used tokens: %v", err)
	}
	if active != 1 {
		t.Fatalf("active tokens = %d, want 1", active)
	}
	if used != 1 {
		t.Fatalf("invalidated tokens = %d, want 1", used)
	}
}

func TestResetPasswordUpdatesPasswordAndSession(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	userID := seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	tokenService := auth.NewTokenService(queries)
	raw, err := tokenService.CreatePasswordReset(context.Background(), "known@example.com", 60)
	if err != nil {
		t.Fatalf("CreatePasswordReset: %v", err)
	}

	srv := newPasswordResetTestServer(t, db, queries, &recordingMailer{})
	t.Cleanup(srv.Close)

	resp, err := http.Post(
		srv.URL+"/api/v1/auth/reset-password",
		"application/json",
		strings.NewReader(`{"token":"`+raw+`","password":"new-pass-1"}`),
	)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.User.ID != userID || body.User.Email != "known@example.com" {
		t.Fatalf("user = %+v, want id=%s email=known@example.com", body.User, userID)
	}

	user, err := queries.GetUserByEmail(context.Background(), "known@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if err := auth.ComparePassword(user.PasswordHash, "new-pass-1"); err != nil {
		t.Fatal("expected password to be updated")
	}
	if err := auth.ComparePassword(user.PasswordHash, "old-pass-1"); err == nil {
		t.Fatal("expected old password to no longer match")
	}
}

func TestResetPasswordRejectsShortPassword(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	tokenService := auth.NewTokenService(queries)
	raw, err := tokenService.CreatePasswordReset(context.Background(), "known@example.com", 60)
	if err != nil {
		t.Fatalf("CreatePasswordReset: %v", err)
	}

	srv := newPasswordResetTestServer(t, db, queries, &recordingMailer{})
	t.Cleanup(srv.Close)

	resp, err := http.Post(
		srv.URL+"/api/v1/auth/reset-password",
		"application/json",
		strings.NewReader(`{"token":"`+raw+`","password":"short"}`),
	)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestResetPasswordRejectsInvalidToken(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)

	srv := newPasswordResetTestServer(t, db, queries, &recordingMailer{})
	t.Cleanup(srv.Close)

	resp, err := http.Post(
		srv.URL+"/api/v1/auth/reset-password",
		"application/json",
		strings.NewReader(`{"token":"not-a-real-token","password":"new-pass-1"}`),
	)
	if err != nil {
		t.Fatalf("POST reset-password: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestForgotPasswordSkipsEmailWhenSMTPNotConfigured(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	seedPasswordResetUser(t, queries, "known@example.com", "old-pass-1")

	srv := newPasswordResetTestServer(t, db, queries, mail.NoopMailer{})
	t.Cleanup(srv.Close)

	resp, err := http.Post(
		srv.URL+"/api/v1/auth/forgot-password",
		"application/json",
		strings.NewReader(`{"email":"known@example.com"}`),
	)
	if err != nil {
		t.Fatalf("POST forgot-password: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'password_reset' AND email = ?`, "known@example.com").Scan(&count); err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 1 {
		t.Fatalf("token count = %d, want 1", count)
	}
}

type recordingMailer struct {
	messages []mail.Message
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.messages = append(m.messages, msg)
	return nil
}

func newPasswordResetTestServer(t *testing.T, db *sql.DB, queries *storedb.Queries, mailer mail.Mailer) *httptest.Server {
	t.Helper()

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	return httptest.NewServer(api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: queries,
		Mailer:  mailer,
	}))
}

func seedPasswordResetUser(t *testing.T, queries *storedb.Queries, email, password string) string {
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
