package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

func (e *jobEnv) configState(t *testing.T, repo string) string {
	t.Helper()
	var state string
	if err := e.db.Reader.QueryRow(`SELECT config_state FROM repositories WHERE id = ?`, repo).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// An attempt Renovate skipped for a missing config ends the job succeeded, its job_runs row
// no_config, and the repository's config state missing (specs.html section 2.7); a failed attempt
// says nothing about it, and no run writes present: that stays the installation sync's.
func TestANoConfigAttemptEndsNoConfigAndRecordsTheConfigState(t *testing.T) {
	e := newJobEnv(t, 2)
	ctx := context.Background()
	repo, other := e.repos[0], e.repos[1]

	e.enqueue(t, repo, queue.PriorityHigh)
	job := e.claim(t)
	e.clock.Advance(5 * time.Second)
	run := queue.RunRecord{ExitCode: intPtr(0), ReportStatus: queue.ReportOK, NoConfig: true}
	if err := e.q.CompleteRun(ctx, job.ID, "0.not-the-owner", run); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("CompleteRun by another owner = %v, want ErrLeaseLost", err)
	}
	if got := e.configState(t, repo); got != "unknown" {
		t.Fatalf("a refused result changed the config state to %q", got)
	}

	if err := e.q.CompleteRun(ctx, job.ID, job.LeaseOwner, run); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	if got := e.row(t, job.ID).State; got != queue.StateSucceeded {
		t.Errorf("job state = %q, want succeeded: skipping a repository is not a failure", got)
	}
	rows := e.runs(t, job.ID)
	if len(rows) != 1 || rows[0].Outcome != "no_config" || rows[0].ErrorCode.Valid {
		t.Fatalf("job_runs = %+v, want one no_config row without an error", rows)
	}
	if got := e.configState(t, repo); got != "missing" {
		t.Errorf("config state = %q, want missing", got)
	}
	if got := e.configState(t, other); got != "unknown" {
		t.Errorf("another repository's config state = %q, want unknown", got)
	}

	// A repeated result is a no-op.
	if err := e.q.CompleteRun(ctx, job.ID, job.LeaseOwner, run); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if rows := e.runs(t, job.ID); len(rows) != 1 {
		t.Errorf("a repeated result stored %d rows", len(rows))
	}

	// A failed attempt leaves the state as it is.
	e.enqueue(t, repo, queue.PriorityHigh)
	job = e.claim(t)
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{Run: &queue.RunRecord{NoConfig: true, ErrorCode: "renovate_exit"}}); err != nil {
		t.Fatal(err)
	}
	if rows := e.runs(t, job.ID); len(rows) != 1 || rows[0].Outcome != "failed" {
		t.Errorf("a failed attempt's job_runs = %+v, want failed", rows)
	}
	if got := e.configState(t, repo); got != "missing" {
		t.Errorf("a failed attempt changed the config state to %q", got)
	}

	// A run that went on says nothing about the config either: only the installation sync records present.
	e.clock.Advance(time.Hour)
	job = e.claim(t)
	if err := e.q.CompleteRun(ctx, job.ID, job.LeaseOwner, queue.RunRecord{ExitCode: intPtr(0), ReportStatus: queue.ReportOK}); err != nil {
		t.Fatal(err)
	}
	if rows := e.runs(t, job.ID); len(rows) != 2 || rows[1].Outcome != "succeeded" {
		t.Errorf("job_runs = %+v, want the failed attempt and then a succeeded one", rows)
	}
	if got := e.configState(t, repo); got != "missing" {
		t.Errorf("a succeeded run changed the config state to %q, want missing left alone", got)
	}
}

// A result with no record says nothing about the config.
func TestACompletionWithoutARecordLeavesTheConfigState(t *testing.T) {
	e := newJobEnv(t, 1)
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	if err := e.q.Complete(context.Background(), job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if got := e.configState(t, e.repos[0]); got != "unknown" {
		t.Errorf("config state = %q, want unknown", got)
	}
}
