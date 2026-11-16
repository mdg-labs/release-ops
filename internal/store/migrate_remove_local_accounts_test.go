package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"
)

// removeLocalAccountsIndex is the position of the remove_local_accounts migration in all.
func removeLocalAccountsIndex(t *testing.T, all []sqlitemigrate.Migration) int {
	t.Helper()

	idx := -1
	for i, m := range all {
		if m.Slug == removeLocalAccountsSlug {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration %s not found after another migration in %d migrations", removeLocalAccountsSlug, len(all))
	}
	return idx
}

// seedBeforeRemoveLocalAccounts applies every migration before remove_local_accounts to a new
// database and fills it the way an instance that has run both local accounts and GitHub sign-in
// would be filled: a local user with a password and a GitHub user with an identity, sessions of
// both, a bootstrap admin, one-time tokens of every kind and the legacy token windows.
func seedBeforeRemoveLocalAccounts(t *testing.T, dbPath string) *sql.DB {
	t.Helper()

	ctx := context.Background()
	all := loadEmbedded(t, ctx)
	idx := removeLocalAccountsIndex(t, all)
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
		`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u-local', 'admin@example.com', 'bcrypt-hash', '` + at + `', '` + at + `')`,
		`INSERT INTO users (id, created_at, updated_at) VALUES ('u-gh', '` + at + `', '` + at + `')`,
		`INSERT INTO auth_identities (id, user_id, provider, subject, login, created_at) VALUES ('ai-1', 'u-gh', 'github', '583231', 'octocat', '` + at + `')`,
		`INSERT INTO instance_settings (id, concurrency_cap, bootstrap_admin_user_id, updated_at) VALUES (1, 4, 'u-gh', '` + at + `')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-local', x'0102', 2461000.5, 'u-local')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-gh', x'0304', 2461000.5, 'u-gh')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-anon', x'05', 2461000.5, NULL)`,
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', '` + at + `', '` + at + `')`,
		`INSERT INTO session_repositories (session_token, repository_id) VALUES ('tok-gh', 'repo-1')`,
		`INSERT INTO login_link_tokens (id, token_hash, expires_at, created_at) VALUES ('ll-1', 'link-hash', '` + at + `', '` + at + `')`,
		`INSERT INTO app_settings (id, invite_token_expiry_hours, password_reset_token_expiry_minutes, updated_at) VALUES (1, 90, 30, '` + at + `')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at)
		 VALUES ('t-invite', 'invitation', 'i@example.com', 'h1', NULL, 'u-local', '2026-12-01T00:00:00.000Z', NULL, '` + at + `')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at)
		 VALUES ('t-reset', 'password_reset', 'admin@example.com', 'h2', NULL, NULL, '2026-12-01T00:00:00.000Z', '` + at + `', '` + at + `')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at)
		 VALUES ('t-email', 'email_change', 'admin@example.com', 'h3', 'new@example.com', NULL, '2026-12-01T00:00:00.000Z', NULL, '` + at + `')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at)
		 VALUES ('t-setup', 'setup', '', 'setup-hash', NULL, NULL, '9999-12-31T23:59:59.000Z', NULL, '` + at + `')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	return raw
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// An instance that has local accounts upgrades through remove_local_accounts without losing a
// row of any table that stays: users, identities, sessions, the viewers' repository lists, the
// bootstrap admin and the setup token all survive. The migration drops the password and email
// columns, the token windows, and the rows of the one-time token kinds nothing redeems any more.
// The migration rebuilds users and auth_tokens, and a rebuild that dropped users with foreign
// keys enforced would cascade away the identities and sessions of the survivors.
func TestRemoveLocalAccountsMigrationKeepsTheSurvivingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	raw := seedBeforeRemoveLocalAccounts(t, dbPath)

	all := loadEmbedded(t, ctx)
	applied, err := applyMigrations(ctx, dbPath, all[:removeLocalAccountsIndex(t, all)+1])
	if err != nil {
		t.Fatalf("apply remove_local_accounts over a database with local accounts: %v", err)
	}
	if len(applied) != 1 || applied[0].Slug != removeLocalAccountsSlug {
		t.Fatalf("applied = %v, want only %s", applied, removeLocalAccountsSlug)
	}

	for table, want := range map[string]int{
		"users": 2, "auth_identities": 1, "sessions": 3, "session_repositories": 1,
		"instance_settings": 1, "login_link_tokens": 1, "repositories": 1, "installations": 1, "auth_tokens": 1,
	} {
		if got := count(t, raw, `SELECT COUNT(*) FROM `+table); got != want {
			t.Errorf("%s has %d rows after the migration, want %d", table, got, want)
		}
	}

	var id, created, updated string
	if err := raw.QueryRow(`SELECT id, created_at, updated_at FROM users WHERE id = 'u-local'`).Scan(&id, &created, &updated); err != nil || created != "2026-11-14T12:00:00.000Z" || updated != created {
		t.Errorf("the local user after the migration = %q %q %q, %v; want it kept with its timestamps", id, created, updated, err)
	}
	var login string
	if err := raw.QueryRow(`SELECT login FROM auth_identities WHERE user_id = 'u-gh' AND subject = '583231'`).Scan(&login); err != nil || login != "octocat" {
		t.Errorf("the GitHub identity after the migration = %q, %v; want octocat", login, err)
	}
	var boot string
	if err := raw.QueryRow(`SELECT bootstrap_admin_user_id FROM instance_settings WHERE id = 1`).Scan(&boot); err != nil || boot != "u-gh" {
		t.Errorf("bootstrap admin after the migration = %q, %v; want u-gh", boot, err)
	}
	for token, user := range map[string]sql.NullString{"tok-local": {String: "u-local", Valid: true}, "tok-gh": {String: "u-gh", Valid: true}, "tok-anon": {}} {
		var data []byte
		var got sql.NullString
		if err := raw.QueryRow(`SELECT data, user_id FROM sessions WHERE token = ?`, token).Scan(&data, &got); err != nil || got != user || len(data) == 0 {
			t.Errorf("session %s after the migration = user %v data %x, %v; want user %v and its data kept", token, got, data, err, user)
		}
	}
	var tokenID, kind, hash string
	if err := raw.QueryRow(`SELECT id, kind, token_hash FROM auth_tokens`).Scan(&tokenID, &kind, &hash); err != nil || tokenID != "t-setup" || kind != "setup" || hash != "setup-hash" {
		t.Errorf("auth_tokens after the migration = %q %q %q, %v; want only the setup token", tokenID, kind, hash, err)
	}

	for _, gone := range []struct{ table, column string }{
		{"users", "email"}, {"users", "password_hash"},
		{"auth_tokens", "email"}, {"auth_tokens", "new_email"}, {"auth_tokens", "invited_by_user_id"},
	} {
		if count(t, raw, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, gone.table, gone.column) != 0 {
			t.Errorf("%s.%s is still there after the migration", gone.table, gone.column)
		}
	}
	if count(t, raw, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'app_settings'`) != 0 {
		t.Error("app_settings is still there after the migration")
	}

	if _, err := raw.Exec(`INSERT INTO auth_tokens (id, kind, token_hash, expires_at, created_at) VALUES ('t-new', 'invitation', 'h', 't', 't')`); err == nil {
		t.Error("an invitation token was accepted after the migration, want the CHECK to allow only the setup kind")
	}
	if _, err := raw.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('u-new', 't', 't')`); err != nil {
		t.Errorf("a user without email or password: %v", err)
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM pragma_index_list('auth_tokens') WHERE name = 'idx_auth_tokens_token_hash'`); got != 1 {
		t.Errorf("token_hash index present = %d, want 1", got)
	}

	if _, err := raw.Exec(`DELETE FROM users WHERE id = 'u-gh'`); err == nil {
		t.Error("deleting the bootstrap admin's user succeeded, want its foreign key from instance_settings to hold")
	}
}

// A start that finds nothing but the one-time tokens of the setup kind, as a database that
// never had local accounts does, applies the migration like any other.
func TestRemoveLocalAccountsMigrationOnADatabaseWithoutOldTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	raw := seedBeforeRemoveLocalAccounts(t, dbPath)
	if _, err := raw.Exec(`DELETE FROM auth_tokens WHERE kind <> 'setup'`); err != nil {
		t.Fatalf("remove old tokens: %v", err)
	}

	all := loadEmbedded(t, ctx)
	through := all[:removeLocalAccountsIndex(t, all)+1]
	if _, err := applyMigrations(ctx, dbPath, through); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM auth_tokens WHERE kind = 'setup'`); got != 1 {
		t.Fatalf("setup tokens after the migration = %d, want 1", got)
	}
	again, err := applyMigrations(ctx, dbPath, through)
	if err != nil || len(again) != 0 {
		t.Fatalf("second apply = %v, %v; want nothing to apply", again, err)
	}
}

// Without the removal of the old token kinds first, the rebuilt auth_tokens would refuse the rows
// it is given and the whole upgrade would fail. This pins that the removal is what makes it pass.
func TestRemoveLocalAccountsMigrationNeedsTheOldTokensRemovedFirst(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	raw := seedBeforeRemoveLocalAccounts(t, dbPath)
	all := loadEmbedded(t, ctx)
	through := all[:removeLocalAccountsIndex(t, all)+1]

	runner := &sqlitemigrate.Runner{DBPath: dbPath, SnapshotDir: filepath.Join(filepath.Dir(dbPath), "snapshots")}
	if _, err := runner.Apply(ctx, through); err == nil {
		t.Fatal("the migration applied over invitation tokens without the removal step, want the CHECK of the rebuilt table to refuse them")
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM auth_tokens`); got != 4 {
		t.Fatalf("auth_tokens has %d rows after the failed migration, want the 4 it started with (nothing half-applied)", got)
	}
	if got := count(t, raw, `SELECT COUNT(*) FROM pragma_table_info('users') WHERE name = 'email'`); got != 1 {
		t.Fatal("users lost its email column in a migration that failed")
	}
}
