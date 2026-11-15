package scheduler_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/scheduler"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type fakeRepositories struct {
	ids []string
	err error
}

func (f fakeRepositories) ListScheduled(context.Context) ([]string, error) { return f.ids, f.err }

type fakeQueue struct {
	mu       sync.Mutex
	got      []queue.EnqueueParams
	failFor  map[string]error
	outcomes map[string]queue.EnqueueOutcome
}

func (f *fakeQueue) Enqueue(_ context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, p)
	if err := f.failFor[p.RepositoryID]; err != nil {
		return queue.EnqueueResult{}, err
	}
	out := queue.EnqueueQueued
	if o, ok := f.outcomes[p.RepositoryID]; ok {
		out = o
	}
	return queue.EnqueueResult{Outcome: out, JobID: "job-" + p.RepositoryID}, nil
}

func (f *fakeQueue) enqueued() []queue.EnqueueParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.got)
}

func TestTickEnqueuesOneNormalScheduleJobPerListedRepository(t *testing.T) {
	q := &fakeQueue{}
	s := scheduler.New(fakeRepositories{ids: []string{"a", "b", "c"}}, q)

	res, err := s.Tick(t.Context())
	if err != nil || res != (scheduler.Result{Enqueued: 3}) {
		t.Fatalf("Tick = %+v, %v; want 3 enqueued", res, err)
	}
	got := q.enqueued()
	if len(got) != 3 {
		t.Fatalf("enqueued %d jobs, want 3", len(got))
	}
	for i, p := range got {
		want := queue.EnqueueParams{RepositoryID: []string{"a", "b", "c"}[i], Kind: queue.KindRenovateRun, Priority: queue.PriorityNormal, Trigger: queue.TriggerSchedule}
		if p.RepositoryID != want.RepositoryID || p.Kind != want.Kind || p.Priority != want.Priority || p.Trigger != want.Trigger {
			t.Errorf("enqueue %d = %+v, want %+v", i, p, want)
		}
	}
}

func TestTickCountsAFoldedTriggerAsSkippedAndKeepsGoingAfterAFailure(t *testing.T) {
	boom := errors.New("boom")
	q := &fakeQueue{
		failFor:  map[string]error{"b": boom, "c": queue.ErrRepositoryPaused},
		outcomes: map[string]queue.EnqueueOutcome{"d": queue.EnqueueCoalesced},
	}
	s := scheduler.New(fakeRepositories{ids: []string{"a", "b", "c", "d", "e"}}, q)

	res, err := s.Tick(t.Context())
	if !errors.Is(err, boom) {
		t.Fatalf("Tick error = %v, want it to carry the failure of b", err)
	}
	if res != (scheduler.Result{Enqueued: 2, Skipped: 2}) {
		t.Fatalf("Tick = %+v, want 2 enqueued (a, e) and 2 skipped (c paused meanwhile, d folded)", res)
	}
	if n := len(q.enqueued()); n != 5 {
		t.Errorf("enqueue was called %d times, want every repository tried", n)
	}
}

func TestTickReportsAFailedListAndEnqueuesNothing(t *testing.T) {
	boom := errors.New("db down")
	q := &fakeQueue{}
	_, err := scheduler.New(fakeRepositories{err: boom}, q).Tick(t.Context())
	if !errors.Is(err, boom) || len(q.enqueued()) != 0 {
		t.Fatalf("Tick = %v with %d enqueues, want the list error and none", err, len(q.enqueued()))
	}
}

type tickResult struct {
	res scheduler.Result
	err error
}

// Run is driven by a clock of the test's own: nothing happens until a tick arrives, each tick
// is one pass, and a failed pass does not end the loop.
func TestRunTicksOnTheInjectedClockOnly(t *testing.T) {
	q := &fakeQueue{}
	repos := &flakyRepositories{ids: []string{"a"}}
	s := scheduler.New(repos, q)
	ticks := make(chan time.Time)
	reports := make(chan tickResult, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx, ticks, func(res scheduler.Result, err error) { reports <- tickResult{res, err} })
	}()

	if n := len(q.enqueued()); n != 0 {
		t.Fatalf("enqueued %d jobs before the first tick", n)
	}
	repos.failNext()
	ticks <- time.Time{}
	if r := <-reports; r.err == nil {
		t.Fatal("first tick reported no error, want the failed list")
	}
	ticks <- time.Time{}
	if r := <-reports; r.err != nil || r.res.Enqueued != 1 {
		t.Fatalf("second tick = %+v, want 1 enqueued after the failed one", r)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
}

type flakyRepositories struct {
	mu   sync.Mutex
	ids  []string
	fail bool
}

func (f *flakyRepositories) failNext() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = true
}

func (f *flakyRepositories) ListScheduled(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		f.fail = false
		return nil, errors.New("list failed")
	}
	return f.ids, nil
}

func TestIntervalIsHourly(t *testing.T) {
	if scheduler.Interval != time.Hour {
		t.Fatalf("Interval = %s, want 1h: the safety net has no setting (specs.html section 5.7)", scheduler.Interval)
	}
}

// The store and the queue behind the scheduler: eligible repositories get exactly one queued
// normal job, however many ticks pass, and a job that is running or queued is not doubled.
func TestTicksOverTheStoreEnqueueOneJobPerEligibleRepositoryWithoutDuplicates(t *testing.T) {
	db := storetest.OpenMigrated(t)
	ids := storetest.SeedRepositories(t, db, 6)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Writer.Exec(query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	// ids[0] present, ids[1] unknown: both take part. The rest do not.
	exec(`UPDATE repositories SET config_state = 'present' WHERE id = ?`, ids[0])
	exec(`UPDATE repositories SET enabled = 0 WHERE id = ?`, ids[2])
	exec(`UPDATE repositories SET config_state = 'missing' WHERE id = ?`, ids[3])
	exec(`UPDATE repositories SET removed_at = '2026-11-14T11:00:00.000Z' WHERE id = ?`, ids[4])

	jobs := store.NewJobQueue(db, store.JobQueueOptions{})
	s := scheduler.New(store.New(db, nil).Repositories(), jobs)

	queued := func() []string {
		t.Helper()
		rows, err := db.Writer.Query(`SELECT repository_id || '/' || state || '/' || priority || '/' || "trigger" FROM jobs ORDER BY repository_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var s string
			_ = rows.Scan(&s)
			out = append(out, s)
		}
		return out
	}

	for range 3 {
		if _, err := s.Tick(t.Context()); err != nil {
			t.Fatalf("Tick: %v", err)
		}
	}
	want := []string{"repo-1/queued/normal/schedule", "repo-2/queued/normal/schedule", "repo-6/queued/normal/schedule"}
	if got := queued(); !slices.Equal(got, want) {
		t.Fatalf("jobs after 3 ticks = %v, want %v", got, want)
	}

	// One repository starts running: further ticks leave it alone, and add nothing for the others.
	job, err := jobs.Claim(t.Context())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	for range 2 {
		if res, err := s.Tick(t.Context()); err != nil || res.Enqueued != 0 {
			t.Fatalf("Tick with a running job = %+v, %v; want nothing enqueued", res, err)
		}
	}
	if n := len(queued()); n != 3 {
		t.Fatalf("jobs = %d (%v), want 3", n, queued())
	}

	// When it finishes the repository is eligible again.
	if err := jobs.Complete(t.Context(), job.ID, job.LeaseOwner); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if res, err := s.Tick(t.Context()); err != nil || res.Enqueued != 1 {
		t.Fatalf("Tick after the job ended = %+v, %v; want the finished repository enqueued again", res, err)
	}
}

// AfterTick runs once after every tick, also after a failed one, so the PR reconcile follows the
// safety net hourly.
func TestRunCallsAfterTickAfterEveryTickIncludingAFailedOne(t *testing.T) {
	q := &fakeQueue{}
	repos := &flakyRepositories{ids: []string{"a"}}
	s := scheduler.New(repos, q)
	after := make(chan int, 4)
	s.AfterTick = func(context.Context) { after <- len(q.enqueued()) }
	ticks := make(chan time.Time)
	reports := make(chan tickResult, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Run(ctx, ticks, func(res scheduler.Result, err error) { reports <- tickResult{res, err} })
	}()
	t.Cleanup(func() { cancel(); <-done })

	repos.failNext()
	ticks <- time.Time{}
	<-reports
	if n := <-after; n != 0 {
		t.Fatalf("after a failed tick, %d jobs enqueued, want 0", n)
	}
	ticks <- time.Time{}
	<-reports
	if n := <-after; n != 1 {
		t.Fatalf("after the second tick, %d jobs enqueued, want 1 (the hook runs after the tick)", n)
	}
}
