package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database that already holds ticket projects, and the rows that reference them, upgrades
// through the migration that drops the open-ticket policy and adds the lifecycle columns of
// update_tickets with every existing row intact.
func TestUpdateTicketLifecycleMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "update_ticket_lifecycle") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration update_ticket_lifecycle not found after another migration in %d migrations", len(all))
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
	const custom = `{"title":"T {{ .Repository }}","description":"","supersedeComment":"gone"}`
	for _, stmt := range []string{
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at)
		 VALUES ('int-1', 'kaneo', 'Kaneo', 'https://kaneo.example', 'x', '` + at + `', '` + at + `')`,
		`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, content_templates, on_open_ticket_policy, created_at, updated_at)
		 VALUES ('tp-custom', 'int-1', 'p1', 'Custom', '{}', '{}', '` + custom + `', 'merge', '` + at + `', '` + at + `')`,
		`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at)
		 VALUES ('tp-default', 'int-1', 'p2', 'Default', '{}', '{}', '` + at + `', '` + at + `')`,
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO installation_settings (installation_id, default_ticket_project_id, updated_at) VALUES ('inst-1', 'tp-default', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, ticket_project_id, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', 'tp-custom', '` + at + `', '` + at + `')`,
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'n', 'https://github.com/acme/widgets/pull/1', 'Bump', 'renovate/x', 'open', '` + at + `', '` + at + `', '` + at + `')`,
		`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, state, created_at, updated_at)
		 VALUES ('ut-1', 'pr-1', 'tp-custom', 'EXT-1', 'open', '` + at + `', '` + at + `')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply update_ticket_lifecycle: %v", err)
	}

	for table, want := range map[string]int{"ticket_projects": 2, "installation_settings": 1, "repositories": 1, "update_tickets": 1} {
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s has %d rows after the migration (%v), want %d", table, n, err, want)
		}
	}
	var templates string
	if err := raw.QueryRow(`SELECT content_templates FROM ticket_projects WHERE id = 'tp-custom'`).Scan(&templates); err != nil || templates != custom {
		t.Errorf("custom templates = %q (%v), want them untouched", templates, err)
	}
	if err := raw.QueryRow(`SELECT content_templates FROM ticket_projects WHERE id = 'tp-default'`).Scan(&templates); err != nil ||
		templates != `{"title":"","description":"","supersedeComment":""}` {
		t.Errorf("existing default templates = %q (%v), want the stored value untouched", templates, err)
	}
	var repoProject, defaultProject, externalID string
	if err := raw.QueryRow(`SELECT ticket_project_id FROM repositories WHERE id = 'repo-1'`).Scan(&repoProject); err != nil || repoProject != "tp-custom" {
		t.Errorf("repository ticket project = %q (%v), want tp-custom", repoProject, err)
	}
	if err := raw.QueryRow(`SELECT default_ticket_project_id FROM installation_settings`).Scan(&defaultProject); err != nil || defaultProject != "tp-default" {
		t.Errorf("default ticket project = %q (%v), want tp-default", defaultProject, err)
	}
	var target, handsOff sql.NullString
	if err := raw.QueryRow(`SELECT external_id, target_state, hands_off_at FROM update_tickets WHERE id = 'ut-1'`).Scan(&externalID, &target, &handsOff); err != nil {
		t.Fatalf("read the ticket: %v", err)
	}
	if externalID != "EXT-1" || target.Valid || handsOff.Valid {
		t.Errorf("ticket = %q %v %v, want its link kept and the new columns NULL", externalID, target, handsOff)
	}

	if _, err := raw.Exec(`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at)
		VALUES ('tp-new', 'int-1', 'p3', 'New', '{}', '{}', '` + at + `', '` + at + `')`); err != nil {
		t.Fatalf("insert a project after the migration: %v", err)
	}
	if err := raw.QueryRow(`SELECT content_templates FROM ticket_projects WHERE id = 'tp-new'`).Scan(&templates); err != nil ||
		templates != `{"title":"","description":"","updateComment":"","supersedeComment":""}` {
		t.Errorf("new project templates = %q (%v), want the four-key default", templates, err)
	}
	if _, err := raw.Exec(`UPDATE update_tickets SET target_state = 'open' WHERE id = 'ut-1'`); err == nil {
		t.Error("target_state accepted open, want the CHECK to refuse it")
	}
	if _, err := raw.Exec(`UPDATE update_tickets SET target_state = 'superseded' WHERE id = 'ut-1'`); err != nil {
		t.Errorf("target_state refused superseded: %v", err)
	}
}
