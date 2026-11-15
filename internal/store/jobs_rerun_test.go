package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

type rerunJob struct {
	Priority, Trigger string
	Markers, Webhook  sql.NullString
	Requested         int
	RerunTrigger      sql.NullString
	RerunMarkers      sql.NullString
	RerunWebhook      sql.NullString
}

func (e *jobEnv) rerunRow(t *testing.T, id string) rerunJob {
	t.Helper()

	var r rerunJob
	err := e.db.Writer.QueryRow(
		`SELECT priority, "trigger", matched_markers, webhook_received_at, rerun_requested, rerun_trigger, rerun_markers, rerun_webhook_received_at
		 FROM jobs WHERE id = ?`, id,
	).Scan(&r.Priority, &r.Trigger, &r.Markers, &r.Webhook, &r.Requested, &r.RerunTrigger, &r.RerunMarkers, &r.RerunWebhook)
	if err != nil {
		t.Fatalf("read job %s: %v", id, err)
	}
	return r
}

// queuedFor is the repository's queued job.
func (e *jobEnv) queuedFor(t *testing.T, repo string) (string, rerunJob) {
	t.Helper()

	var id string
	if err := e.db.Writer.QueryRow(`SELECT id FROM jobs WHERE repository_id = ? AND state = 'queued'`, repo).Scan(&id); err != nil {
		t.Fatalf("no queued job for %s: %v", repo, err)
	}
	return id, e.rerunRow(t, id)
}

func (e *jobEnv) tick(t *testing.T, repo string, prio queue.Priority, trigger queue.Trigger, markers []string, arrived *time.Time) queue.EnqueueResult {
	t.Helper()

	res, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{
		RepositoryID: repo, Priority: prio, Trigger: trigger, MatchedMarkers: markers, WebhookReceivedAt: arrived,
	})
	if err != nil {
		t.Fatalf("Enqueue(%s): %v", trigger, err)
	}
	return res
}

func arrival(sec int) *time.Time {
	v := time.Date(2026, 11, 14, 12, 0, sec, 0, time.UTC)
	return &v
}

func stamp(sec int) string { return formatForTest(*arrival(sec)) }

func wantText(t *testing.T, what string, got sql.NullString, want string) {
	t.Helper()

	if want == "" && !got.Valid {
		return
	}
	if !got.Valid || got.String != want {
		t.Errorf("%s = %v, want %q", what, got, want)
	}
}

// A push job is running and an admin ticks a dashboard checkbox: the follow-up is the tick's.
func TestFollowUpCarriesTheTriggerAndMarkersThatCausedIt(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh) // a push
	job := e.claim(t)

	res := e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(5))
	if res.Outcome != queue.EnqueueRerunRequested {
		t.Fatalf("tick mid-run = %s, want rerun_requested", res.Outcome)
	}
	running := e.rerunRow(t, job.ID)
	if running.Requested != 1 || running.Trigger != "push" || running.Markers.Valid {
		t.Errorf("running job = %+v, want rerun requested and its own trigger untouched", running)
	}
	wantText(t, "rerun_trigger", running.RerunTrigger, "dashboard_checkbox")
	wantText(t, "rerun_markers", running.RerunMarkers, `["approve-branch=x"]`)
	wantText(t, "rerun_webhook_received_at", running.RerunWebhook, stamp(5))

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, follow := e.queuedFor(t, e.repos[0])
	if follow.Priority != "high" || follow.Trigger != "dashboard_checkbox" {
		t.Errorf("follow-up = priority %s trigger %s, want high dashboard_checkbox", follow.Priority, follow.Trigger)
	}
	wantText(t, "follow-up markers", follow.Markers, `["approve-branch=x"]`)
	wantText(t, "follow-up webhook_received_at", follow.Webhook, stamp(5))
	if follow.Requested != 0 || follow.RerunTrigger.Valid || follow.RerunMarkers.Valid || follow.RerunWebhook.Valid {
		t.Errorf("follow-up = %+v, want no rerun state of its own", follow)
	}
}

func TestSeveralTicksDuringOneRunGiveOneFollowUpWithBothMarkers(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	// The later delivery names the earlier arrival: the earliest one is kept, not the last.
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(9))
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPRCheckbox, []string{"rebase-check", "approve-branch=x"}, arrival(4))
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, nil, nil)
	// A normal-priority trigger records nothing.
	if res := e.tick(t, e.repos[0], queue.PriorityNormal, queue.TriggerPolling, []string{"ignored"}, arrival(1)); res.Outcome != queue.EnqueueDropped {
		t.Fatalf("normal trigger mid-run = %s, want dropped", res.Outcome)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if n := e.count(t, `repository_id = ? AND state = 'queued'`, e.repos[0]); n != 1 {
		t.Fatalf("%d queued jobs, want one follow-up", n)
	}
	_, follow := e.queuedFor(t, e.repos[0])
	if follow.Trigger != "push" {
		t.Errorf("follow-up trigger = %s, want the latest trigger, push", follow.Trigger)
	}
	wantText(t, "follow-up markers", follow.Markers, `["approve-branch=x","rebase-check"]`)
	wantText(t, "follow-up webhook_received_at", follow.Webhook, stamp(4))
}

// The follow-up merges into a job that is already queued the way a coalesce does.
func TestFollowUpMergesIntoAnAlreadyQueuedJob(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(5))
	// A queued job beside a running one cannot come from Enqueue; it is a row a restore or an
	// older version left.
	if _, err := e.db.Writer.Exec(
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", matched_markers, webhook_received_at, created_at)
		 VALUES ('queued-normal', 'renovate_run', ?, 'normal', 'queued', 't', 'schedule', '["old"]', ?, 't')`,
		e.repos[0], stamp(8),
	); err != nil {
		t.Fatalf("insert queued job: %v", err)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	id, merged := e.queuedFor(t, e.repos[0])
	if id != "queued-normal" {
		t.Fatalf("queued job = %s, want the existing one kept", id)
	}
	if merged.Priority != "high" || merged.Trigger != "dashboard_checkbox" {
		t.Errorf("merged job = priority %s trigger %s, want high dashboard_checkbox", merged.Priority, merged.Trigger)
	}
	wantText(t, "merged markers", merged.Markers, `["old","approve-branch=x"]`)
	wantText(t, "merged webhook_received_at", merged.Webhook, stamp(5))

	// An already high-priority queued job keeps its own trigger, as a coalesce does.
	e2 := newJobEnv(t, 1)
	e2.enqueue(t, e2.repos[0], queue.PriorityHigh)
	job2 := e2.claim(t)
	e2.tick(t, e2.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(5))
	if _, err := e2.db.Writer.Exec(
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		 VALUES ('queued-high', 'renovate_run', ?, 'high', 'queued', 't', 'run_now', 't')`, e2.repos[0],
	); err != nil {
		t.Fatalf("insert queued job: %v", err)
	}
	if err := e2.q.Complete(ctx, job2.ID, job2.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, kept := e2.queuedFor(t, e2.repos[0])
	if kept.Trigger != "run_now" {
		t.Errorf("trigger = %s, want the queued high-priority job's own run_now", kept.Trigger)
	}
	wantText(t, "markers", kept.Markers, `["approve-branch=x"]`)
}

// A failed run that is retried is the follow-up: it keeps its own trigger and markers and
// folds the recorded ones in; the recorded state is cleared.
func TestRetryKeepsItsOwnTriggerAndMergesTheRecordedOne(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, []string{"rebase-check"}, arrival(2))
	job := e.claim(t)
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(5))

	if out, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{}); err != nil || out != queue.FailRequeued {
		t.Fatalf("Fail = %s, %v; want requeued", out, err)
	}
	retry := e.rerunRow(t, job.ID)
	if retry.Priority != "high" || retry.Trigger != "push" {
		t.Errorf("retry = priority %s trigger %s, want high push", retry.Priority, retry.Trigger)
	}
	wantText(t, "retry markers", retry.Markers, `["rebase-check","approve-branch=x"]`)
	wantText(t, "retry webhook_received_at", retry.Webhook, stamp(2))
	if retry.Requested != 0 || retry.RerunTrigger.Valid || retry.RerunMarkers.Valid || retry.RerunWebhook.Valid {
		t.Errorf("retry = %+v, want the recorded state cleared", retry)
	}
}

func TestRetryOfANormalJobRaisesItToTheRecordedTrigger(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.tick(t, e.repos[0], queue.PriorityNormal, queue.TriggerSchedule, nil, nil)
	job := e.claim(t)
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPRClosed, nil, arrival(3))

	if err := e.q.Release(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Release: %v", err)
	}
	retry := e.rerunRow(t, job.ID)
	if retry.Priority != "high" || retry.Trigger != "pr_closed" {
		t.Errorf("released job = priority %s trigger %s, want high pr_closed", retry.Priority, retry.Trigger)
	}
	wantText(t, "webhook_received_at", retry.Webhook, stamp(3))
}

// A job that was running when the database was upgraded has rerun_requested set and none of the
// recorded columns: its follow-up carries the finished job's trigger and markers, as before.
func TestFollowUpOfARerunRequestedBeforeTheColumnsExistedUsesTheFinishedJob(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerPush, []string{"rebase-check"}, arrival(2))
	job := e.claim(t)
	if _, err := e.db.Writer.Exec(`UPDATE jobs SET rerun_requested = 1 WHERE id = ?`, job.ID); err != nil {
		t.Fatalf("flag rerun: %v", err)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, follow := e.queuedFor(t, e.repos[0])
	if follow.Priority != "high" || follow.Trigger != "push" {
		t.Errorf("follow-up = priority %s trigger %s, want high push", follow.Priority, follow.Trigger)
	}
	wantText(t, "follow-up markers", follow.Markers, `["rebase-check"]`)
	if follow.Webhook.Valid {
		t.Errorf("follow-up webhook_received_at = %v, want NULL as before", follow.Webhook)
	}

	// A tick after the upgrade records itself on top of the old flag.
	e2 := newJobEnv(t, 1)
	e2.enqueue(t, e2.repos[0], queue.PriorityHigh)
	job2 := e2.claim(t)
	if _, err := e2.db.Writer.Exec(`UPDATE jobs SET rerun_requested = 1 WHERE id = ?`, job2.ID); err != nil {
		t.Fatalf("flag rerun: %v", err)
	}
	e2.tick(t, e2.repos[0], queue.PriorityHigh, queue.TriggerPRCheckbox, []string{"rebase-check"}, arrival(7))
	if err := e2.q.Complete(ctx, job2.ID, job2.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	_, after := e2.queuedFor(t, e2.repos[0])
	if after.Trigger != "pr_checkbox" {
		t.Errorf("follow-up trigger = %s, want pr_checkbox", after.Trigger)
	}
}

// The reaper ends an attempt whose lease expired: with attempts used up, the follow-up still
// carries the recorded trigger.
func TestReapedJobFollowUpCarriesTheRecordedTrigger(t *testing.T) {
	e := newJobEnv(t, 1)
	cfg := queue.DefaultConfig()
	cfg.MaxAttempts = 1
	q := store.NewJobQueue(e.db, store.JobQueueOptions{Config: cfg, Now: e.clock.Now})

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	if _, err := q.Claim(context.Background()); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	e.tick(t, e.repos[0], queue.PriorityHigh, queue.TriggerDashboardCheckbox, []string{"approve-branch=x"}, arrival(5))
	e.clock.Advance(time.Hour)
	if res, err := q.Reap(context.Background()); err != nil || res.Failed != 1 {
		t.Fatalf("Reap = %+v, %v; want the job failed", res, err)
	}
	_, follow := e.queuedFor(t, e.repos[0])
	if follow.Trigger != "dashboard_checkbox" {
		t.Errorf("follow-up trigger = %s, want dashboard_checkbox", follow.Trigger)
	}
	wantText(t, "follow-up markers", follow.Markers, `["approve-branch=x"]`)
}
