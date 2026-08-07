package auth_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func TestTokenServiceGenerateHashValidateConsume(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	seedAppSettings(t, db)
	service := auth.NewTokenService(queries)

	raw, row, err := service.CreateInvitation(context.Background(), "invitee@example.com", "", 168)
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if raw == "" {
		t.Fatal("expected raw token")
	}
	if auth.HashToken(raw) != row.TokenHash {
		t.Fatal("token hash mismatch")
	}

	validated, err := service.Validate(context.Background(), raw, auth.TokenKindInvitation)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if validated.ID != row.ID {
		t.Fatalf("validated id = %q, want %q", validated.ID, row.ID)
	}

	if err := service.Consume(context.Background(), row.ID); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := service.Validate(context.Background(), raw, auth.TokenKindInvitation); err == nil {
		t.Fatal("expected invalid token after consume")
	}
}

func TestTokenServiceValidateRejectsExpired(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	service := auth.NewTokenService(queries)

	raw, err := auth.GenerateRawToken()
	if err != nil {
		t.Fatalf("GenerateRawToken: %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	_, err = queries.CreateAuthToken(context.Background(), storedb.CreateAuthTokenParams{
		ID:              uuid.NewString(),
		Kind:            auth.TokenKindInvitation,
		Email:           "expired@example.com",
		TokenHash:       auth.HashToken(raw),
		InvitedByUserID: sql.NullString{},
		ExpiresAt:       expiredAt,
		CreatedAt:       expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	if _, err := service.Validate(context.Background(), raw, auth.TokenKindInvitation); err == nil {
		t.Fatal("expected expired token to be invalid")
	}
}

func seedAppSettings(t *testing.T, db *sql.DB) {
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
