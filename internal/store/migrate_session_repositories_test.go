package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that already holds users, sessions and repositories upgrades through the migration
// that adds session_repositories with every existing row intact. The sessions that existed
// before have no list, which is no repository, and a list is deleted with its session or its
// repository.
func TestSessionRepositoriesMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "session_repositories") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration session_repositories not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	const at = "2026-11-14T12:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO users (id, created_at, updated_at) VALUES ('user-1', '` + at + `', '` + at + `')`,
		`INSERT INTO users (id, created_at, updated_at) VALUES ('user-2', '` + at + `', '` + at + `')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-1', x'00', 2461000.5, 'user-1')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-2', x'00', 2461000.5, NULL)`,
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', '` + at + `', '` + at + `')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply session_repositories: %v", err)
	}

	for table, want := range map[string]int{"users": 2, "sessions": 2, "repositories": 1, "session_repositories": 0} {
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s has %d rows after the migration (%v), want %d", table, n, err, want)
		}
	}
	var userID sql.NullString
	if err := raw.QueryRow(`SELECT user_id FROM sessions WHERE token = 'tok-1'`).Scan(&userID); err != nil || userID.String != "user-1" {
		t.Errorf("session user after the migration = %v, %v; want user-1", userID, err)
	}

	// Sessions from before the migration have no stored list, and so no repository.
	var listed int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM session_repositories WHERE session_token IN ('tok-1', 'tok-2')`).Scan(&listed); err != nil || listed != 0 {
		t.Fatalf("an old session has %d listed repositories (%v), want none", listed, err)
	}

	if _, err := raw.Exec(`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok-1', 'repo-1')`); err != nil {
		t.Fatalf("insert list row: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok-1', 'repo-1')`); err == nil {
		t.Error("the same repository was listed twice for one session, want the primary key to refuse it")
	}
	if _, err := raw.Exec(`INSERT INTO session_repositories (session_token, repository_id) VALUES ('no-such-session', 'repo-1')`); err == nil {
		t.Error("a list row for a session that does not exist was accepted")
	}
	if _, err := raw.Exec(`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok-1', 'no-such-repository')`); err == nil {
		t.Error("a list row for a repository that does not exist was accepted")
	}

	if _, err := raw.Exec(`DELETE FROM sessions WHERE token = 'tok-1'`); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if err := raw.QueryRow(`SELECT COUNT(*) FROM session_repositories`).Scan(&listed); err != nil || listed != 0 {
		t.Errorf("list rows after its session was deleted = %d (%v), want 0", listed, err)
	}

	if _, err := raw.Exec(`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok-2', 'repo-1')`); err != nil {
		t.Fatalf("insert list row: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM repositories WHERE id = 'repo-1'`); err != nil {
		t.Fatalf("delete repository: %v", err)
	}
	if err := raw.QueryRow(`SELECT COUNT(*) FROM session_repositories`).Scan(&listed); err != nil || listed != 0 {
		t.Errorf("list rows after its repository was deleted = %d (%v), want 0", listed, err)
	}
}
