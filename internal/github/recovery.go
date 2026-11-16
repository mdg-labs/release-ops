package github

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// RedeliveryInterval is how often the app redelivers failed webhook deliveries after the
	// pass at startup.
	RedeliveryInterval = 15 * time.Minute
	// PollingInterval is how often the installation and its repositories are re-read from
	// GitHub when no webhook is configured. It gives minutes of latency without spending the
	// rate limit of a small installation.
	PollingInterval = 5 * time.Minute
	// DefaultRedeliveryLookback is how far back failed deliveries are looked for: it covers a
	// restart window without replaying old history, and stays shorter than the retention of
	// the receiver's delivery records.
	DefaultRedeliveryLookback = 24 * time.Hour
)

// RecoveryOptions configures a Recovery.
type RecoveryOptions struct {
	// Lookback is how far back failed deliveries are redelivered; zero means
	// DefaultRedeliveryLookback.
	Lookback time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Recovery asks GitHub to redeliver webhook deliveries the receiver did not accept. GitHub
// does not retry a failed delivery by itself. A redelivery goes to the webhook URL like the
// original, so it passes the same signature check and the same GUID dedupe; nothing is
// applied from here.
type Recovery struct {
	app      *AppClient
	lookback time.Duration
	now      func() time.Time
}

// NewRecovery returns a Recovery that reads and redelivers through app.
func NewRecovery(app *AppClient, opts RecoveryOptions) *Recovery {
	r := &Recovery{app: app, lookback: opts.Lookback, now: opts.Now}
	if r.lookback <= 0 {
		r.lookback = DefaultRedeliveryLookback
	}
	if r.now == nil {
		r.now = time.Now
	}
	return r
}

// Redeliver redelivers, once per GUID, the failed deliveries of the lookback window: a GUID
// is left alone when any of its attempts since succeeded. It returns how many redeliveries
// GitHub accepted. Before the App is stored it does nothing. When the list cannot be read
// nothing is redelivered; a redelivery that fails does not stop the others, except one that is
// rate limited, and the errors of all are returned together. While an installation's rate-limit
// budget is exhausted nothing is listed or redelivered and the error matches
// ratelimit.ErrRateLimited; the next pass resumes.
func (r *Recovery) Redeliver(ctx context.Context) (int, error) {
	if err := r.app.GateAny(ratelimit.ConsumerRedelivery); err != nil {
		return 0, err
	}
	listed, err := r.app.Deliveries(ctx, r.now().Add(-r.lookback))
	if errors.Is(err, store.ErrGitHubAppNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("list deliveries: %w", err)
	}

	succeeded := map[string]bool{}
	for _, d := range listed {
		if d.Succeeded() {
			succeeded[d.GUID] = true
		}
	}
	// The listing is newest first: the first failed attempt of a GUID is the one to repeat,
	// and the repeats go out oldest event first.
	var pending []Delivery
	queued := map[string]bool{}
	for _, d := range listed {
		if d.Succeeded() || succeeded[d.GUID] || queued[d.GUID] {
			continue
		}
		queued[d.GUID] = true
		pending = append(pending, d)
	}

	var errs []error
	redelivered := 0
	for i := len(pending) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return redelivered, err
		}
		if err := r.app.Redeliver(ctx, pending[i].ID); err != nil {
			errs = append(errs, fmt.Errorf("redeliver delivery %d: %w", pending[i].ID, err))
			if errors.Is(err, ratelimit.ErrRateLimited) {
				break
			}
			continue
		}
		redelivered++
	}
	return redelivered, errors.Join(errs...)
}

// Run redelivers at once and then on every tick until ctx ends. report gets the result of
// every pass; a failed pass does not stop the next.
func (r *Recovery) Run(ctx context.Context, ticks <-chan time.Time, report func(redelivered int, err error)) {
	for {
		redelivered, err := r.Redeliver(ctx)
		if err == nil || ctx.Err() == nil {
			report(redelivered, err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}
