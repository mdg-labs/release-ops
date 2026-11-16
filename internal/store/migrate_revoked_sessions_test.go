package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that already holds users and sessions upgrades through the migration that adds
// revoked_sessions with every existing row intact; no earlier session is revoked.
func TestRevokedSessionsMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "revoked_sessions") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration revoked_sessions not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	const at = "2026-11-15T12:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO users (id, created_at, updated_at) VALUES ('user-1', '` + at + `', '` + at + `')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-1', x'00', 2461000.5, 'user-1')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply revoked_sessions: %v", err)
	}

	for table, want := range map[string]int{"users": 1, "sessions": 1, "revoked_sessions": 0} {
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s has %d rows after the migration (%v), want %d", table, n, err, want)
		}
	}
	if _, err := raw.Exec(`INSERT INTO revoked_sessions (token_hash, expiry) VALUES ('h', 2461000.5)`); err != nil {
		t.Fatalf("insert marker: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO revoked_sessions (token_hash, expiry) VALUES ('h', 2461000.5)`); err == nil {
		t.Error("the same token hash was recorded twice, want the primary key to refuse it")
	}
}
