package worker

import (
	"context"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// Result is what a handler reports for an attempt.
type Result struct {
	// Err is nil for a succeeded attempt.
	Err error
	// RetryAfter is the minimum delay before a retry, when an upstream asked for one.
	RetryAfter time.Duration
	// RateLimit is the last rate-limit state of GitHub the attempt saw, when it saw any: it
	// updates the installation's shared budget, and a Limited one makes the attempt a
	// rate-limited failure.
	RateLimit *queue.RateLimitReport
	// Run is the attempt's record for the app to store; nil reports the outcome alone. A
	// record with NoConfig set reports a succeeded attempt Renovate skipped for a missing
	// config as the no_config outcome.
	Run *queue.RunRecord
}

// skippedForNoConfig reports whether the attempt ended because Renovate skipped the repository
// for having no config.
func (r Result) skippedForNoConfig() bool { return r.Run != nil && r.Run.NoConfig }

// Handler runs one claimed job. It returns when the job is done or ctx ends: ctx is
// cancelled when the app aborts the job or the worker runs out of its shutdown grace.
type Handler interface {
	Run(ctx context.Context, job queue.ClaimedJob) Result
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, job queue.ClaimedJob) Result

// Run implements Handler.
func (f HandlerFunc) Run(ctx context.Context, job queue.ClaimedJob) Result {
	return f(ctx, job)
}

// Registry maps a job kind to its handler.
type Registry map[queue.Kind]Handler
