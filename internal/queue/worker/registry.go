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
}

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

// NoopHandler succeeds at once. It stands in for the kinds whose real handler is not wired.
var NoopHandler Handler = HandlerFunc(func(context.Context, queue.ClaimedJob) Result { return Result{} })
