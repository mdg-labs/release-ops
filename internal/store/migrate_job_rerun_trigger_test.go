package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// A database whose jobs include a running job with rerun_requested set, with job runs and events
// hanging off it, upgrades through the migration that records the mid-run trigger: every row
// stays, the job keeps its flag, the new columns are NULL and the trigger column is checked.
func TestJobRerunTriggerMigrationKeepsExistingData(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := loadEmbedded(t, ctx)
	idx := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "job_rerun_trigger") {
			idx = i
		}
	}
	if idx < 1 {
		t.Fatalf("migration job_rerun_trigger not found after another migration in %d migrations", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	const at = "2026-11-15T08:00:00.000Z"
	for _, stmt := range []string{
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1000, 4242, 'acme', 'Organization', 'active', 'selected', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 7001, 'acme/widgets', 'main', '` + at + `', '` + at + `')`,
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, rerun_requested, "trigger", matched_markers, created_at)
		 VALUES ('job-running', 'renovate_run', 'repo-1', 'high', 'running', '` + at + `', 1, 1, 'push', '["rebase-check"]', '` + at + `')`,
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		 VALUES ('job-done', 'renovate_run', 'repo-1', 'normal', 'succeeded', '` + at + `', 'schedule', '` + at + `')`,
		`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome) VALUES ('run-1', 'job-done', 1, '` + at + `', 'succeeded')`,
		`INSERT INTO job_events (id, job_id, at, kind) VALUES ('event-1', 'job-running', '` + at + `', 'claimed')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all[:idx+1]); err != nil {
		t.Fatalf("apply job_rerun_trigger: %v", err)
	}

	for table, want := range map[string]int{"jobs": 2, "job_runs": 1, "job_events": 1} {
		var n int
		if err := raw.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != want {
			t.Errorf("%s has %d rows after the migration (%v), want %d", table, n, err, want)
		}
	}
	var requested int
	var trigger, markers, rerunTrigger, rerunMarkers, rerunWebhook sql.NullString
	if err := raw.QueryRow(
		`SELECT rerun_requested, "trigger", matched_markers, rerun_trigger, rerun_markers, rerun_webhook_received_at FROM jobs WHERE id = 'job-running'`,
	).Scan(&requested, &trigger, &markers, &rerunTrigger, &rerunMarkers, &rerunWebhook); err != nil {
		t.Fatalf("read the running job: %v", err)
	}
	if requested != 1 || trigger.String != "push" || markers.String != `["rebase-check"]` {
		t.Errorf("running job = %d %v %v after the migration, want its flag, trigger and markers kept", requested, trigger, markers)
	}
	if rerunTrigger.Valid || rerunMarkers.Valid || rerunWebhook.Valid {
		t.Errorf("recorded columns = %v %v %v, want NULL for a job that existed before", rerunTrigger, rerunMarkers, rerunWebhook)
	}

	if _, err := raw.Exec(`UPDATE jobs SET rerun_trigger = 'dashboard_checkbox', rerun_markers = '["a"]' WHERE id = 'job-running'`); err != nil {
		t.Errorf("record a valid trigger: %v", err)
	}
	if _, err := raw.Exec(`UPDATE jobs SET rerun_trigger = 'nonsense' WHERE id = 'job-running'`); err == nil {
		t.Error("an unknown rerun_trigger was accepted, want the CHECK to refuse it")
	}
	if _, err := raw.Exec(`UPDATE jobs SET rerun_markers = 'not json' WHERE id = 'job-running'`); err == nil {
		t.Error("rerun_markers that is not JSON was accepted, want the CHECK to refuse it")
	}
}
