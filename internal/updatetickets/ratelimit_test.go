package updatetickets_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

func retryAfter(v string) http.Header { return http.Header{"Retry-After": {v}} }

func nextAttempt(t *testing.T, e *env, prID string) time.Time {
	t.Helper()
	row, _ := e.row(t, prID)
	at, err := time.Parse("2006-01-02T15:04:05.000Z", row.NextAttemptAt.String)
	if err != nil {
		t.Fatalf("next_attempt_at %q: %v", row.NextAttemptAt.String, err)
	}
	return at
}

// A 429 on the create stored nothing, so the marker is cleared and the retry waits for at least
// the Retry-After (with the jitter on top) and then creates exactly one ticket.
func TestARateLimitedCreateWaitsForRetryAfterAndThenCreatesOneTicket(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		header http.Header
		after  time.Duration
	}{
		"429 with Retry-After seconds": {http.StatusTooManyRequests, retryAfter("3600"), time.Hour},
		"429 with a Retry-After date":  {http.StatusTooManyRequests, retryAfter(time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)), 2 * time.Hour},
		"403 with Retry-After":         {http.StatusForbidden, retryAfter("5400"), 90 * time.Minute},
		"403 with an exhausted limit":  {http.StatusForbidden, http.Header{"X-RateLimit-Remaining": {"0"}, "X-RateLimit-Reset": {strconv.FormatInt(time.Now().Add(3*time.Hour).Unix(), 10)}}, 3 * time.Hour},
		"429 without any rate headers": {http.StatusTooManyRequests, nil, time.Minute},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.engine.Rand = func() float64 { return 0.5 } // 10% jitter on top
			e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/task/", Code: tc.status, Times: 1, Header: tc.header})
			id := e.addPR(t, pr{Deps: foo})

			if out, err := process(t, e, id); err == nil || out != updatetickets.OutcomeFailed {
				t.Fatalf("Process = %s, %v, want the rate-limited create to fail", out, err)
			}
			row, _ := e.row(t, id)
			if row.PendingSince.Valid || row.Attempts != 1 {
				t.Fatalf("row = %+v, want the marker cleared (a 4xx stored nothing) and one attempt", row)
			}
			// The ticket system's answer is read at the real time, the engine runs on the fake
			// clock: a date or a reset time is within two seconds of the delay it names.
			got := nextAttempt(t, e, id)
			if low, high := epoch.Add(tc.after-2*time.Second), epoch.Add(tc.after+tc.after/5); got.Before(low) || got.After(high) {
				t.Fatalf("next_attempt_at = %v, want within [%v, %v]: never before the %v the ticket system asked for, spread up by the jitter", got, low, high, tc.after)
			}

			e.advance(tc.after - 3*time.Second)
			if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeWaiting {
				t.Fatalf("a retry before the Retry-After = %s, %v, want it to wait", out, err)
			}
			if n := len(e.tracker.calls()); n != 1 {
				t.Fatalf("tracker calls = %d, want only the rate-limited one", n)
			}

			e.advance(tc.after/5 + 3*time.Second)
			if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
				t.Fatalf("the due retry = %s, %v, want created", out, err)
			}
			if creates := len(e.tracker.writes()); creates != 2 || len(e.tracker.tasks) != 1 {
				t.Fatalf("writes = %d, tasks = %d, want the refused create and one stored ticket", creates, len(e.tracker.tasks))
			}
		})
	}
}

// A rate limit on an update of an existing ticket is retried the same way.
func TestARateLimitedUpdateWaitsForRetryAfter(t *testing.T) {
	e := newEnv(t)
	e.engine.Rand = func() float64 { return 0 }
	id := e.ticketed(t, pr{})
	e.setDeps(t, id, []dep{{"foo", "1.4.0", "3.0.0", "major"}})
	e.tracker.fail(trackerFailure{Method: http.MethodPut, PathPrefix: "/api/task/", Code: http.StatusTooManyRequests, Times: 1, Header: retryAfter("7200")})

	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want a failure", out, err)
	}
	if got := nextAttempt(t, e, id); got.Before(epoch.Add(2 * time.Hour)) {
		t.Fatalf("next_attempt_at = %v, want no earlier than the Retry-After of two hours (the plain backoff is a minute)", got)
	}
	e.advance(2*time.Hour - time.Second)
	wantSync(t, e, id, updatetickets.SyncWaiting)
	e.advance(2 * time.Second)
	wantSync(t, e, id, updatetickets.SyncUpdated)
}

// An answer that is not a rate limit keeps the plain backoff.
func TestAPlainClientErrorKeepsTheOrdinaryBackoff(t *testing.T) {
	e := newEnv(t)
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/task/", Code: http.StatusForbidden, Times: 1})
	id := e.addPR(t, pr{Deps: foo})
	if _, err := process(t, e, id); err == nil {
		t.Fatal("want a failure")
	}
	if got := nextAttempt(t, e, id); !got.Equal(epoch.Add(time.Minute)) {
		t.Fatalf("next_attempt_at = %v, want the one-minute backoff for a 403 that is no rate limit", got)
	}
}
