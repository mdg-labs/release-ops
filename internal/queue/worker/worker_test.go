package worker_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const secret = "0123456789abcdef0123456789abcdef-test-secret"

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
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

type env struct {
	db     *store.DB
	q      *store.JobQueue
	clock  *fakeClock
	client *worker.Client
	repos  []string
}

func newEnv(t *testing.T) *env {
	t.Helper()

	db := storetest.OpenMigrated(t)
	clock := &fakeClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	q := store.NewJobQueue(db, store.JobQueueOptions{Now: clock.Now, Rand: func() float64 { return 0.5 }})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: secret, ClaimWait: 100 * time.Millisecond}))
	t.Cleanup(srv.Close)
	return &env{
		db: db, q: q, clock: clock,
		client: worker.NewClient(srv.URL, secret, nil),
		repos:  storetest.SeedRepositories(t, db, 3),
	}
}

func (e *env) enqueue(t *testing.T, repo string) string {
	t.Helper()
	res, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{
		RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return res.JobID
}

func (e *env) state(t *testing.T, id string) (state string, attempts int, owner sql.NullString) {
	t.Helper()
	if err := e.db.Writer.QueryRow(`SELECT state, attempts, lease_owner FROM jobs WHERE id = ?`, id).Scan(&state, &attempts, &owner); err != nil {
		t.Fatalf("read job: %v", err)
	}
	return state, attempts, owner
}

func (e *env) waitState(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got, _, _ := e.state(t, id); got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, attempts, _ := e.state(t, id)
	t.Fatalf("job %s is %s (attempts %d), want %s", id, got, attempts, want)
}

// waitAttempts waits until the job has used n attempts and is queued again.
func (e *env) waitAttempts(t *testing.T, id string, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if state, attempts, _ := e.state(t, id); state == "queued" && attempts >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, attempts, _ := e.state(t, id)
	t.Fatalf("job %s is %s with %d attempts, want queued with %d", id, state, attempts, n)
}

func opts() worker.Options {
	return worker.Options{
		Slots:             2,
		HeartbeatInterval: 10 * time.Millisecond,
		ShutdownGrace:     100 * time.Millisecond,
		ReleaseTimeout:    2 * time.Second,
		RetryDelay:        10 * time.Millisecond,
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// start runs a worker until the returned stop function is called, which waits for it to end.
func start(t *testing.T, e *env, reg worker.Registry, o worker.Options) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.NewRunner(e.client, reg, o).Run(ctx)
	}()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("worker did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestWorkerRunsTheHandlerOfAClaimedJobAndReportsTheResult(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	var got queue.ClaimedJob
	var mu sync.Mutex
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(_ context.Context, job queue.ClaimedJob) worker.Result {
			mu.Lock()
			got = job
			mu.Unlock()
			return worker.Result{}
		}),
	}, opts())

	e.waitState(t, id, "succeeded")
	mu.Lock()
	defer mu.Unlock()
	if got.ID != id || got.RepositoryFullName != "acme/repo-1" || got.Kind != queue.KindRenovateRun || got.Attempt != 1 {
		t.Fatalf("handler got %+v", got)
	}
}

func TestWorkerReportsAFailedAttemptAndHonoursRetryAfter(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	var runs atomic.Int32
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(context.Context, queue.ClaimedJob) worker.Result {
			runs.Add(1)
			return worker.Result{Err: errors.New("renovate exited 1"), RetryAfter: 10 * time.Minute}
		}),
	}, opts())

	e.waitAttempts(t, id, 1)
	if _, attempts, owner := e.state(t, id); attempts != 1 || owner.Valid {
		t.Fatalf("job after a failed attempt: attempts %d, lease %v; want 1 and no lease", attempts, owner)
	}
	time.Sleep(300 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("handler ran %d times inside the Retry-After window, want 1", n)
	}
	e.clock.Advance(11 * time.Minute)
	e.waitAttempts(t, id, 2)
	if n := runs.Load(); n != 2 {
		t.Fatalf("handler ran %d times after the Retry-After window, want 2", n)
	}
}

func scrapeMetrics(t *testing.T, reg *metrics.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// The job lifecycle records every attempt by outcome and times it: a succeeded one, a failed
// one, and one the worker lost the lease of (aborted).
func TestWorkerRecordsJobRunsFailuresAndDuration(t *testing.T) {
	e := newEnv(t)
	reg := metrics.NewRegistry()
	jobs, err := metrics.NewJobs(reg)
	if err != nil {
		t.Fatal(err)
	}
	o := opts()
	o.Jobs = jobs

	ok := e.enqueue(t, e.repos[0])
	failing := e.enqueue(t, e.repos[1])
	e.enqueue(t, e.repos[2])

	running := make(chan struct{})
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(ctx context.Context, job queue.ClaimedJob) worker.Result {
			switch job.RepositoryFullName {
			case "acme/repo-2":
				return worker.Result{Err: errors.New("renovate exited 1"), RetryAfter: time.Hour}
			case "acme/repo-3":
				close(running)
				<-ctx.Done()
				return worker.Result{Err: ctx.Err()}
			}
			return worker.Result{}
		}),
	}, o)

	e.waitState(t, ok, "succeeded")
	e.waitAttempts(t, failing, 1)
	<-running
	time.Sleep(50 * time.Millisecond)
	// Past the lease of the running job, inside the failed job's Retry-After.
	e.clock.Advance(3 * time.Minute)
	if res, err := e.q.Reap(context.Background()); err != nil || res.Requeued != 1 {
		t.Fatalf("Reap = %+v, %v", res, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var body string
	for {
		body = scrapeMetrics(t, reg)
		if strings.Contains(body, `outcome="aborted",repository="acme/repo-3"} 1`) &&
			strings.Contains(body, `outcome="succeeded",repository="acme/repo-1"} 1`) &&
			strings.Contains(body, `outcome="failed",repository="acme/repo-2"} 1`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics lack the three outcomes:\n%s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, want := range []string{
		`release_ops_job_failures_total{repository="acme/repo-2"} 1`,
		`release_ops_job_duration_seconds_count{repository="acme/repo-1"} 1`,
		`release_ops_job_duration_seconds_count{repository="acme/repo-2"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if strings.Contains(body, `release_ops_job_failures_total{repository="acme/repo-1"}`) ||
		strings.Contains(body, `release_ops_job_failures_total{repository="acme/repo-3"}`) {
		t.Error("a succeeded or aborted attempt counted as a failure")
	}
}

func TestWorkerFailsAJobOfAnUnregisteredKindAndSurvivesAPanic(t *testing.T) {
	e := newEnv(t)
	noHandler := e.enqueue(t, e.repos[0])
	start(t, e, worker.Registry{}, opts())
	e.waitAttempts(t, noHandler, 1)

	e2 := newEnv(t)
	panicking := e2.enqueue(t, e2.repos[0])
	start(t, e2, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(context.Context, queue.ClaimedJob) worker.Result { panic("boom") }),
	}, opts())
	e2.waitAttempts(t, panicking, 1)
}

func TestWorkerHeartbeatsKeepTheLeaseAlive(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	release := make(chan struct{})
	running := make(chan struct{})
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(ctx context.Context, _ queue.ClaimedJob) worker.Result {
			close(running)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return worker.Result{}
		}),
	}, opts())

	<-running
	var started sql.NullString
	deadline := time.Now().Add(5 * time.Second)
	for !started.Valid && time.Now().Before(deadline) {
		_ = e.db.Writer.QueryRow(`SELECT started_at FROM jobs WHERE id = ?`, id).Scan(&started)
		time.Sleep(10 * time.Millisecond)
	}
	if !started.Valid {
		t.Fatal("the worker's first heartbeat did not mark the job started")
	}

	// Past the original lease: only the heartbeats renewed it, so the reaper finds nothing.
	e.clock.Advance(90 * time.Second)
	time.Sleep(100 * time.Millisecond)
	e.clock.Advance(90 * time.Second)
	time.Sleep(100 * time.Millisecond)
	if res, err := e.q.Reap(context.Background()); err != nil || res != (queue.ReapResult{}) {
		t.Fatalf("Reap = %+v, %v; want a heartbeating job left alone", res, err)
	}
	close(release)
	e.waitState(t, id, "succeeded")
}

func TestWorkerAbortsAJobWhoseLeaseWasReaped(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	running := make(chan struct{})
	cancelled := make(chan struct{})
	start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(ctx context.Context, _ queue.ClaimedJob) worker.Result {
			close(running)
			<-ctx.Done()
			close(cancelled)
			return worker.Result{Err: ctx.Err()}
		}),
	}, opts())

	<-running
	time.Sleep(50 * time.Millisecond)
	// Another path takes the lease away: the reaper after the lease expired.
	e.clock.Advance(time.Hour)
	if res, err := e.q.Reap(context.Background()); err != nil || res.Requeued != 1 {
		t.Fatalf("Reap = %+v, %v", res, err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler was not cancelled after the lease was lost")
	}
	// The cancelled worker's failure must not be recorded against the requeued job.
	time.Sleep(100 * time.Millisecond)
	if _, attempts, _ := e.state(t, id); attempts != 1 {
		t.Fatalf("attempts = %d, want only the reaped attempt", attempts)
	}
}

func TestShutdownLetsAHandlerFinishWithinTheGrace(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	running := make(chan struct{})
	o := opts()
	o.ShutdownGrace = 5 * time.Second
	stop := start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(ctx context.Context, _ queue.ClaimedJob) worker.Result {
			close(running)
			time.Sleep(300 * time.Millisecond)
			return worker.Result{}
		}),
	}, o)

	<-running
	stop()
	if state, _, _ := e.state(t, id); state != "succeeded" {
		t.Fatalf("job = %s after a shutdown inside the grace, want succeeded", state)
	}
}

func TestShutdownReleasesTheLeaseOfAHandlerThatOutlastsTheGrace(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])
	second := e.enqueue(t, e.repos[1])

	var running sync.WaitGroup
	running.Add(2)
	stop := start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(ctx context.Context, _ queue.ClaimedJob) worker.Result {
			running.Done()
			<-ctx.Done()
			return worker.Result{Err: ctx.Err()}
		}),
	}, opts())

	running.Wait()
	stop()

	for _, jobID := range []string{id, second} {
		state, attempts, owner := e.state(t, jobID)
		if state != "queued" || attempts != 0 || owner.Valid {
			t.Fatalf("job %s = %s, attempts %d, lease %v after shutdown; want queued with no attempt used and no lease", jobID, state, attempts, owner)
		}
	}
	if _, err := e.q.Claim(context.Background()); err != nil {
		t.Fatalf("a released job is claimable at once: %v", err)
	}
}

func TestShutdownReleasesAHandlerThatIgnoresItsContext(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(t, e.repos[0])

	running := make(chan struct{})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	o := opts()
	o.ReleaseTimeout = 200 * time.Millisecond
	stop := start(t, e, worker.Registry{
		queue.KindRenovateRun: worker.HandlerFunc(func(context.Context, queue.ClaimedJob) worker.Result {
			close(running)
			<-block
			return worker.Result{}
		}),
	}, o)

	<-running
	stop()
	if state, attempts, owner := e.state(t, id); state != "queued" || attempts != 0 || owner.Valid {
		t.Fatalf("job = %s, attempts %d, lease %v; want the lease released by the worker itself", state, attempts, owner)
	}
}

func TestClientRefusesAWrongSecretAndDoesNotFollowRedirects(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(internalapi.NewHandler(e.q, internalapi.Options{Secret: secret, ClaimWait: 50 * time.Millisecond}))
	t.Cleanup(srv.Close)

	bad := worker.NewClient(srv.URL, "another-secret-another-secret-another", nil)
	if _, err := bad.Claim(context.Background()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Claim with a wrong secret = %v, want a 401 error", err)
	}
	if strings.Contains(errString(bad.Release(context.Background(), "x", "y")), "another-secret") {
		t.Fatal("an error message carries the secret")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// The worker must stay free of the database: its import graph holds neither the store nor
// the SQLite driver.
func TestWorkerPackagesDoNotDependOnTheDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, "go", "list", "-deps",
		"github.com/mdg-labs/release-ops/internal/queue/worker",
		"github.com/mdg-labs/release-ops/internal/config").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.Contains(dep, "release-ops/internal/store") || strings.Contains(dep, "sqlite") || strings.Contains(dep, "release-ops/internal/crypto") {
			t.Errorf("the worker depends on %s", dep)
		}
	}
}
