package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type jobEnv struct {
	db    *store.DB
	q     *store.JobQueue
	clock *fakeClock
	repos []string
}

// newJobEnv builds a queue over a migrated database with a fake clock and no jitter
// (rand returns 0.5, the middle of the jitter range).
func newJobEnv(t *testing.T, repos int) *jobEnv {
	t.Helper()

	db := storetest.OpenMigrated(t)
	clock := newFakeClock()
	return &jobEnv{
		db:    db,
		clock: clock,
		repos: storetest.SeedRepositories(t, db, repos),
		q: store.NewJobQueue(db, store.JobQueueOptions{
			Now:  clock.Now,
			Rand: func() float64 { return 0.5 },
		}),
	}
}

func (e *jobEnv) enqueue(t *testing.T, repo string, prio queue.Priority) queue.EnqueueResult {
	t.Helper()

	res, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{
		RepositoryID: repo,
		Priority:     prio,
		Trigger:      queue.TriggerPush,
	})
	if err != nil {
		t.Fatalf("Enqueue(%s, %s): %v", repo, prio, err)
	}
	return res
}

func (e *jobEnv) claim(t *testing.T) *queue.ClaimedJob {
	t.Helper()

	job, err := e.q.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return job
}

func (e *jobEnv) wantNoClaim(t *testing.T) {
	t.Helper()

	if job, err := e.q.Claim(context.Background()); !errors.Is(err, queue.ErrNoJob) {
		t.Fatalf("Claim = %+v, %v; want ErrNoJob", job, err)
	}
}

type jobRow struct {
	State          string
	Priority       string
	Attempts       int
	RunAfter       string
	RerunRequested int
	LeaseOwner     sql.NullString
	LeaseExpiresAt sql.NullString
	StartedAt      sql.NullString
	Trigger        string
	Markers        sql.NullString
}

func (e *jobEnv) row(t *testing.T, id string) jobRow {
	t.Helper()

	var r jobRow
	err := e.db.Writer.QueryRow(
		`SELECT state, priority, attempts, run_after, rerun_requested, lease_owner, lease_expires_at, started_at, "trigger", matched_markers
		 FROM jobs WHERE id = ?`, id,
	).Scan(&r.State, &r.Priority, &r.Attempts, &r.RunAfter, &r.RerunRequested, &r.LeaseOwner, &r.LeaseExpiresAt, &r.StartedAt, &r.Trigger, &r.Markers)
	if err != nil {
		t.Fatalf("read job %s: %v", id, err)
	}
	return r
}

func (e *jobEnv) count(t *testing.T, where string, args ...any) int {
	t.Helper()

	var n int
	if err := e.db.Writer.QueryRow(`SELECT COUNT(*) FROM jobs WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatalf("count jobs where %s: %v", where, err)
	}
	return n
}

func (e *jobEnv) setCap(t *testing.T, n int) {
	t.Helper()

	if _, err := e.db.Writer.Exec(
		`INSERT INTO instance_settings (id, concurrency_cap, updated_at) VALUES (1, ?, 't')
		 ON CONFLICT (id) DO UPDATE SET concurrency_cap = excluded.concurrency_cap`, n,
	); err != nil {
		t.Fatalf("set concurrency cap: %v", err)
	}
}

func TestEnqueueCoalescesQueuedJob(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	first, err := e.q.Enqueue(ctx, queue.EnqueueParams{
		RepositoryID: e.repos[0], Priority: queue.PriorityNormal, Trigger: queue.TriggerSchedule,
	})
	if err != nil || first.Outcome != queue.EnqueueQueued {
		t.Fatalf("first Enqueue = %+v, %v; want queued", first, err)
	}
	arrived := time.Date(2026, 11, 14, 11, 59, 58, 0, time.UTC)
	second, err := e.q.Enqueue(ctx, queue.EnqueueParams{
		RepositoryID: e.repos[0], Priority: queue.PriorityHigh, Trigger: queue.TriggerPRCheckbox,
		MatchedMarkers: []string{"rebase-check"}, WebhookReceivedAt: &arrived,
	})
	if err != nil || second.Outcome != queue.EnqueueCoalesced || second.JobID != first.JobID {
		t.Fatalf("second Enqueue = %+v, %v; want coalesced into %s", second, err, first.JobID)
	}
	third, err := e.q.Enqueue(ctx, queue.EnqueueParams{
		RepositoryID: e.repos[0], Priority: queue.PriorityNormal, Trigger: queue.TriggerPolling,
		MatchedMarkers: []string{"rebase-check", "approve-branch=x"},
	})
	if err != nil || third.Outcome != queue.EnqueueCoalesced {
		t.Fatalf("third Enqueue = %+v, %v; want coalesced", third, err)
	}

	if n := e.count(t, `repository_id = ?`, e.repos[0]); n != 1 {
		t.Fatalf("%d job rows for the repository, want 1", n)
	}
	row := e.row(t, first.JobID)
	if row.Priority != "high" || row.Trigger != "pr_checkbox" {
		t.Errorf("job = priority %s trigger %s, want the high pr_checkbox trigger to win", row.Priority, row.Trigger)
	}
	if !row.Markers.Valid || row.Markers.String != `["rebase-check","approve-branch=x"]` {
		t.Errorf("matched_markers = %v, want the union of both ticks", row.Markers)
	}
}

func TestEnqueueDuringRunSetsRerunAndQueuesOneFollowUp(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	first := e.enqueue(t, e.repos[0], queue.PriorityNormal)
	job := e.claim(t)

	if res := e.enqueue(t, e.repos[0], queue.PriorityNormal); res.Outcome != queue.EnqueueDropped {
		t.Errorf("normal trigger mid-run = %s, want dropped", res.Outcome)
	}
	if e.row(t, first.JobID).RerunRequested != 0 {
		t.Fatal("a normal trigger mid-run set rerun_requested")
	}
	for range 3 {
		if res := e.enqueue(t, e.repos[0], queue.PriorityHigh); res.Outcome != queue.EnqueueRerunRequested || res.JobID != first.JobID {
			t.Fatalf("high trigger mid-run = %+v, want rerun_requested on %s", res, first.JobID)
		}
	}
	if n := e.count(t, `repository_id = ? AND state = 'queued'`, e.repos[0]); n != 0 {
		t.Fatalf("%d queued jobs while the repository's job runs, want 0", n)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if n := e.count(t, `repository_id = ? AND state = 'queued' AND priority = 'high'`, e.repos[0]); n != 1 {
		t.Fatalf("%d follow-up jobs after the run, want exactly 1", n)
	}
	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("repeated Complete: %v", err)
	}
	if n := e.count(t, `repository_id = ?`, e.repos[0]); n != 2 {
		t.Fatalf("%d job rows after a repeated Complete, want the finished job and one follow-up", n)
	}
}

func TestClaimOrdersByPriorityThenAge(t *testing.T) {
	e := newJobEnv(t, 3)
	e.setCap(t, 4)

	normal := e.enqueue(t, e.repos[0], queue.PriorityNormal)
	e.clock.Advance(time.Second)
	high1 := e.enqueue(t, e.repos[1], queue.PriorityHigh)
	e.clock.Advance(time.Second)
	high2 := e.enqueue(t, e.repos[2], queue.PriorityHigh)

	for i, want := range []string{high1.JobID, high2.JobID, normal.JobID} {
		if got := e.claim(t).ID; got != want {
			t.Fatalf("claim %d = %s, want %s", i+1, got, want)
		}
	}
}

func TestClaimHonoursRunAfterAndOneRunningJobPerRepository(t *testing.T) {
	e := newJobEnv(t, 1)

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	running := e.claim(t)

	// The partial unique indexes allow one queued and one running job per repository, so a
	// queued row can sit beside the running one; the claim must still skip it.
	if _, err := e.db.Writer.Exec(
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		 VALUES ('queued-beside', 'renovate_run', ?, 'high', 'queued', '2026-11-14T11:00:00.000Z', 'push', '2026-11-14T11:00:00.000Z')`,
		e.repos[0],
	); err != nil {
		t.Fatalf("insert queued job: %v", err)
	}
	e.wantNoClaim(t)

	if err := e.q.Complete(context.Background(), running.ID, running.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := e.claim(t).ID; got != "queued-beside" {
		t.Fatalf("claim after the run = %s, want queued-beside", got)
	}
}

func TestClaimWaitsForRunAfter(t *testing.T) {
	e := newJobEnv(t, 1)

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	if _, err := e.q.Fail(context.Background(), job.ID, job.LeaseOwner, queue.FailParams{}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	e.wantNoClaim(t)
	e.clock.Advance(time.Hour)
	if got := e.claim(t); got.ID != job.ID || got.Attempt != 2 {
		t.Fatalf("claim after the backoff = %+v, want job %s attempt 2", got, job.ID)
	}
}

func TestConcurrencyCapAndReservedHighPrioritySlot(t *testing.T) {
	t.Run("default cap of 2 keeps the last slot for high priority", func(t *testing.T) {
		e := newJobEnv(t, 4)
		e.enqueue(t, e.repos[0], queue.PriorityNormal)
		e.enqueue(t, e.repos[1], queue.PriorityNormal)
		h1 := e.enqueue(t, e.repos[2], queue.PriorityHigh)
		e.clock.Advance(time.Second)
		h2 := e.enqueue(t, e.repos[3], queue.PriorityHigh)

		if got := e.claim(t).ID; got != h1.JobID {
			t.Fatalf("first claim = %s, want the high job %s", got, h1.JobID)
		}
		// One job runs, the last free slot is reserved: the normal job must wait.
		if got := e.claim(t).ID; got != h2.JobID {
			t.Fatalf("second claim = %s, want the high job %s", got, h2.JobID)
		}
		e.wantNoClaim(t)
	})

	t.Run("a normal job never takes the last free slot", func(t *testing.T) {
		e := newJobEnv(t, 3)
		n1 := e.enqueue(t, e.repos[0], queue.PriorityNormal)
		e.clock.Advance(time.Second)
		e.enqueue(t, e.repos[1], queue.PriorityNormal)

		if got := e.claim(t).ID; got != n1.JobID {
			t.Fatalf("first claim = %s, want %s", got, n1.JobID)
		}
		e.wantNoClaim(t)

		h := e.enqueue(t, e.repos[2], queue.PriorityHigh)
		if got := e.claim(t).ID; got != h.JobID {
			t.Fatalf("claim with the last slot free = %s, want the high job %s", got, h.JobID)
		}
		e.wantNoClaim(t)
	})

	t.Run("a cap of 1 reserves nothing", func(t *testing.T) {
		e := newJobEnv(t, 2)
		e.setCap(t, 1)
		e.enqueue(t, e.repos[0], queue.PriorityNormal)
		e.enqueue(t, e.repos[1], queue.PriorityHigh)
		if job := e.claim(t); job.Priority != queue.PriorityHigh {
			t.Fatalf("first claim = %+v, want the high job", job)
		}
		e.wantNoClaim(t)
	})

	t.Run("the cap is read at every claim", func(t *testing.T) {
		e := newJobEnv(t, 3)
		for _, r := range e.repos {
			e.enqueue(t, r, queue.PriorityHigh)
		}
		e.claim(t)
		e.claim(t)
		e.wantNoClaim(t)
		e.setCap(t, 3)
		e.claim(t)
		if n := e.count(t, `state = 'running'`); n != 3 {
			t.Fatalf("%d running jobs, want 3", n)
		}
	})
}

func TestClaimNamesDistinctSlotsBelowTheCap(t *testing.T) {
	e := newJobEnv(t, 3)
	e.setCap(t, 3)
	for _, r := range e.repos {
		e.enqueue(t, r, queue.PriorityHigh)
	}

	a, b, c := e.claim(t), e.claim(t), e.claim(t)
	if a.Slot != 0 || b.Slot != 1 || c.Slot != 2 {
		t.Fatalf("slots = %d %d %d, want 0 1 2", a.Slot, b.Slot, c.Slot)
	}
	if a.RepositoryFullName == "" || a.LeaseOwner == b.LeaseOwner {
		t.Fatalf("claim = %+v: want the repository name and a lease owner per claim", a)
	}

	if err := e.q.Complete(context.Background(), b.ID, b.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	e.enqueue(t, b.RepositoryID, queue.PriorityHigh)
	if again := e.claim(t); again.Slot != 1 {
		t.Fatalf("slot after slot 1 freed = %d, want 1", again.Slot)
	}
}

func TestClaimSetsLeaseAndHeartbeatRenewsIt(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	cfg := queue.DefaultConfig()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)

	row := e.row(t, job.ID)
	if row.State != "running" || row.LeaseOwner.String != job.LeaseOwner {
		t.Fatalf("claimed job = %+v, want running under lease owner %s", row, job.LeaseOwner)
	}
	if want := "2026-11-14T12:02:00.000Z"; row.LeaseExpiresAt.String != want || cfg.LeaseDuration != 2*time.Minute {
		t.Fatalf("lease_expires_at = %s, want %s", row.LeaseExpiresAt.String, want)
	}
	if row.StartedAt.Valid {
		t.Fatal("started_at is set by the claim; the first heartbeat sets it")
	}

	e.clock.Advance(90 * time.Second)
	res, err := e.q.Heartbeat(ctx, job.ID, job.LeaseOwner)
	if err != nil || res.Abort {
		t.Fatalf("Heartbeat = %+v, %v; want no abort", res, err)
	}
	row = e.row(t, job.ID)
	if want := "2026-11-14T12:03:30.000Z"; row.LeaseExpiresAt.String != want {
		t.Errorf("lease_expires_at after a heartbeat = %s, want %s", row.LeaseExpiresAt.String, want)
	}
	if want := "2026-11-14T12:01:30.000Z"; row.StartedAt.String != want {
		t.Errorf("started_at = %s, want the first heartbeat %s", row.StartedAt.String, want)
	}
	e.clock.Advance(time.Minute)
	if _, err := e.q.Heartbeat(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("second Heartbeat: %v", err)
	}
	if got := e.row(t, job.ID).StartedAt.String; got != "2026-11-14T12:01:30.000Z" {
		t.Errorf("started_at moved to %s on a later heartbeat", got)
	}

	for name, owner := range map[string]string{"wrong owner": "0.deadbeef", "empty owner": ""} {
		res, err := e.q.Heartbeat(ctx, job.ID, owner)
		if err != nil || !res.Abort || res.Reason != queue.AbortLeaseLost {
			t.Errorf("Heartbeat with a %s = %+v, %v; want a lease_lost abort", name, res, err)
		}
	}
}

func TestResultRequiresTheLease(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)

	if err := e.q.Complete(ctx, job.ID, "0.not-the-owner"); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("Complete with a foreign owner = %v, want ErrLeaseLost", err)
	}
	if _, err := e.q.Fail(ctx, job.ID, "", queue.FailParams{}); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("Fail with an empty owner = %v, want ErrLeaseLost", err)
	}
	if err := e.q.Release(ctx, "missing", job.LeaseOwner); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("Release of an unknown job = %v, want ErrLeaseLost", err)
	}
	if got := e.row(t, job.ID); got.State != "running" || got.Attempts != 0 {
		t.Fatalf("job after refused calls = %+v, want untouched", got)
	}

	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{}); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("Fail after Complete = %v, want ErrLeaseLost", err)
	}
	if got := e.row(t, job.ID); got.State != "succeeded" || got.Attempts != 1 {
		t.Fatalf("job = %+v, want succeeded after 1 attempt", got)
	}
}

func TestFailBacksOffWithJitterThenFails(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)

	// rand 0.5 is the middle of the jitter range, so the delays are the plain curve:
	// 30s, then 60s.
	for attempt, wantDelay := range []time.Duration{30 * time.Second, 60 * time.Second} {
		job := e.claim(t)
		if job.Attempt != attempt+1 {
			t.Fatalf("claim %d: attempt = %d", attempt+1, job.Attempt)
		}
		outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{})
		if err != nil || outcome != queue.FailRequeued {
			t.Fatalf("Fail %d = %s, %v; want requeued", attempt+1, outcome, err)
		}
		row := e.row(t, job.ID)
		want := e.clock.Now().Add(wantDelay).Format("2006-01-02T15:04:05.000Z")
		if row.State != "queued" || row.Attempts != attempt+1 || row.RunAfter != want {
			t.Fatalf("after failure %d: %+v, want queued, attempts %d, run_after %s", attempt+1, row, attempt+1, want)
		}
		if row.LeaseOwner.Valid || row.LeaseExpiresAt.Valid {
			t.Fatalf("lease kept on a requeued job: %+v", row)
		}
		e.clock.Advance(wantDelay)
	}

	job := e.claim(t)
	outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{})
	if err != nil || outcome != queue.FailExhausted {
		t.Fatalf("last Fail = %s, %v; want failed", outcome, err)
	}
	if got := e.row(t, job.ID); got.State != "failed" || got.Attempts != 3 {
		t.Fatalf("job = %+v, want failed after 3 attempts", got)
	}
	e.clock.Advance(24 * time.Hour)
	e.wantNoClaim(t)
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{}); err != nil {
		t.Errorf("repeated Fail of a failed job = %v, want a no-op", err)
	}
}

func TestFailHonoursRetryAfter(t *testing.T) {
	e := newJobEnv(t, 1)

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	if _, err := e.q.Fail(context.Background(), job.ID, job.LeaseOwner, queue.FailParams{RetryAfter: 10 * time.Minute}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	e.clock.Advance(9 * time.Minute)
	e.wantNoClaim(t)
	e.clock.Advance(2 * time.Minute)
	e.claim(t)
}

func TestReleaseKeepsTheAttemptAndRequeues(t *testing.T) {
	e := newJobEnv(t, 1)

	e.enqueue(t, e.repos[0], queue.PriorityNormal)
	job := e.claim(t)
	e.enqueue(t, e.repos[0], queue.PriorityHigh) // rerun_requested while it runs
	if err := e.q.Release(context.Background(), job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Release: %v", err)
	}

	row := e.row(t, job.ID)
	if row.State != "queued" || row.Attempts != 0 || row.LeaseOwner.Valid || row.RerunRequested != 0 || row.Priority != "high" {
		t.Fatalf("released job = %+v, want queued, no attempt used, no lease, the rerun folded into a high-priority retry", row)
	}
	if again := e.claim(t); again.ID != job.ID || again.Attempt != 1 {
		t.Fatalf("claim after release = %+v, want the same job at attempt 1", again)
	}
}

// A worker killed mid-job stops heartbeating. Its job stays running until the lease expires;
// the reaper then requeues it, counting the attempt, and a second worker runs it once.
func TestReaperRequeuesAnExpiredLeaseForASecondWorker(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	dead := e.claim(t)
	if _, err := e.q.Heartbeat(ctx, dead.ID, dead.LeaseOwner); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	e.clock.Advance(time.Minute)
	if res, err := e.q.Reap(ctx); err != nil || res != (queue.ReapResult{}) {
		t.Fatalf("Reap before the lease expired = %+v, %v; want nothing", res, err)
	}
	e.wantNoClaim(t)
	if got := e.row(t, dead.ID); got.State != "running" {
		t.Fatalf("job = %s before its lease expired, want running", got.State)
	}

	e.clock.Advance(2 * time.Minute)
	res, err := e.q.Reap(ctx)
	if err != nil || res != (queue.ReapResult{Requeued: 1}) {
		t.Fatalf("Reap after the lease expired = %+v, %v; want one requeued", res, err)
	}
	row := e.row(t, dead.ID)
	if row.State != "queued" || row.Attempts != 1 || row.LeaseOwner.Valid {
		t.Fatalf("reaped job = %+v, want queued with the attempt counted and no lease", row)
	}

	e.clock.Advance(time.Hour)
	second := e.claim(t)
	if second.ID != dead.ID || second.Attempt != 2 || second.LeaseOwner == dead.LeaseOwner {
		t.Fatalf("second claim = %+v, want the same job, attempt 2, a new lease", second)
	}
	if err := e.q.Complete(ctx, second.ID, second.LeaseOwner); err != nil {
		t.Fatalf("second worker Complete: %v", err)
	}

	// The dead worker comes back: it holds nothing.
	if err := e.q.Complete(ctx, dead.ID, dead.LeaseOwner); !errors.Is(err, queue.ErrLeaseLost) {
		t.Errorf("late Complete by the first worker = %v, want ErrLeaseLost", err)
	}
	if res, err := e.q.Heartbeat(ctx, dead.ID, dead.LeaseOwner); err != nil || !res.Abort {
		t.Errorf("late Heartbeat by the first worker = %+v, %v; want an abort", res, err)
	}
	if got := e.row(t, dead.ID); got.State != "succeeded" || got.Attempts != 2 {
		t.Fatalf("job = %+v, want succeeded after 2 attempts", got)
	}
}

func TestReaperFailsAJobWithNoAttemptsLeft(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	var id string
	for i := range 3 {
		job := e.claim(t)
		id = job.ID
		e.clock.Advance(3 * time.Minute)
		res, err := e.q.Reap(ctx)
		want := queue.ReapResult{Requeued: 1}
		if i == 2 {
			want = queue.ReapResult{Failed: 1}
		}
		if err != nil || res != want {
			t.Fatalf("Reap %d = %+v, %v; want %+v", i+1, res, err, want)
		}
		e.clock.Advance(time.Hour)
	}
	if got := e.row(t, id); got.State != "failed" || got.Attempts != 3 {
		t.Fatalf("job = %+v, want failed after 3 attempts", got)
	}
}

func TestReaperQueuesTheFollowUpOfAFailedJob(t *testing.T) {
	e := newJobEnv(t, 1)
	cfg := queue.DefaultConfig()
	cfg.MaxAttempts = 1
	q := store.NewJobQueue(e.db, store.JobQueueOptions{Config: cfg, Now: e.clock.Now})

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	if _, err := q.Claim(context.Background()); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	e.clock.Advance(time.Hour)
	if res, err := q.Reap(context.Background()); err != nil || res.Failed != 1 {
		t.Fatalf("Reap = %+v, %v; want the job failed", res, err)
	}
	if n := e.count(t, `state = 'queued' AND priority = 'high'`); n != 1 {
		t.Fatalf("%d queued follow-ups after the job failed, want 1", n)
	}
}

// Two workers racing for one queued job: exactly one claim wins, every round.
func TestConcurrentClaimsNeverShareAJob(t *testing.T) {
	e := newJobEnv(t, 1)

	for round := range 25 {
		e.enqueue(t, e.repos[0], queue.PriorityHigh)

		var wg sync.WaitGroup
		results := make(chan *queue.ClaimedJob, 8)
		for range 8 {
			wg.Go(func() {
				job, err := e.q.Claim(context.Background())
				switch {
				case err == nil:
					results <- job
				case !errors.Is(err, queue.ErrNoJob):
					t.Errorf("Claim: %v", err)
				}
			})
		}
		wg.Wait()
		close(results)

		var won []*queue.ClaimedJob
		for job := range results {
			won = append(won, job)
		}
		if len(won) != 1 {
			t.Fatalf("round %d: %d claims won the one job, want 1", round, len(won))
		}
		if err := e.q.Complete(context.Background(), won[0].ID, won[0].LeaseOwner); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
}

func TestConcurrentClaimsNeverExceedTheCap(t *testing.T) {
	e := newJobEnv(t, 6)
	for _, r := range e.repos {
		e.enqueue(t, r, queue.PriorityHigh)
	}

	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := e.q.Claim(context.Background()); err != nil && !errors.Is(err, queue.ErrNoJob) {
				t.Errorf("Claim: %v", err)
			}
		})
	}
	wg.Wait()
	if n := e.count(t, `state = 'running'`); n != 2 {
		t.Fatalf("%d running jobs, want exactly the cap of 2", n)
	}
}

func TestChangedWakesWaitersOnEnqueue(t *testing.T) {
	e := newJobEnv(t, 1)

	changed := e.q.Changed()
	select {
	case <-changed:
		t.Fatal("Changed fired before any change")
	default:
	}
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("Enqueue did not wake a waiter")
	}
}

func TestEnqueueRejectsAnInvalidRequest(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()

	for name, p := range map[string]queue.EnqueueParams{
		"no repository":      {Priority: queue.PriorityHigh, Trigger: queue.TriggerPush},
		"bad priority":       {RepositoryID: e.repos[0], Priority: "urgent", Trigger: queue.TriggerPush},
		"unknown repository": {RepositoryID: "missing", Priority: queue.PriorityHigh, Trigger: queue.TriggerPush},
		"unknown trigger":    {RepositoryID: e.repos[0], Priority: queue.PriorityHigh, Trigger: "whim"},
	} {
		if _, err := e.q.Enqueue(ctx, p); err == nil {
			t.Errorf("%s: Enqueue succeeded", name)
		}
	}
	if n := e.count(t, `1 = 1`); n != 0 {
		t.Fatalf("%d jobs after refused enqueues, want 0", n)
	}
}

func TestLastWorkerContactCoversEveryWorkerCall(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t, 1)

	if _, ok, err := e.q.LastWorkerContact(ctx); err != nil || ok {
		t.Fatalf("LastWorkerContact before any call = ok %v, %v; want none", ok, err)
	}

	// An idle worker only polls: a claim that finds nothing is a contact.
	e.wantNoClaim(t)
	at, ok, err := e.q.LastWorkerContact(ctx)
	if err != nil || !ok || !at.Equal(e.clock.Now()) {
		t.Fatalf("after an empty claim = %v, ok %v, %v; want %v", at, ok, err, e.clock.Now())
	}

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	for _, step := range []struct {
		name string
		call func() error
	}{
		{"heartbeat", func() error { _, err := e.q.Heartbeat(ctx, job.ID, job.LeaseOwner); return err }},
		{"held job", func() error { _, err := e.q.HeldJob(ctx, job.ID, job.LeaseOwner); return err }},
		{"release", func() error { return e.q.Release(ctx, job.ID, job.LeaseOwner) }},
	} {
		name, call := step.name, step.call
		e.clock.Advance(time.Second)
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if at, _, _ := e.q.LastWorkerContact(ctx); !at.Equal(e.clock.Now()) {
			t.Errorf("after %s the contact is %v, want %v", name, at, e.clock.Now())
		}
	}

	e.clock.Advance(time.Second)
	job = e.claim(t)
	e.clock.Advance(time.Second)
	if err := e.q.Complete(ctx, job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if at, _, _ := e.q.LastWorkerContact(ctx); !at.Equal(e.clock.Now()) {
		t.Errorf("after a result the contact is %v, want %v", at, e.clock.Now())
	}

	// The app's own reaper is not a worker.
	e.clock.Advance(time.Minute)
	if _, err := e.q.Reap(ctx); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if at, _, _ := e.q.LastWorkerContact(ctx); at.Equal(e.clock.Now()) {
		t.Error("a reap pass counted as a worker contact")
	}
}

func TestStatsCountsOnlyEligibleQueuedJobs(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t, 4)
	e.setCap(t, 1)

	empty, err := e.q.Stats(ctx)
	if err != nil || empty.OldestAge != 0 || empty.Depth[queue.PriorityHigh] != 0 || empty.Depth[queue.PriorityNormal] != 0 {
		t.Fatalf("Stats of an empty queue = %+v, %v; want zeros for both priorities", empty, err)
	}
	if _, ok := empty.Depth[queue.PriorityNormal]; !ok {
		t.Fatal("Stats of an empty queue omits a priority")
	}

	e.enqueue(t, e.repos[0], queue.PriorityNormal)
	e.clock.Advance(40 * time.Second)
	e.enqueue(t, e.repos[1], queue.PriorityNormal)
	e.enqueue(t, e.repos[2], queue.PriorityHigh)
	e.clock.Advance(20 * time.Second)

	stats, err := e.q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Depth[queue.PriorityNormal] != 2 || stats.Depth[queue.PriorityHigh] != 1 {
		t.Errorf("Depth = %v, want 2 normal and 1 high", stats.Depth)
	}
	if stats.OldestAge != time.Minute {
		t.Errorf("OldestAge = %v, want 1m (the first job, queued 60s ago)", stats.OldestAge)
	}

	// A job that failed and waits out its backoff is not eligible yet, and a running job is
	// not queued: neither counts.
	e.enqueue(t, e.repos[3], queue.PriorityHigh)
	job := e.claim(t)
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{}); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	after, err := e.q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if got := after.Depth[queue.PriorityNormal] + after.Depth[queue.PriorityHigh]; got != 3 {
		t.Errorf("Depth = %v after a failed job entered its backoff, want the 3 eligible jobs only", after.Depth)
	}
}

func TestReapReportsEachExpiredAttemptByRepository(t *testing.T) {
	ctx := context.Background()
	e := newJobEnv(t, 2)
	e.setCap(t, 2)

	var expired []string
	q := store.NewJobQueue(e.db, store.JobQueueOptions{
		Now:            e.clock.Now,
		Rand:           func() float64 { return 0.5 },
		OnLeaseExpired: func(repository string) { expired = append(expired, repository) },
	})
	for _, repo := range e.repos {
		if _, err := q.Enqueue(ctx, queue.EnqueueParams{RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	for range e.repos {
		if _, err := q.Claim(ctx); err != nil {
			t.Fatalf("Claim: %v", err)
		}
	}

	if _, err := q.Reap(ctx); err != nil || len(expired) != 0 {
		t.Fatalf("Reap before expiry = %v, expired %v; want none", err, expired)
	}
	e.clock.Advance(queue.DefaultConfig().LeaseDuration + time.Second)
	if _, err := q.Reap(ctx); err != nil {
		t.Fatalf("Reap: %v", err)
	}
	slices.Sort(expired)
	if !slices.Equal(expired, []string{"acme/repo-1", "acme/repo-2"}) {
		t.Errorf("expired attempts reported for %v, want both repositories", expired)
	}
}

// pause marks a repository paused (enabled = 0); remove marks it removed from its installation.
func (e *jobEnv) pause(t *testing.T, repo string) {
	t.Helper()

	if _, err := e.db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE id = ?`, repo); err != nil {
		t.Fatalf("pause %s: %v", repo, err)
	}
}

func (e *jobEnv) remove(t *testing.T, repo string) {
	t.Helper()

	if _, err := e.db.Writer.Exec(
		`UPDATE repositories SET removed_at = '2026-11-14T11:00:00.000Z' WHERE id = ?`, repo,
	); err != nil {
		t.Fatalf("remove %s: %v", repo, err)
	}
}

func TestClaimSkipsAQueuedJobOfAPausedOrRemovedRepository(t *testing.T) {
	e := newJobEnv(t, 3)

	// The paused and the removed repository's jobs are older and high priority, so they
	// would be claimed first if the claim did not look at the repository.
	paused := e.enqueue(t, e.repos[0], queue.PriorityHigh)
	removed := e.enqueue(t, e.repos[1], queue.PriorityHigh)
	e.clock.Advance(time.Second)
	eligible := e.enqueue(t, e.repos[2], queue.PriorityNormal)
	e.pause(t, e.repos[0])
	e.remove(t, e.repos[1])

	if got := e.claim(t).ID; got != eligible.JobID {
		t.Fatalf("claim = %s, want the eligible repository's job %s", got, eligible.JobID)
	}
	e.wantNoClaim(t)

	// The jobs stay queued: nothing deletes or fails them.
	for name, id := range map[string]string{"paused": paused.JobID, "removed": removed.JobID} {
		if row := e.row(t, id); row.State != "queued" || row.Attempts != 0 {
			t.Errorf("job of the %s repository = %+v, want it left queued", name, row)
		}
	}

	// Resuming the repository makes its job claimable again.
	if _, err := e.db.Writer.Exec(`UPDATE repositories SET enabled = 1 WHERE id = ?`, e.repos[0]); err != nil {
		t.Fatal(err)
	}
	if got := e.claim(t).ID; got != paused.JobID {
		t.Fatalf("claim after resuming = %s, want %s", got, paused.JobID)
	}
}

func TestHeldJobRefusesAJobOfAPausedOrRemovedRepository(t *testing.T) {
	for name, change := range map[string]func(*jobEnv, *testing.T, string){
		"paused":  (*jobEnv).pause,
		"removed": (*jobEnv).remove,
	} {
		t.Run(name, func(t *testing.T) {
			e := newJobEnv(t, 1)
			e.enqueue(t, e.repos[0], queue.PriorityHigh)
			job := e.claim(t)
			if _, err := e.q.HeldJob(context.Background(), job.ID, job.LeaseOwner); err != nil {
				t.Fatalf("HeldJob before the change: %v", err)
			}

			change(e, t, e.repos[0])
			if h, err := e.q.HeldJob(context.Background(), job.ID, job.LeaseOwner); !errors.Is(err, queue.ErrLeaseLost) || h != nil {
				t.Errorf("HeldJob = %+v, %v, want ErrLeaseLost and no job", h, err)
			}
		})
	}
}
