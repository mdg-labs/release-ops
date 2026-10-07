package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

func setSeedEnv(t *testing.T, dbPath string) {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("a", 64))
	t.Setenv("APP_ENCRYPTION_KEY", strings.Repeat("1", 64))
	t.Setenv("APP_DB_PATH", dbPath)
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
}

func countUsers(t *testing.T, dbPath string) int64 {
	t.Helper()
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	n, err := storedb.New(db).CountUsers(context.Background())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	return n
}

func TestRunCreatesSchemaAndAdminOnFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data", "app.db")
	setSeedEnv(t, dbPath)

	var out bytes.Buffer
	args := []string{"--email", "admin@example.com", "--password", "a-strong-password"}
	if err := run(context.Background(), args, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run on fresh database: %v", err)
	}
	if !strings.Contains(out.String(), "Admin user created.") {
		t.Fatalf("output = %q, want admin created message", out.String())
	}
	if n := countUsers(t, dbPath); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}

func TestRunRefusesWhenUsersExist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	setSeedEnv(t, dbPath)

	args := []string{"--email", "admin@example.com", "--password", "a-strong-password"}
	if err := run(context.Background(), args, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	args = []string{"--email", "second@example.com", "--password", "another-strong-password"}
	err := run(context.Background(), args, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "users already exist") {
		t.Fatalf("second run error = %v, want users already exist", err)
	}
	if n := countUsers(t, dbPath); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
}
