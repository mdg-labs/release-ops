// Package scheduler is the hourly safety net of specs.html § 2.9: on every tick it enqueues one
// normal-priority job per repository that takes part, so a missed webhook is noticed within
// the hour. The interval is fixed; there is no setting for it (§ 5.7).
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// Interval is how often the safety net runs.
const Interval = time.Hour

// Repositories lists the repositories a tick enqueues.
type Repositories interface {
	// ListScheduled returns the ids of the enabled repositories of an active installation that
	// are not recorded as having no config and have no queued or running job.
	ListScheduled(ctx context.Context) ([]string, error)
}

// Enqueuer is the queue's enqueue call.
type Enqueuer interface {
	Enqueue(ctx context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error)
}

// Scheduler enqueues the safety-net jobs.
type Scheduler struct {
	repos Repositories
	queue Enqueuer
	// AfterTick, when set, runs after every tick of Run, whether or not the tick failed: the
	// PR reconcile of specs.html section 5.7 runs hourly with the safety net. It must not
	// block.
	AfterTick func(ctx context.Context)
}

// New returns a Scheduler over the repositories and the queue.
func New(repos Repositories, q Enqueuer) *Scheduler {
	return &Scheduler{repos: repos, queue: q}
}

// Result counts what a tick did.
type Result struct {
	// Enqueued is the number of repositories that got a new queued job.
	Enqueued int
	// Skipped is the number of repositories the queue did not take a job for: one was paused
	// since the list was read, or it already had a job the trigger folds into.
	Skipped int
}

// Tick enqueues one normal-priority job per scheduled repository. A failure on one repository
// does not stop the others; the errors of all are returned together.
func (s *Scheduler) Tick(ctx context.Context) (Result, error) {
	ids, err := s.repos.ListScheduled(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("list scheduled repositories: %w", err)
	}
	var res Result
	var errs []error
	for _, id := range ids {
		out, err := s.queue.Enqueue(ctx, queue.EnqueueParams{
			RepositoryID: id,
			Kind:         queue.KindRenovateRun,
			Priority:     queue.PriorityNormal,
			Trigger:      queue.TriggerSchedule,
		})
		switch {
		case errors.Is(err, queue.ErrRepositoryPaused), errors.Is(err, queue.ErrRepositoryNotFound):
			res.Skipped++
		case err != nil:
			errs = append(errs, fmt.Errorf("repository %s: %w", id, err))
		case out.Outcome == queue.EnqueueQueued:
			res.Enqueued++
		default:
			res.Skipped++
		}
	}
	return res, errors.Join(errs...)
}

// Run ticks on every value of ticks until ctx ends; the caller owns the clock, so a test drives
// it with a channel of its own. It does not tick at start: an instance that restarts more often
// than the interval would otherwise re-enqueue every repository on every restart. report gets
// the result of every tick; a failed tick does not stop the next.
func (s *Scheduler) Run(ctx context.Context, ticks <-chan time.Time, report func(Result, error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		res, err := s.Tick(ctx)
		if err != nil && ctx.Err() != nil {
			return
		}
		report(res, err)
		if s.AfterTick != nil {
			s.AfterTick(ctx)
		}
	}
}
