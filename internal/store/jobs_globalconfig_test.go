package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

type capturedRow struct {
	Content, Source, CapturedAt sql.NullString
	UpdatedAt                   string
}

func (e *jobEnv) capture(t *testing.T, installation string) (capturedRow, bool) {
	t.Helper()

	var r capturedRow
	err := e.db.Writer.QueryRow(
		`SELECT global_config_content, global_config_source, global_config_captured_at, updated_at
		 FROM installation_settings WHERE installation_id = ?`, installation,
	).Scan(&r.Content, &r.Source, &r.CapturedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		t.Fatalf("read settings of %s: %v", installation, err)
	}
	return r, true
}

// seedSecondInstallation adds a removed installation with one repository and a settings row
// holding a config of its own.
func (e *jobEnv) seedSecondInstallation(t *testing.T) {
	t.Helper()

	const at = "2026-11-14T12:00:00.000Z"
	for _, q := range []string{
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-2', 2, 2, 'other', 'Organization', 'removed', 'all', '` + at + `', '` + at + `')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-other', 'inst-2', 99, 'other/repo', 'main', '` + at + `', '` + at + `')`,
		`INSERT INTO installation_settings (installation_id, global_config_content, global_config_source, global_config_captured_at, updated_at)
		 VALUES ('inst-2', 'other', 'mounted', '` + at + `', '` + at + `')`,
	} {
		if _, err := e.db.Writer.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestAnAttemptStoresItsGlobalConfigOnTheJobsInstallationOnly(t *testing.T) {
	e := newJobEnv(t, 1)
	e.seedSecondInstallation(t)
	ctx := context.Background()
	const rawConfig = "module.exports = {\r\n  allowedCommands: ['^x$'],\r\n};\n"

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	e.clock.Advance(5 * time.Second)
	run := queue.RunRecord{ReportStatus: queue.ReportOK, GlobalConfig: &queue.GlobalConfigCapture{Source: queue.GlobalConfigSourceRepository, Content: rawConfig}}
	if err := e.q.CompleteRun(ctx, job.ID, "0.not-the-owner", run); err == nil {
		t.Fatal("CompleteRun by another owner succeeded")
	}
	if _, ok := e.capture(t, "inst-1"); ok {
		t.Fatal("a refused result stored a global config")
	}

	if err := e.q.CompleteRun(ctx, job.ID, job.LeaseOwner, run); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	got, ok := e.capture(t, "inst-1")
	if !ok || got.Content.String != rawConfig || got.Source.String != "repository" || got.CapturedAt.String != formatForTest(e.clock.Now()) || got.UpdatedAt != formatForTest(e.clock.Now()) {
		t.Fatalf("captured = %+v, %v; want the raw text, its source and the time of the result", got, ok)
	}
	if other, _ := e.capture(t, "inst-2"); other.Content.String != "other" || other.Source.String != "mounted" {
		t.Errorf("the config was stored for an installation the job does not belong to: %+v", other)
	}

	// A result without a config leaves the stored copy as it is, on either outcome.
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job = e.claim(t)
	e.clock.Advance(5 * time.Second)
	if err := e.q.CompleteRun(ctx, job.ID, job.LeaseOwner, queue.RunRecord{ReportStatus: queue.ReportOK}); err != nil {
		t.Fatalf("CompleteRun without a config: %v", err)
	}
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job = e.claim(t)
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{Run: &queue.RunRecord{ErrorCode: "global_config"}}); err != nil {
		t.Fatalf("Fail without a config: %v", err)
	}
	if again, _ := e.capture(t, "inst-1"); again != got {
		t.Errorf("a result without a config changed the stored copy: %+v, was %+v", again, got)
	}

	// A failed attempt that read the config replaces it.
	e.clock.Advance(time.Hour)
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job = e.claim(t)
	next := &queue.GlobalConfigCapture{Source: queue.GlobalConfigSourceMounted, Content: "{}"}
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{Run: &queue.RunRecord{ErrorCode: "renovate_exit", GlobalConfig: next}}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got, _ = e.capture(t, "inst-1")
	if got.Content.String != "{}" || got.Source.String != "mounted" || got.CapturedAt.String != formatForTest(e.clock.Now()) {
		t.Errorf("after a failed attempt that read the config: %+v", got)
	}
}
