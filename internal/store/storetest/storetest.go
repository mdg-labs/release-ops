// Package storetest applies the embedded migrations to throwaway databases for tests.
package storetest

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

// Migrate creates the SQLite file at dbPath and applies every embedded migration to it.
func Migrate(t testing.TB, dbPath string) {
	t.Helper()

	if _, err := store.Migrate(context.Background(), dbPath); err != nil {
		t.Fatalf("migrate %s: %v", dbPath, err)
	}
}

// OpenMigrated returns a migrated database in a temporary directory, closed with the test.
func OpenMigrated(t testing.TB) *store.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	Migrate(t, dbPath)
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// SeedRepositories inserts one installation and n repositories of it and returns the
// repository ids, so jobs have repositories to belong to.
func SeedRepositories(t testing.TB, db *store.DB, n int) []string {
	t.Helper()

	const at = "2026-11-14T12:00:00.000Z"
	if _, err := db.Writer.Exec(
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1, 1, 'acme', 'Organization', 'active', 'all', ?, ?)`, at, at,
	); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("repo-%d", i+1)
		if _, err := db.Writer.Exec(
			`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
			 VALUES (?, 'inst-1', ?, ?, 'main', ?, ?)`, ids[i], i+1, fmt.Sprintf("acme/repo-%d", i+1), at, at,
		); err != nil {
			t.Fatalf("seed repository %d: %v", i+1, err)
		}
	}
	return ids
}
