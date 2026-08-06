package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mdguggenbichler/release-ops/internal/api/auth"
	storedb "github.com/mdguggenbichler/release-ops/internal/store/db"
)

func TestHashPasswordAndComparePassword(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}

	if err := auth.ComparePassword(hash, "correct-horse-battery"); err != nil {
		t.Fatalf("ComparePassword valid password: %v", err)
	}
}

func TestComparePasswordRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	hash, err := auth.HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if err := auth.ComparePassword(hash, "wrong"); err == nil {
		t.Fatal("expected compare error for wrong password")
	}
}

func TestGetUserByEmailReturnsUserRow(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() {
		_ = db.Close()
	})

	queries := storedb.New(db)
	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("admin-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	created, err := queries.CreateUser(context.Background(), storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "admin@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := queries.GetUserByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if got.ID != created.ID || got.Email != created.Email || got.PasswordHash != created.PasswordHash {
		t.Fatalf("GetUserByEmail = %+v, want %+v", got, created)
	}
}

func TestCountUsersForBootstrapGuard(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() {
		_ = db.Close()
	})

	queries := storedb.New(db)
	ctx := context.Background()

	count, err := queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers empty db: %v", err)
	}
	if count != 0 {
		t.Fatalf("CountUsers = %d, want 0", count)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	hash, err := auth.HashPassword("bootstrap")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if _, err := queries.CreateUser(ctx, storedb.CreateUserParams{
		ID:           uuid.NewString(),
		Email:        "bootstrap@example.com",
		PasswordHash: hash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	count, err = queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers after insert: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUsers = %d, want 1", count)
	}
}
