package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// A webhook-triggered job that the worker reports as rate limited is rescheduled by the app, not
// failed: it waits for the reset however often the worker's claim loop asks, then runs again and
// completes. The whole path is the real one: the signed webhook route, the queue, the internal
// listener, the worker's runner and the shared budget behind the claim.
func TestARateLimitedWebhookJobIsDeferredByTheAppFromTheWorkersResultAndLaterCompletes(t *testing.T) {
	t.Parallel()

	clock := &stepClock{now: time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)}
	reg := metrics.NewRegistry()
	var budget *ratelimit.Budget
	f := newRenovateFlowWith(t, 50*time.Millisecond, func(deps *ServerDeps, q *store.JobQueueOptions, a *internalapi.Options) {
		budget = ratelimit.NewBudget(ratelimit.Options{Now: clock.Now, Store: deps.Store.RateLimits()})
		if _, err := metrics.RegisterRateLimits(reg, budget); err != nil {
			t.Fatal(err)
		}
		q.Now, q.Rand = clock.Now, func() float64 { return 0 }
		a.RateLimits, a.Now = budget, clock.Now
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go budget.Run(ctx)

	var runs atomic.Int32
	started := make(chan struct{}, 4)
	reset := clock.Now().Add(20 * time.Minute)
	zero := int64(0)
	handler := worker.HandlerFunc(func(_ context.Context, _ queue.ClaimedJob) worker.Result {
		defer func() { started <- struct{}{} }()
		if runs.Add(1) == 1 {
			return worker.Result{
				Err: errors.New("rate limited"),
				RateLimit: &queue.RateLimitReport{
					Limited: true, Remaining: &zero, ResetUnix: reset.Unix(),
				},
				Run: &queue.RunRecord{ErrorCode: queue.ErrorCodeRateLimited, ErrorMessage: "GitHub's rate limit stopped the run"},
			}
		}
		return worker.Result{}
	})
	runner := worker.NewRunner(f.worker, worker.Registry{queue.KindRenovateRun: handler}, worker.Options{
		Slots: 1, HeartbeatInterval: time.Hour, RetryDelay: 10 * time.Millisecond, ShutdownGrace: time.Second,
	})
	done := make(chan struct{})
	go func() { runner.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	if status, _ := f.post(t, signedHook("issues", "g-rl", fixture(t, "issues_edited_dashboard_tick.json"))); status != http.StatusAccepted {
		t.Fatalf("webhook = %d, want 202", status)
	}
	waitFor(t, "the first run", func() bool { return len(started) == 1 })
	<-started

	// The app took the failed attempt and put the job back, no earlier than the reset.
	var state, runAfter string
	var attempts int
	waitFor(t, "the job to be requeued", func() bool {
		if err := f.deps.DB.QueryRow(`SELECT state, attempts, run_after FROM jobs`).Scan(&state, &attempts, &runAfter); err != nil {
			t.Fatal(err)
		}
		return state == "queued" && attempts == 1
	})
	if want := reset.UTC().Format("2006-01-02T15:04:05.000Z"); runAfter < want {
		t.Fatalf("run_after = %s, want it no earlier than the reset %s", runAfter, want)
	}
	var kind, message string
	if err := f.deps.DB.QueryRow(`SELECT kind, message FROM job_events`).Scan(&kind, &message); err != nil || kind != "rate_limited" || !strings.Contains(message, "attempt 1 of 8") {
		t.Fatalf("job_events = %q %q, %v, want the attempt recorded as rate_limited", kind, message, err)
	}
	// The budget reached the installation row, which the claim reads.
	waitFor(t, "the budget to be stored", func() bool {
		var remaining *int64
		_ = f.deps.DB.QueryRow(`SELECT rate_limit_remaining FROM installations`).Scan(&remaining)
		return remaining != nil && *remaining == 0
	})

	// Before the reset the worker's claim loop gets nothing, however often it asks.
	clock.Advance(19 * time.Minute)
	time.Sleep(300 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("the handler ran %d times before the reset, want 1", n)
	}

	clock.Advance(10 * time.Minute)
	waitFor(t, "the job to run again", func() bool { return len(started) == 1 })
	waitFor(t, "the job to complete", func() bool {
		_ = f.deps.DB.QueryRow(`SELECT state FROM jobs`).Scan(&state)
		return state == "succeeded"
	})
	if n := runs.Load(); n != 2 {
		t.Fatalf("the handler ran %d times, want 2", n)
	}

	scrape := httptest.NewRecorder()
	reg.Handler().ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	var githubID string
	if err := f.deps.DB.QueryRow(`SELECT CAST(github_installation_id AS TEXT) FROM installations`).Scan(&githubID); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`release_ops_github_rate_limit_hits_total{installation="` + githubID + `"} 1`,
		`release_ops_github_rate_limit_deferred_total{consumer="renovate_run",installation="` + githubID + `"} 1`,
		`release_ops_github_rate_limit_remaining{installation="` + githubID + `"} 0`,
	} {
		if !strings.Contains(scrape.Body.String(), want) {
			t.Errorf("/metrics lacks %q", want)
		}
	}

	var runsStored int
	if err := f.deps.DB.QueryRow(`SELECT COUNT(*) FROM job_runs WHERE outcome = 'failed'`).Scan(&runsStored); err != nil || runsStored != 1 {
		t.Errorf("failed attempts stored = %d, %v, want the one rate-limited attempt", runsStored, err)
	}
}
