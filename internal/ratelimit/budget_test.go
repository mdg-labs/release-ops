package ratelimit_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type recorder struct {
	mu       sync.Mutex
	hits     map[int64]int
	deferred map[string]int
}

func newRecorder() *recorder {
	return &recorder{hits: map[int64]int{}, deferred: map[string]int{}}
}

func (r *recorder) Hit(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits[id]++
}

func (r *recorder) Deferred(id int64, consumer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deferred[strconv.FormatInt(id, 10)+"/"+consumer]++
}

type memStore struct {
	mu    sync.Mutex
	saved []ratelimit.State
	fail  error
}

func (m *memStore) SaveRateLimit(_ context.Context, s ratelimit.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.saved = append(m.saved, s)
	return nil
}

func sig(remaining int, reset time.Time) ratelimit.Signal {
	return ratelimit.Parse(http.StatusOK, header(
		"X-RateLimit-Remaining", strconv.Itoa(remaining), "X-RateLimit-Limit", "5000",
		"X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10)), now)
}

func TestBudgetDefersUntilTheResetThenResumes(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	rec := newRecorder()
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now, Recorder: rec})
	reset := now.Add(20 * time.Minute)

	b.Observe(7, sig(4000, reset))
	if err := b.Gate(7, ratelimit.ConsumerReconcile); err != nil {
		t.Fatalf("Gate with requests left = %v", err)
	}

	b.Observe(7, sig(0, reset))
	for _, consumer := range []string{ratelimit.ConsumerRenovateRun, ratelimit.ConsumerReconcile, ratelimit.ConsumerRedelivery, ratelimit.ConsumerRoleCheck} {
		err := b.Gate(7, consumer)
		if !errors.Is(err, ratelimit.ErrRateLimited) {
			t.Fatalf("Gate(%s) = %v, want ErrRateLimited while the budget is at 0", consumer, err)
		}
		if d, _ := ratelimit.DelayOf(err); d != 20*time.Minute {
			t.Errorf("Gate(%s) delay = %v, want the 20 minutes to the reset", consumer, d)
		}
		if rec.deferred["7/"+consumer] != 1 {
			t.Errorf("deferrals of %s = %d, want 1", consumer, rec.deferred["7/"+consumer])
		}
	}
	if err := b.GateAny(ratelimit.ConsumerRedelivery); !errors.Is(err, ratelimit.ErrRateLimited) {
		t.Errorf("GateAny = %v, want a deferral while any installation is exhausted", err)
	}
	if err := b.Gate(8, ratelimit.ConsumerReconcile); err != nil {
		t.Errorf("another installation is gated by this one's budget: %v", err)
	}

	c.Advance(19 * time.Minute)
	if err := b.Gate(7, ratelimit.ConsumerReconcile); err == nil {
		t.Error("Gate before the reset = nil")
	}
	c.Advance(time.Minute)
	if err := b.Gate(7, ratelimit.ConsumerReconcile); err != nil {
		t.Errorf("Gate at the reset = %v, want work to resume", err)
	}
	if err := b.GateAny(ratelimit.ConsumerRedelivery); err != nil {
		t.Errorf("GateAny after the reset = %v", err)
	}
}

func TestBudgetRetryAfterCountsAsExhaustedUntilItLapses(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	rec := newRecorder()
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now, Recorder: rec})

	b.Observe(1, ratelimit.Parse(http.StatusForbidden, header("Retry-After", "45", "X-RateLimit-Remaining", "3000"), now))
	if rec.hits[1] != 1 {
		t.Errorf("hits = %d, want the limited answer counted", rec.hits[1])
	}
	if until, ok := b.Until(1); !ok || !until.Equal(now.Add(45*time.Second)) {
		t.Fatalf("Until = %v, %v, want the Retry-After", until, ok)
	}
	c.Advance(46 * time.Second)
	if _, ok := b.Until(1); ok {
		t.Error("still exhausted after the Retry-After")
	}
}

func TestBudgetKeepsTheLowestRemainingOfAWindowAndTakesALaterWindow(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now})
	reset := now.Add(30 * time.Minute)

	b.Observe(1, sig(100, reset))
	b.Observe(1, sig(150, reset)) // a response that arrived late
	if got := b.Snapshot()[0]; got.Remaining != 100 {
		t.Errorf("remaining = %d, want 100: a late response cannot raise it", got.Remaining)
	}
	b.Observe(1, sig(90, reset))
	b.Observe(1, sig(4000, now.Add(10*time.Minute))) // an older window
	if got := b.Snapshot()[0]; got.Remaining != 90 || !got.ResetAt.Equal(reset) {
		t.Errorf("state = %+v, want the older window ignored", got)
	}
	next := reset.Add(time.Hour)
	b.Observe(1, sig(4999, next))
	if got := b.Snapshot()[0]; got.Remaining != 4999 || !got.ResetAt.Equal(next) || got.Limit != 5000 {
		t.Errorf("state = %+v, want the later window", got)
	}
}

func TestBudgetCountsTokensOfTheirInstallationOnly(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now})
	b.Bind("token-a", 1, now.Add(time.Hour))
	b.Bind("token-b", 2, now.Add(time.Hour))

	b.ObserveToken("token-a", sig(0, now.Add(10*time.Minute)))
	b.ObserveToken("token-b", sig(2500, now.Add(10*time.Minute)))
	b.ObserveToken("unbound", sig(0, now.Add(10*time.Minute)))

	if len(b.Snapshot()) != 2 {
		t.Fatalf("snapshot = %+v, want separate budgets for the two installations only", b.Snapshot())
	}
	if err := b.GateToken("token-a"); !errors.Is(err, ratelimit.ErrRateLimited) {
		t.Errorf("GateToken(a) = %v, want deferred", err)
	}
	if err := b.GateToken("token-b"); err != nil {
		t.Errorf("GateToken(b) = %v", err)
	}
	if err := b.GateToken("unbound"); err != nil {
		t.Errorf("GateToken of a token that is not an installation token = %v", err)
	}
	c.Advance(2 * time.Hour)
	b.ObserveToken("token-a", sig(0, c.Now().Add(time.Hour)))
	if len(b.Snapshot()) != 2 {
		t.Error("an expired token's binding still counts")
	}
}

func TestBudgetSeedsStoredStateWithoutOverwritingNewerKnowledge(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now})
	b.Seed(ratelimit.State{InstallationID: 1, Remaining: 0, Limit: 5000, ResetAt: now.Add(time.Hour)})
	if err := b.Gate(1, ratelimit.ConsumerReconcile); !errors.Is(err, ratelimit.ErrRateLimited) {
		t.Fatalf("Gate after a restart = %v, want the stored exhaustion honoured", err)
	}
	b.Seed(ratelimit.State{InstallationID: 1, Remaining: 4000, ResetAt: now.Add(time.Hour)})
	if err := b.Gate(1, ""); err == nil {
		t.Error("a second Seed replaced what the budget already knew")
	}
}

func TestBudgetFlushWritesChangesAndRetriesAFailedWrite(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	st := &memStore{}
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now, Store: st})
	b.Observe(1, sig(10, now.Add(time.Hour)))

	st.fail = errors.New("database is locked")
	if err := b.Flush(context.Background()); err == nil {
		t.Fatal("Flush = nil, want the store's error")
	}
	st.fail = nil
	if err := b.Flush(context.Background()); err != nil || len(st.saved) != 1 || st.saved[0].Remaining != 10 {
		t.Fatalf("second Flush = %v, saved %+v, want the budget written after the failed attempt", err, st.saved)
	}
	if err := b.Flush(context.Background()); err != nil || len(st.saved) != 1 {
		t.Fatalf("a flush with nothing changed wrote %+v", st.saved)
	}
}

func TestBudgetRunWritesAnExhaustedBudgetAtOnce(t *testing.T) {
	t.Parallel()

	st := &memStore{}
	b := ratelimit.NewBudget(ratelimit.Options{Now: func() time.Time { return now }, Store: st})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.Run(ctx); close(done) }()

	b.Observe(1, sig(0, now.Add(time.Hour)))
	deadline := time.Now().Add(5 * time.Second)
	for {
		st.mu.Lock()
		n := len(st.saved)
		st.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("an exhausted budget was not written without waiting for the flush interval")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestNilBudgetDoesNothing(t *testing.T) {
	t.Parallel()

	var b *ratelimit.Budget
	b.Observe(1, sig(0, now.Add(time.Hour)))
	b.Bind("x", 1, now)
	if err := b.Gate(1, ratelimit.ConsumerReconcile); err != nil {
		t.Errorf("Gate = %v", err)
	}
	if err := b.GateAny(ratelimit.ConsumerRedelivery); err != nil {
		t.Errorf("GateAny = %v", err)
	}
	if err := b.GateToken("x"); err != nil {
		t.Errorf("GateToken = %v", err)
	}
	if b.Snapshot() != nil || b.Flush(context.Background()) != nil {
		t.Error("a nil budget has state")
	}
}

// An answer that says the budget is gone until a reset far in the future holds work back for
// the cap at most, whatever the status, and the budget that is written to the store carries the
// capped time.
func TestBudgetCapsAnUpstreamResetTime(t *testing.T) {
	t.Parallel()

	for name, status := range map[string]int{"403": http.StatusForbidden, "200": http.StatusOK} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := &clock{t: now}
			st := &memStore{}
			b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now, Store: st})
			b.Observe(7, ratelimit.Parse(status, header("X-RateLimit-Remaining", "0", "X-RateLimit-Reset", "99999999999"), now))

			err := b.Gate(7, ratelimit.ConsumerReconcile)
			if d, ok := ratelimit.DelayOf(err); !ok || d != ratelimit.MaxUpstreamDelay {
				t.Fatalf("Gate delay = %v, %v, want the cap of %v", d, ok, ratelimit.MaxUpstreamDelay)
			}
			if err := b.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if want := now.Add(ratelimit.MaxUpstreamDelay); len(st.saved) != 1 || !st.saved[0].ResetAt.Equal(want) {
				t.Fatalf("stored = %+v, want the reset capped to %v", st.saved, want)
			}
			c.Advance(ratelimit.MaxUpstreamDelay)
			if err := b.Gate(7, ratelimit.ConsumerReconcile); err != nil {
				t.Errorf("Gate once the cap has passed = %v, want work to resume", err)
			}
		})
	}
}

// A signal made by hand, not through Parse, is capped as well, and so is a stored budget that
// is seeded after a restart.
func TestBudgetCapsASignalAndASeedWithAFarReset(t *testing.T) {
	t.Parallel()

	c := &clock{t: now}
	b := ratelimit.NewBudget(ratelimit.Options{Now: c.Now})
	far := time.Date(5138, 11, 16, 0, 0, 0, 0, time.UTC)
	b.Observe(1, ratelimit.Signal{Remaining: 0, HasRemaining: true, ResetAt: far})
	b.Seed(ratelimit.State{InstallationID: 2, Remaining: 0, ResetAt: far})
	for _, id := range []int64{1, 2} {
		until, exhausted := b.Until(id)
		if !exhausted || !until.Equal(now.Add(ratelimit.MaxUpstreamDelay)) {
			t.Errorf("installation %d: Until = %v, %v, want the cap", id, until, exhausted)
		}
	}
}
