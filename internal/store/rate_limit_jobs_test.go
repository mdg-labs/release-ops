package store_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

func (e *jobEnv) rateLimits() store.RateLimits {
	return store.New(e.db, nil).RateLimits()
}

func (e *jobEnv) events(t *testing.T, jobID string) []string {
	t.Helper()

	rows, err := e.db.Writer.Query(`SELECT kind || ': ' || COALESCE(message, '') FROM job_events WHERE job_id = ? ORDER BY at, id`, jobID)
	if err != nil {
		t.Fatalf("read job_events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// A rate-limited attempt is deferred no earlier than the delay the upstream asked for, with the
// jitter spread upward, and the exponential backoff of repeated hits grows to its cap.
func TestRateLimitedFailureDefersAtLeastRetryAfterWithJitterAndGrows(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityHigh)

	layout := "2006-01-02T15:04:05.000Z"
	// rand 0.5 and jitter 0.2 add 10% of the base. Attempt 1: Retry-After 10m beats the 30s
	// curve; attempt 2: 60s curve is below 10m again; attempt 5 would reach 8m on the curve.
	for attempt, base := range []time.Duration{10 * time.Minute, 10 * time.Minute, 10 * time.Minute} {
		job := e.claim(t)
		outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{RetryAfter: base, RateLimited: true})
		if err != nil || outcome != queue.FailRequeued {
			t.Fatalf("Fail %d = %s, %v; want requeued", attempt+1, outcome, err)
		}
		want := e.clock.Now().Add(base + base/10).Format(layout)
		if row := e.row(t, job.ID); row.RunAfter != want || row.Attempts != attempt+1 {
			t.Fatalf("after hit %d: run_after %s attempts %d, want %s and %d", attempt+1, row.RunAfter, row.Attempts, want, attempt+1)
		}
		e.clock.Advance(base)
		e.wantNoClaim(t) // the jitter is on top of the reset, never before it
		e.clock.Advance(base / 10)
	}

	// Without a Retry-After the delay is the exponential curve (30s doubling per attempt),
	// capped at the hour: the fourth hit onward is 4m, 8m, 16m, 32m, then 1h.
	var delays []time.Duration
	for range 4 {
		job := e.claim(t)
		before := e.clock.Now()
		if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{RateLimited: true}); err != nil {
			t.Fatalf("Fail: %v", err)
		}
		next, err := time.Parse("2006-01-02T15:04:05.000Z", e.row(t, job.ID).RunAfter)
		if err != nil {
			t.Fatal(err)
		}
		delays = append(delays, next.Sub(before))
		e.clock.Advance(next.Sub(before))
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] <= delays[i-1] {
			t.Fatalf("delays = %v, want each rate-limited retry longer than the one before", delays)
		}
	}
	if delays[3] != 32*time.Minute+32*time.Minute/10 {
		t.Errorf("seventh hit delay = %v, want 32m plus the 10%% jitter", delays[3])
	}
}

func TestRateLimitedJobGetsEightAttemptsAndEveryAttemptIsAnEvent(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityHigh)

	for attempt := 1; attempt <= 7; attempt++ {
		job := e.claim(t)
		outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{RateLimited: true, RetryAfter: time.Minute})
		if err != nil || outcome != queue.FailRequeued {
			t.Fatalf("Fail %d = %s, %v; a rate-limited job has more than the plain 3 attempts", attempt, outcome, err)
		}
		e.clock.Advance(2 * time.Hour)
	}
	job := e.claim(t)
	outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{RateLimited: true})
	if err != nil || outcome != queue.FailExhausted {
		t.Fatalf("Fail 8 = %s, %v; want failed", outcome, err)
	}
	row := e.row(t, job.ID)
	if row.State != "failed" || row.Attempts != 8 {
		t.Fatalf("job = %+v, want failed after 8 attempts", row)
	}
	events := e.events(t, job.ID)
	if len(events) != 8 {
		t.Fatalf("events = %v, want one per attempt", events)
	}
	for i, ev := range events[:7] {
		if want := "rate_limited: attempt " + string(rune('1'+i)) + " of 8 was rate limited; the next one is not before "; len(ev) < len(want) || ev[:len(want)] != want {
			t.Errorf("event %d = %q, want it to start %q", i+1, ev, want)
		}
	}
	if want := "rate_limited: attempt 8 of 8 was rate limited; no attempts are left"; events[7] != want {
		t.Errorf("last event = %q, want %q", events[7], want)
	}
	var linked int
	if err := e.db.Writer.QueryRow(`SELECT COUNT(*) FROM job_events ev JOIN job_runs r ON r.id = ev.job_run_id WHERE ev.job_id = ?`, job.ID).Scan(&linked); err != nil || linked != 8 {
		t.Errorf("events linked to their attempt's job_runs row = %d, %v, want 8", linked, err)
	}
}

func TestPlainFailureKeepsThreeAttemptsEvenWhenAnEarlierOneWasRateLimited(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityHigh)

	job := e.claim(t)
	if _, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{RateLimited: true}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Hour)
	for i := range 2 {
		job = e.claim(t)
		outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{})
		want := queue.FailRequeued
		if i == 1 {
			want = queue.FailExhausted
		}
		if err != nil || outcome != want {
			t.Fatalf("plain Fail after a rate limit = %s, %v, want %s", outcome, err, want)
		}
		e.clock.Advance(time.Hour)
	}
	if events := e.events(t, job.ID); len(events) != 2 {
		t.Fatalf("events = %v, want one for the rate-limited attempt and one for the retried plain failure", events)
	}
}

// A job of an installation whose budget is exhausted stays queued, not failed, and is claimed
// once the reset has passed; webhook-triggered (high priority) jobs go first afterwards.
func TestClaimWaitsForTheInstallationsBudgetToReset(t *testing.T) {
	e := newJobEnv(t, 2)
	ctx := context.Background()
	e.setCap(t, 4)
	e.enqueue(t, e.repos[0], queue.PriorityNormal)
	e.enqueue(t, e.repos[1], queue.PriorityHigh)

	reset := e.clock.Now().Add(20 * time.Minute)
	if err := e.rateLimits().SaveRateLimit(ctx, ratelimit.State{InstallationID: 1, Remaining: 0, Limit: 5000, ResetAt: reset}); err != nil {
		t.Fatalf("SaveRateLimit: %v", err)
	}
	e.wantNoClaim(t)
	e.clock.Advance(19 * time.Minute)
	e.wantNoClaim(t)
	if n := e.count(t, `state = 'queued'`); n != 2 {
		t.Fatalf("queued jobs = %d, want both deferred, none failed", n)
	}

	e.clock.Advance(2 * time.Minute)
	first := e.claim(t)
	if first.Priority != queue.PriorityHigh {
		t.Errorf("first claim after the reset = %s priority, want the webhook-triggered job first", first.Priority)
	}
	e.claim(t)
}

func TestClaimIsNotHeldByABudgetWithRequestsLeft(t *testing.T) {
	e := newJobEnv(t, 1)
	e.enqueue(t, e.repos[0], queue.PriorityNormal)
	if err := e.rateLimits().SaveRateLimit(context.Background(), ratelimit.State{InstallationID: 1, Remaining: 1, Limit: 5000, ResetAt: e.clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	e.claim(t)
}

func TestRateLimitsRoundTripOnTheInstallationRow(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	rl := e.rateLimits()

	if got, err := rl.Load(ctx); err != nil || len(got) != 0 {
		t.Fatalf("Load before any budget = %v, %v, want none", got, err)
	}
	reset := time.Date(2026, 11, 14, 13, 0, 0, 0, time.UTC)
	want := ratelimit.State{InstallationID: 1, Remaining: 0, Limit: 5000, ResetAt: reset}
	if err := rl.SaveRateLimit(ctx, want); err != nil {
		t.Fatal(err)
	}
	if err := rl.SaveRateLimit(ctx, ratelimit.State{InstallationID: 999, Remaining: 3, ResetAt: reset}); err != nil {
		t.Errorf("SaveRateLimit of an unknown installation = %v, want it dropped without an error", err)
	}
	got, err := rl.Load(ctx)
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("Load = %+v, %v, want %+v", got, err, want)
	}
	var login string
	if err := e.db.Writer.QueryRow(`SELECT account_login FROM installations WHERE github_installation_id = 1`).Scan(&login); err != nil || login != "acme" {
		t.Errorf("installation row = %q, %v, want it otherwise untouched", login, err)
	}
}

// A budget whose reset was reported far in the future is stored capped: it round-trips through
// the installation row and holds a queued job back for the cap at most.
func TestAFarResetIsCappedBeforeItReachesTheClaim(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityNormal)

	clock := e.clock
	b := ratelimit.NewBudget(ratelimit.Options{Now: clock.Now, Store: e.rateLimits()})
	b.Observe(1, ratelimit.Parse(http.StatusForbidden, http.Header{
		"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"999999999999"},
	}, clock.Now()))
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := e.rateLimits().Load(ctx)
	capped := clock.Now().Add(ratelimit.MaxUpstreamDelay).Truncate(time.Millisecond)
	if err != nil || len(got) != 1 || !got[0].ResetAt.Equal(capped) {
		t.Fatalf("Load = %+v, %v, want the reset capped to %v", got, err, capped)
	}
	e.wantNoClaim(t)
	clock.Advance(ratelimit.MaxUpstreamDelay + time.Minute)
	e.claim(t)
}

// A stored reset time that cannot be read, or that lies beyond the cap, neither stops the
// server from starting nor holds work back for longer than the cap: the budget is seeded without
// it or capped, and the next flush writes the usable value back to the row the claim reads.
func TestLoadToleratesAStoredResetItCannotUse(t *testing.T) {
	ctx := context.Background()
	for _, stored := range []string{"33658-09-27T01:46:39.000Z", "not a time", ""} {
		e := newJobEnv(t, 1)
		e.enqueue(t, e.repos[0], queue.PriorityNormal)
		if _, err := e.db.Writer.Exec(`UPDATE installations SET rate_limit_remaining = 0, rate_limit_reset_at = ? WHERE github_installation_id = 1`, stored); err != nil {
			t.Fatal(err)
		}
		got, err := e.rateLimits().Load(ctx)
		if err != nil || len(got) != 1 || !got[0].ResetAt.IsZero() {
			t.Fatalf("Load with reset %q = %+v, %v, want startup not to fail and the budget without a reset", stored, got, err)
		}

		b := ratelimit.NewBudget(ratelimit.Options{Now: e.clock.Now, Store: e.rateLimits()})
		b.Seed(got[0])
		if err := b.Gate(1, ""); err != nil {
			t.Errorf("Gate with reset %q = %v, want nothing held back", stored, err)
		}
		if err := b.Flush(ctx); err != nil {
			t.Fatal(err)
		}
		e.claim(t)
	}
}

// A stored reset that is readable but beyond the cap is cut when it is seeded and the row is
// rewritten, so the claim does not wait for it either.
func TestLoadedFarResetIsCappedAndWrittenBack(t *testing.T) {
	e := newJobEnv(t, 1)
	ctx := context.Background()
	e.enqueue(t, e.repos[0], queue.PriorityNormal)
	if _, err := e.db.Writer.Exec(`UPDATE installations SET rate_limit_remaining = 0, rate_limit_reset_at = '9999-12-31T00:00:00.000Z' WHERE github_installation_id = 1`); err != nil {
		t.Fatal(err)
	}
	got, err := e.rateLimits().Load(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	b := ratelimit.NewBudget(ratelimit.Options{Now: e.clock.Now, Store: e.rateLimits()})
	b.Seed(got[0])
	if until, held := b.Until(1); !held || !until.Equal(e.clock.Now().Add(ratelimit.MaxUpstreamDelay)) {
		t.Fatalf("Until = %v, %v, want the cap", until, held)
	}
	if err := b.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	e.wantNoClaim(t)
	e.clock.Advance(ratelimit.MaxUpstreamDelay + time.Minute)
	e.claim(t)
}
