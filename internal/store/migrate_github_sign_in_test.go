package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that holds local-account users, a session, a GitHub identity and a pinned
// bootstrap admin upgrades through the migration that makes users.email and
// users.password_hash optional with every row intact. The migration rebuilds users, so the
// test also shows that the rebuild does not cascade into the tables that reference it.
func TestGitHubSignInMigrationKeepsUsersSessionsAndIdentities(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "github_sign_in") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration github_sign_in not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := raw.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u1', 'a@example.com', 'hash-1', 'c1', 'u1')`)
	exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u2', 'b@example.com', 'hash-2', 'c2', 'u2')`)
	exec(`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('s1', x'0102', 2461449600.0, 'u1')`)
	exec(`INSERT INTO auth_identities (id, user_id, provider, subject, login, created_at) VALUES ('i1', 'u1', 'github', '583231', 'octocat', 'c')`)
	exec(`INSERT INTO instance_settings (id, bootstrap_admin_user_id, updated_at) VALUES (1, 'u1', 't')`)

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply github_sign_in: %v", err)
	}

	rows, err := raw.Query(`SELECT id, email, password_hash, created_at, updated_at FROM users ORDER BY id`)
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	defer func() { _ = rows.Close() }()
	type user struct{ id, email, hash, created, updated string }
	var got []user
	for rows.Next() {
		var u user
		if err := rows.Scan(&u.id, &u.email, &u.hash, &u.created, &u.updated); err != nil {
			t.Fatalf("scan user: %v", err)
		}
		got = append(got, u)
	}
	want := []user{
		{"u1", "a@example.com", "hash-1", "c1", "u1"},
		{"u2", "b@example.com", "hash-2", "c2", "u2"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("users after the migration = %+v, want %+v", got, want)
	}

	count := func(query string) int {
		t.Helper()
		var n int
		if err := raw.QueryRow(query).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		return n
	}
	if n := count(`SELECT COUNT(*) FROM sessions WHERE token = 's1' AND user_id = 'u1'`); n != 1 {
		t.Errorf("session of u1 present = %d, want 1", n)
	}
	if n := count(`SELECT COUNT(*) FROM auth_identities WHERE id = 'i1' AND user_id = 'u1' AND subject = '583231'`); n != 1 {
		t.Errorf("GitHub identity of u1 present = %d, want 1: the users rebuild must not cascade", n)
	}
	if n := count(`SELECT COUNT(*) FROM instance_settings WHERE bootstrap_admin_user_id = 'u1'`); n != 1 {
		t.Errorf("bootstrap admin kept = %d, want 1", n)
	}

	exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('g1', 't', 't')`)
	exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('g2', 't', 't')`)
	if _, err := raw.Exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u3', 'A@example.com', 'x', 't', 't')`); err != nil {
		t.Fatalf("a different email is refused: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u4', 'a@example.com', 'x', 't', 't')`); err == nil {
		t.Fatal("a duplicate email was accepted, want the UNIQUE constraint to refuse it")
	}
}
