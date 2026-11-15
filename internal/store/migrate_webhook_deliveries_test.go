package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that already holds an installation and jobs upgrades through the migration
// that adds webhook_deliveries with every existing row intact, and the new table works.
func TestWebhookDeliveriesMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "webhook_deliveries") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration webhook_deliveries not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	const at = "2026-11-14T12:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', '` + at + `', '` + at + `')`,
		`INSERT INTO jobs (id, kind, repository_id, priority, run_after, "trigger", created_at)
		 VALUES ('job-1', 'renovate_run', 'repo-1', 'normal', '` + at + `', 'run_now', '` + at + `')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply webhook_deliveries: %v", err)
	}

	for table, want := range map[string]int{"installations": 1, "repositories": 1, "jobs": 1, "webhook_deliveries": 0} {
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s has %d rows after the migration (%v), want %d", table, n, err, want)
		}
	}
	if _, err := raw.Exec(`INSERT INTO webhook_deliveries (guid, event, installation_id, received_at) VALUES ('g1', 'installation', 'inst-1', ?)`, at); err != nil {
		t.Fatalf("insert delivery: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO webhook_deliveries (guid, event, received_at) VALUES ('g1', 'installation', ?)`, at); err == nil {
		t.Error("a second row with the same guid was accepted, want the primary key to refuse it")
	}
}
