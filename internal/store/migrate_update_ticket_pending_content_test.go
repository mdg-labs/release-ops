package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database whose update tickets already exist upgrades through the migration that adds
// pending_content_hash to update_tickets: every row stays as it was and the new column is NULL,
// so no existing ticket is taken for one with an unfinished content update.
func TestUpdateTicketPendingContentMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "update_ticket_pending_content") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration update_ticket_pending_content not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	const at = "2026-11-16T08:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at)
		 VALUES ('int-1', 'kaneo', 'Kaneo', 'https://kaneo.example', 'x', '` + at + `', '` + at + `')`,
		`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at)
		 VALUES ('tp-1', 'int-1', 'p1', 'Project', '{}', '{}', '` + at + `', '` + at + `')`,
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, ticket_project_id, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', 'tp-1', '` + at + `', '` + at + `')`,
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'n1', 'https://github.com/acme/widgets/pull/1', 'Bump', 'renovate/x', 'open', '` + at + `', '` + at + `', '` + at + `')`,
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-2', 'repo-1', 2, 'n2', 'https://github.com/acme/widgets/pull/2', 'Bump 2', 'renovate/y', 'open', '` + at + `', '` + at + `', '` + at + `')`,
		`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, state, synced_hash, target_state, attempts, last_error, created_at, updated_at)
		 VALUES ('ut-1', 'pr-1', 'tp-1', 'EXT-1', 'open', 'hash-a', 'done', 2, 'boom', '` + at + `', '` + at + `')`,
		`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, pending_since, created_at, updated_at)
		 VALUES ('ut-2', 'pr-2', 'tp-1', 'pending', '` + at + `', '` + at + `', '` + at + `')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	applied, err := applyMigrations(ctx, dbPath, all[:idx+1])
	if err != nil {
		t.Fatalf("apply update_ticket_pending_content: %v", err)
	}
	if len(applied) != 1 || !strings.HasSuffix(applied[0].Slug, "update_ticket_pending_content") {
		t.Fatalf("applied = %v, want only update_ticket_pending_content", applied)
	}

	if got := count(t, raw, `SELECT COUNT(*) FROM update_tickets`); got != 2 {
		t.Fatalf("update_tickets has %d rows after the migration, want 2", got)
	}
	var externalID, hash, target, lastError string
	var attempts int
	var pending sql.NullString
	if err := raw.QueryRow(`SELECT external_id, synced_hash, target_state, attempts, last_error, pending_content_hash FROM update_tickets WHERE id = 'ut-1'`).
		Scan(&externalID, &hash, &target, &attempts, &lastError, &pending); err != nil {
		t.Fatalf("read the ticket: %v", err)
	}
	if externalID != "EXT-1" || hash != "hash-a" || target != "done" || attempts != 2 || lastError != "boom" || pending.Valid {
		t.Errorf("ticket = %q %q %q %d %q %v, want its values kept and pending_content_hash NULL", externalID, hash, target, attempts, lastError, pending)
	}
	if err := raw.QueryRow(`SELECT pending_content_hash FROM update_tickets WHERE id = 'ut-2'`).Scan(&pending); err != nil || pending.Valid {
		t.Errorf("pending_content_hash of the row without a ticket = %v (%v), want NULL", pending, err)
	}
	if _, err := raw.Exec(`UPDATE update_tickets SET pending_content_hash = 'hash-b' WHERE id = 'ut-1'`); err != nil {
		t.Errorf("store a pending content hash: %v", err)
	}
}
