package ratelimit

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// FlushInterval is how often changed budgets are written to the store while calls go on. A
// budget that becomes exhausted, and a new reset window, are written at once.
const FlushInterval = 5 * time.Second

// State is the budget of one installation as last seen in a response.
type State struct {
	// InstallationID is the GitHub installation id.
	InstallationID int64
	Remaining      int64
	// Limit is zero until a response named it.
	Limit   int64
	ResetAt time.Time
}

// Exhausted reports that no request remains before the reset.
func (s State) Exhausted(now time.Time) bool {
	return s.Remaining <= 0 && s.ResetAt.After(now)
}

// Store keeps the budgets across a restart, on the installation row (specs.html § 4.4).
type Store interface {
	SaveRateLimit(ctx context.Context, s State) error
}

// Recorder counts what the budget sees; the /metrics counters implement it.
type Recorder interface {
	// Hit is one answer that refused a call because of a rate limit.
	Hit(installationID int64)
	// Deferred is one piece of work held back or refused because of the budget.
	Deferred(installationID int64, consumer string)
}

// Options configures a Budget.
type Options struct {
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Store, when set, receives the changed budgets from Run and Flush.
	Store Store
	// Recorder, when set, counts hits and deferrals.
	Recorder Recorder
}

// Budget holds the rate-limit budget of every installation, shared by every token minted for
// it. It is safe for concurrent use; a nil *Budget does nothing and defers nothing.
type Budget struct {
	now      func() time.Time
	store    Store
	recorder Recorder
	wake     chan struct{}

	mu       sync.Mutex
	states   map[int64]State
	dirty    map[int64]bool
	bindings map[string]binding
}

type binding struct {
	installationID int64
	expiresAt      time.Time
}

// NewBudget returns an empty budget.
func NewBudget(opts Options) *Budget {
	b := &Budget{
		now: opts.Now, store: opts.Store, recorder: opts.Recorder,
		wake:   make(chan struct{}, 1),
		states: map[int64]State{}, dirty: map[int64]bool{}, bindings: map[string]binding{},
	}
	if b.now == nil {
		b.now = time.Now
	}
	return b
}

// SetRecorder sets the recorder; it is called once, before the budget is used.
func (b *Budget) SetRecorder(r Recorder) {
	if b != nil {
		b.recorder = r
	}
}

// Now is the budget's clock.
func (b *Budget) Now() time.Time {
	if b == nil {
		return time.Now()
	}
	return b.now()
}

// Bind records that token is an access token of the installation, so a response to a call made
// with it counts against that installation's budget. The binding ends with the token.
func (b *Budget) Bind(token string, installationID int64, expiresAt time.Time) {
	if b == nil || token == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	for t, bound := range b.bindings {
		if !bound.expiresAt.After(now) {
			delete(b.bindings, t)
		}
	}
	b.bindings[token] = binding{installationID: installationID, expiresAt: expiresAt}
}

func (b *Budget) installationOf(token string) (int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bound, ok := b.bindings[token]
	if !ok || !bound.expiresAt.After(b.now()) {
		return 0, false
	}
	return bound.installationID, true
}

// Seed loads a budget stored before a restart. A reset time further away than MaxUpstreamDelay
// is cut to it, and a stored value that was cut or had no usable reset time is written back at
// the next flush.
func (b *Budget) Seed(s State) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.states[s.InstallationID]; ok {
		return
	}
	limit := b.now().Add(MaxUpstreamDelay)
	if s.ResetAt.After(limit) {
		s.ResetAt = limit
		b.dirty[s.InstallationID] = true
	} else if s.ResetAt.IsZero() {
		b.dirty[s.InstallationID] = true
	}
	b.states[s.InstallationID] = s
}

// ObserveToken records a response to a call made with token; a token that is not an
// installation token of this instance is ignored.
func (b *Budget) ObserveToken(token string, s Signal) {
	if b == nil || s.Empty() {
		return
	}
	if id, ok := b.installationOf(token); ok {
		b.Observe(id, s)
	}
}

// Observe records what a response says about the installation's budget. Within one reset
// window the lowest remaining count wins, so responses that arrive out of order cannot raise
// it; a later window replaces an earlier one, except that an exhausted budget is kept until its
// reset by any answer that is not itself limited. A Retry-After counts as exhausted until it
// lapses, which is stored as remaining 0 with the reset at that time.
func (b *Budget) Observe(installationID int64, s Signal) {
	if b == nil || s.Empty() {
		return
	}
	now := b.now()
	next := State{InstallationID: installationID, Remaining: s.Remaining, Limit: s.Limit, ResetAt: s.ResetAt}
	switch {
	case s.Limited && s.RetryAfter > 0, s.Limited && !s.HasRemaining:
		next.Remaining, next.ResetAt = 0, s.Until(now)
	case s.Limited && s.Remaining == 0 && !s.ResetAt.After(now):
		next.ResetAt = s.Until(now)
	case s.Limited && s.Remaining > 0:
		// Refused although the count says requests remain: hold work back for the fallback
		// delay, not until the primary reset.
		next.Remaining, next.ResetAt = 0, s.Until(now)
	}
	// A reset time is trusted for at most MaxUpstreamDelay, even in a signal not made by Parse.
	if limit := now.Add(MaxUpstreamDelay); next.ResetAt.After(limit) {
		next.ResetAt = limit
	}

	b.mu.Lock()
	cur, known := b.states[installationID]
	wasExhausted := known && cur.Exhausted(now)
	if !s.HasRemaining && !s.Limited {
		// Only a limit or a reset time: keep what is known about the remaining count.
		if !known {
			b.mu.Unlock()
			return
		}
		next.Remaining = cur.Remaining
	}
	if next.ResetAt.IsZero() && known {
		next.ResetAt = cur.ResetAt
	}
	if !s.HasLimit && known {
		next.Limit = cur.Limit
	}
	replace := !known || !cur.ResetAt.After(now)
	// An exhausted budget is only lifted by a limited answer or once its reset has passed: an
	// answer to a call that was already in flight, carrying a later primary reset, must not end
	// a Retry-After cooldown early.
	if !replace && (!wasExhausted || s.Limited) {
		switch {
		case next.ResetAt.After(cur.ResetAt):
			replace = true
		case next.ResetAt.Equal(cur.ResetAt):
			replace = next.Remaining < cur.Remaining || (s.HasLimit && next.Limit != cur.Limit)
			next.Remaining = min(next.Remaining, cur.Remaining)
		default:
			replace = s.Limited
		}
	}
	urgent := false
	if replace {
		b.states[installationID] = next
		b.dirty[installationID] = true
		urgent = next.Exhausted(now) != wasExhausted || !next.ResetAt.Equal(cur.ResetAt)
	}
	b.mu.Unlock()

	if s.Limited && b.recorder != nil {
		b.recorder.Hit(installationID)
	}
	if urgent {
		select {
		case b.wake <- struct{}{}:
		default:
		}
	}
}

// Until returns when the installation's budget is available again, and false while it is.
func (b *Budget) Until(installationID int64) (time.Time, bool) {
	if b == nil {
		return time.Time{}, false
	}
	now := b.now()
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.states[installationID]
	if !ok || !s.Exhausted(now) {
		return time.Time{}, false
	}
	return s.ResetAt, true
}

// Gate refuses work of the installation while its budget is exhausted: it returns a
// LimitedError that names the reset and counts one deferral for the consumer. A consumer
// of "" refuses without counting.
func (b *Budget) Gate(installationID int64, consumer string) error {
	until, exhausted := b.Until(installationID)
	if !exhausted {
		return nil
	}
	b.deferred(installationID, consumer)
	return NewLimitedError(until, b.now())
}

// GateAny is Gate for work that is not tied to one installation, such as the redelivery of
// webhook deliveries: it is refused while any installation's budget is exhausted.
func (b *Budget) GateAny(consumer string) error {
	if b == nil {
		return nil
	}
	now := b.now()
	b.mu.Lock()
	var (
		id    int64
		until time.Time
	)
	for _, s := range b.states {
		if s.Exhausted(now) && s.ResetAt.After(until) {
			id, until = s.InstallationID, s.ResetAt
		}
	}
	b.mu.Unlock()
	if until.IsZero() {
		return nil
	}
	b.deferred(id, consumer)
	return NewLimitedError(until, now)
}

// GateToken is Gate for a call made with token; a token that is not bound is not gated.
func (b *Budget) GateToken(token string) error {
	if b == nil {
		return nil
	}
	id, ok := b.installationOf(token)
	if !ok {
		return nil
	}
	return b.Gate(id, "")
}

// Deferred counts one piece of work of the consumer held back because of the budget.
func (b *Budget) Deferred(installationID int64, consumer string) {
	if b != nil {
		b.deferred(installationID, consumer)
	}
}

func (b *Budget) deferred(installationID int64, consumer string) {
	if consumer != "" && b.recorder != nil {
		b.recorder.Deferred(installationID, consumer)
	}
}

// Snapshot returns the known budgets, ordered by installation id.
func (b *Budget) Snapshot() []State {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	out := make([]State, 0, len(b.states))
	for _, s := range b.states {
		out = append(out, s)
	}
	b.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].InstallationID < out[j].InstallationID })
	return out
}

// Flush writes the budgets that changed since the last flush. A budget whose write fails stays
// changed and is written by the next flush.
func (b *Budget) Flush(ctx context.Context) error {
	if b == nil || b.store == nil {
		return nil
	}
	b.mu.Lock()
	var pending []State
	for id := range b.dirty {
		pending = append(pending, b.states[id])
	}
	b.dirty = map[int64]bool{}
	b.mu.Unlock()

	var firstErr error
	for _, s := range pending {
		if err := b.store.SaveRateLimit(ctx, s); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			b.mu.Lock()
			b.dirty[s.InstallationID] = true
			b.mu.Unlock()
		}
	}
	return firstErr
}

// Run flushes every FlushInterval, and at once when a budget becomes exhausted or enters a new
// window, until ctx ends; it then flushes once more.
func (b *Budget) Run(ctx context.Context) {
	if b == nil || b.store == nil {
		return
	}
	t := time.NewTicker(FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := b.Flush(flushCtx); err != nil {
				slog.Error("rate limit: the final write of the budgets failed", "err", err)
			}
			cancel()
			return
		case <-b.wake:
		case <-t.C:
		}
		if err := b.Flush(ctx); err != nil && ctx.Err() == nil {
			slog.Error("rate limit: writing the budgets failed", "err", err)
		}
	}
}
