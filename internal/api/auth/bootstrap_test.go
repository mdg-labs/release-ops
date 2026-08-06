package auth_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdguggenbichler/release-ops/internal/api"
	"github.com/mdguggenbichler/release-ops/internal/api/auth"
	storedb "github.com/mdguggenbichler/release-ops/internal/store/db"
)

func TestBootstrapFromEnvCreatesAdminOnFirstBoot(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()

	if err := auth.BootstrapFromEnv(ctx, queries, "admin@example.com", "bootstrap-pass"); err != nil {
		t.Fatalf("BootstrapFromEnv: %v", err)
	}

	count, err := queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUsers = %d, want 1", count)
	}

	user, err := queries.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if err := auth.ComparePassword(user.PasswordHash, "bootstrap-pass"); err != nil {
		t.Fatalf("ComparePassword: %v", err)
	}
}

func TestBootstrapFromEnvDoesNotDuplicateOnSecondBoot(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()
	email := "admin@example.com"
	password := "bootstrap-pass"

	if err := auth.BootstrapFromEnv(ctx, queries, email, password); err != nil {
		t.Fatalf("first BootstrapFromEnv: %v", err)
	}
	if err := auth.BootstrapFromEnv(ctx, queries, email, password); err != nil {
		t.Fatalf("second BootstrapFromEnv: %v", err)
	}

	count, err := queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountUsers = %d, want 1", count)
	}
}

func TestBootstrapFromEnvSkipsWhenUsersExist(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()

	if err := auth.SeedAdminUser(ctx, queries, "existing@example.com", "existing-pass"); err != nil {
		t.Fatalf("SeedAdminUser: %v", err)
	}

	if err := auth.BootstrapFromEnv(ctx, queries, "other@example.com", "other-pass"); err != nil {
		t.Fatalf("BootstrapFromEnv: %v", err)
	}

	user, err := queries.GetUserByEmail(ctx, "existing@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail existing: %v", err)
	}
	if _, err := queries.GetUserByEmail(ctx, "other@example.com"); err == nil {
		t.Fatal("expected second bootstrap email to be skipped")
	}
	if user.Email != "existing@example.com" {
		t.Fatalf("email = %q, want existing@example.com", user.Email)
	}
}

func TestBootstrapFromEnvSkipsWhenNotConfigured(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	if err := auth.BootstrapFromEnv(context.Background(), queries, "", ""); err != nil {
		t.Fatalf("BootstrapFromEnv: %v", err)
	}

	count, err := queries.CountUsers(context.Background())
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if count != 0 {
		t.Fatalf("CountUsers = %d, want 0", count)
	}
}

func TestBootstrapRejectsInvalidEmail(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	err := auth.BootstrapFromEnv(context.Background(), queries, "not-an-email", "secret")
	if err == nil {
		t.Fatal("expected error for invalid email")
	}
	if !errors.Is(err, auth.ErrInvalidEmail) {
		t.Fatalf("error = %v, want ErrInvalidEmail", err)
	}
}

func TestBootstrapPasswordHashedWithBcrypt(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()
	password := "correct-horse-battery"

	if err := auth.BootstrapFromEnv(ctx, queries, "admin@example.com", password); err != nil {
		t.Fatalf("BootstrapFromEnv: %v", err)
	}

	user, err := queries.GetUserByEmail(ctx, "admin@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if !strings.HasPrefix(user.PasswordHash, "$2") {
		t.Fatalf("password_hash = %q, want bcrypt prefix", user.PasswordHash)
	}
	if user.PasswordHash == password {
		t.Fatal("password stored in plaintext")
	}
	if err := auth.ComparePassword(user.PasswordHash, password); err != nil {
		t.Fatalf("ComparePassword: %v", err)
	}
}

func TestSeedAdminUserCreatesAdmin(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()

	if err := auth.SeedAdminUser(ctx, queries, "cli@example.com", "cli-pass"); err != nil {
		t.Fatalf("SeedAdminUser: %v", err)
	}

	user, err := queries.GetUserByEmail(ctx, "cli@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if err := auth.ComparePassword(user.PasswordHash, "cli-pass"); err != nil {
		t.Fatalf("ComparePassword: %v", err)
	}
}

func TestSeedAdminUserReturnsErrUsersExist(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	queries := storedb.New(db)
	ctx := context.Background()

	if err := auth.SeedAdminUser(ctx, queries, "first@example.com", "pass"); err != nil {
		t.Fatalf("first SeedAdminUser: %v", err)
	}
	err := auth.SeedAdminUser(ctx, queries, "second@example.com", "pass")
	if err == nil {
		t.Fatal("expected error when users exist")
	}
	if !errors.Is(err, auth.ErrUsersExist) {
		t.Fatalf("error = %v, want ErrUsersExist", err)
	}
}

func TestResolveAdminCredentialsInteractive(t *testing.T) {
	t.Parallel()

	in := strings.NewReader("interactive@example.com\ninteractive-pass\n")
	var out bytes.Buffer

	email, password, err := auth.ResolveAdminCredentials(in, &out, "", "")
	if err != nil {
		t.Fatalf("ResolveAdminCredentials: %v", err)
	}
	if email != "interactive@example.com" {
		t.Fatalf("email = %q, want interactive@example.com", email)
	}
	if password != "interactive-pass" {
		t.Fatalf("password = %q, want interactive-pass", password)
	}
	if !strings.Contains(out.String(), "Email:") || !strings.Contains(out.String(), "Password:") {
		t.Fatalf("prompts = %q, want Email and Password prompts", out.String())
	}
}

func TestNoAuthRegisterRoute(t *testing.T) {
	t.Parallel()

	db := openMigratedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	handler := api.NewServerRouter(&api.ServerDeps{
		DB:      db,
		Session: sm,
		Queries: storedb.New(db),
	})
	mux, ok := handler.(chi.Router)
	if !ok {
		t.Fatal("server router is not chi.Router")
	}

	paths, err := api.ListRoutes(mux)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}

	for _, p := range paths {
		if strings.Contains(p, "/auth/register") {
			t.Fatalf("unexpected register route: %s", p)
		}
	}
	if !slices.Contains(paths, "POST /api/v1/auth/login") {
		t.Fatalf("paths = %v, missing POST /api/v1/auth/login", paths)
	}
}
