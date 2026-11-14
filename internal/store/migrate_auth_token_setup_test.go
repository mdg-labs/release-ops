package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that already holds auth_tokens rows upgrades through the migration that adds
// the setup kind with every row intact: the migration rebuilds the table to widen its CHECK.
func TestAuthTokenSetupKindMigrationKeepsExistingTokens(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "auth_token_kind_setup") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration auth_token_kind_setup not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	if _, err := raw.Exec(`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u1', 'a@example.com', 'x', 't', 't')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	type tokenRow struct {
		id, kind, email, hash, expires, created string
		newEmail, invitedBy, usedAt             sql.NullString
	}
	want := []tokenRow{
		{id: "t1", kind: "invitation", email: "i@example.com", hash: "h1", expires: "2026-12-01T00:00:00.000Z", created: "c1",
			invitedBy: sql.NullString{String: "u1", Valid: true}},
		{id: "t2", kind: "password_reset", email: "p@example.com", hash: "h2", expires: "2026-12-02T00:00:00.000Z", created: "c2",
			usedAt: sql.NullString{String: "2026-11-01T00:00:00.000Z", Valid: true}},
		{id: "t3", kind: "email_change", email: "e@example.com", hash: "h3", expires: "2026-12-03T00:00:00.000Z", created: "c3",
			newEmail: sql.NullString{String: "n@example.com", Valid: true}},
	}
	for _, r := range want {
		if _, err := raw.Exec(
			`INSERT INTO auth_tokens (id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.id, r.kind, r.email, r.hash, r.newEmail, r.invitedBy, r.expires, r.usedAt, r.created,
		); err != nil {
			t.Fatalf("seed token %s: %v", r.id, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply auth_token_kind_setup: %v", err)
	}

	rows, err := raw.Query(`SELECT id, kind, email, token_hash, new_email, invited_by_user_id, expires_at, used_at, created_at FROM auth_tokens ORDER BY id`)
	if err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var got []tokenRow
	for rows.Next() {
		var r tokenRow
		if err := rows.Scan(&r.id, &r.kind, &r.email, &r.hash, &r.newEmail, &r.invitedBy, &r.expires, &r.usedAt, &r.created); err != nil {
			t.Fatalf("scan token: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tokens: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("auth_tokens has %d rows after the migration, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %s = %+v, want %+v", want[i].id, got[i], want[i])
		}
	}

	var indexes int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('auth_tokens') WHERE name = 'idx_auth_tokens_token_hash'`).Scan(&indexes); err != nil || indexes != 1 {
		t.Fatalf("token_hash index present = %d, %v; want 1", indexes, err)
	}

	insert := `INSERT INTO auth_tokens (id, kind, email, token_hash, expires_at, created_at) VALUES (?, ?, '', 'h', 't', 't')`
	if _, err := raw.Exec(insert, "t4", "setup"); err != nil {
		t.Fatalf("insert setup token: %v", err)
	}
	if _, err := raw.Exec(insert, "t5", "bogus"); err == nil {
		t.Fatal("insert of an unknown token kind succeeded, want the CHECK to refuse it")
	}
}
