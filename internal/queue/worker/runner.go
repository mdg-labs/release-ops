package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/metrics"
	"github.com/mdg-labs/release-ops/internal/queue"
)

// Options configures a Runner. Zero fields take their defaults.
type Options struct {
	// Slots is the number of claim loops. The app refuses claims beyond the concurrency cap,
	// so extra loops only wait.
	Slots int
	// HeartbeatInterval is how often a running job heartbeats.
	HeartbeatInterval time.Duration
	// ShutdownGrace is how long running handlers get to finish after shutdown begins.
	ShutdownGrace time.Duration
	// ReleaseTimeout bounds releasing leases at shutdown.
	ReleaseTimeout time.Duration
	// RetryDelay is the pause after a failed claim before the next one.
	RetryDelay time.Duration
	// Logger receives worker events; no job content and no secret is logged.
	Logger *slog.Logger
	// Jobs counts and times the attempts; nil records nothing.
	Jobs *metrics.Jobs
}

const (
	// DefaultSlots is the number of claim loops of a worker.
	DefaultSlots             = 8
	defaultHeartbeatInterval = 30 * time.Second
	defaultShutdownGrace     = 30 * time.Second
	defaultReleaseTimeout    = 10 * time.Second
	defaultRetryDelay        = 5 * time.Second
)

// Runner claims jobs and runs them.
type Runner struct {
	client   *Client
	registry Registry
	opts     Options
	log      *slog.Logger

	mu      sync.Mutex
	running map[string]string // job id -> lease owner
}

// NewRunner returns a runner of the registry's handlers over the client.
func NewRunner(client *Client, registry Registry, opts Options) *Runner {
	if opts.Slots <= 0 {
		opts.Slots = DefaultSlots
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultHeartbeatInterval
	}
	if opts.ShutdownGrace <= 0 {
		opts.ShutdownGrace = defaultShutdownGrace
	}
	if opts.ReleaseTimeout <= 0 {
		opts.ReleaseTimeout = defaultReleaseTimeout
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = defaultRetryDelay
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Runner{client: client, registry: registry, opts: opts, log: log, running: map[string]string{}}
}

// Run claims and runs jobs until ctx ends. Then it stops claiming, gives running handlers
// ShutdownGrace to finish, cancels the ones still running and releases the leases of every
// job that has not reported a result.
func (r *Runner) Run(ctx context.Context) {
	// Handlers run under their own context so that ctx ending stops claiming without
	// cutting a job short.
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

	var wg sync.WaitGroup
	for range r.opts.Slots {
		wg.Go(func() { r.loop(ctx, jobCtx) })
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		return
	case <-ctx.Done():
	}

	grace := time.NewTimer(r.opts.ShutdownGrace)
	defer grace.Stop()
	select {
	case <-done:
		return
	case <-grace.C:
	}
	cancelJobs()

	finalWait := time.NewTimer(r.opts.ReleaseTimeout)
	defer finalWait.Stop()
	select {
	case <-done:
	case <-finalWait.C:
		r.log.Warn("worker: handlers did not stop; releasing their leases")
	}
	r.releaseRemaining()
}

func (r *Runner) loop(ctx, jobCtx context.Context) {
	for ctx.Err() == nil {
		job, err := r.client.Claim(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			r.log.Error("worker: claim failed", "err", err)
			sleep(ctx, r.opts.RetryDelay)
			continue
		}
		if job == nil {
			continue
		}
		r.runJob(ctx, jobCtx, *job)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (r *Runner) track(job queue.ClaimedJob) {
	r.mu.Lock()
	r.running[job.ID] = job.LeaseOwner
	r.mu.Unlock()
}

func (r *Runner) untrack(jobID string) {
	r.mu.Lock()
	delete(r.running, jobID)
	r.mu.Unlock()
}

func (r *Runner) runJob(ctx, jobCtx context.Context, job queue.ClaimedJob) {
	started := time.Now()
	outcome := r.runAttempt(ctx, jobCtx, job)
	r.opts.Jobs.Ended(job.RepositoryFullName, outcome)
	r.opts.Jobs.Took(job.RepositoryFullName, time.Since(started))
}

// runAttempt runs the job and reports its result, and returns the outcome of the attempt
// for the job metrics: an attempt the worker stopped or gave back is aborted.
func (r *Runner) runAttempt(ctx, jobCtx context.Context, job queue.ClaimedJob) string {
	r.track(job)
	defer r.untrack(job.ID)

	runCtx, cancelRun := context.WithCancel(jobCtx)
	defer cancelRun()

	var heartbeats sync.WaitGroup
	hbStop := make(chan struct{})
	lost := make(chan struct{})
	heartbeats.Go(func() { r.heartbeat(job, hbStop, lost, cancelRun) })

	res := r.invoke(runCtx, job)
	close(hbStop)
	heartbeats.Wait()

	select {
	case <-lost:
		r.log.Warn("worker: lease lost, result dropped", "job", job.ID)
		return metrics.OutcomeAborted
	default:
	}

	// A handler cut short by the end of the shutdown grace did not finish: give the job
	// back instead of reporting a failed attempt.
	if jobCtx.Err() != nil && res.Err != nil {
		r.release(job)
		return metrics.OutcomeAborted
	}
	r.report(job, res)
	if res.Err != nil {
		return metrics.OutcomeFailed
	}
	return metrics.OutcomeSucceeded
}

func (r *Runner) invoke(ctx context.Context, job queue.ClaimedJob) (res Result) {
	h, ok := r.registry[job.Kind]
	if !ok {
		return Result{Err: fmt.Errorf("no handler for job kind %q", job.Kind)}
	}
	defer func() {
		if p := recover(); p != nil {
			res = Result{Err: fmt.Errorf("handler panicked: %v", p)}
		}
	}()
	return h.Run(ctx, job)
}

// heartbeat renews the lease until stop closes, the first time at once: that heartbeat marks
// the job started. A lost lease or an abort closes lost and cancels the handler.
func (r *Runner) heartbeat(job queue.ClaimedJob, stop <-chan struct{}, lost chan<- struct{}, cancel context.CancelFunc) {
	t := time.NewTicker(r.opts.HeartbeatInterval)
	defer t.Stop()
	for {
		res, err := r.client.Heartbeat(context.Background(), job.ID, job.LeaseOwner)
		switch {
		case errors.Is(err, ErrLeaseLost) || (err == nil && res.Abort):
			close(lost)
			cancel()
			return
		case err != nil:
			r.log.Warn("worker: heartbeat failed", "job", job.ID, "err", err)
		}
		select {
		case <-stop:
			return
		case <-t.C:
		}
	}
}

func (r *Runner) report(job queue.ClaimedJob, res Result) {
	req := queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: queue.OutcomeSucceeded, Run: res.Run}
	if res.Err != nil {
		req.Outcome = queue.OutcomeFailed
		req.RetryAfterSeconds = int64(res.RetryAfter / time.Second)
		r.log.Warn("worker: job failed", "job", job.ID, "kind", job.Kind)
	}
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.ReleaseTimeout)
	defer cancel()
	if err := r.client.Result(ctx, job.ID, req); err != nil {
		r.log.Error("worker: reporting the result failed", "job", job.ID, "err", err)
	}
}

func (r *Runner) release(job queue.ClaimedJob) {
	ctx, cancel := context.WithTimeout(context.Background(), r.opts.ReleaseTimeout)
	defer cancel()
	if err := r.client.Release(ctx, job.ID, job.LeaseOwner); err != nil {
		r.log.Error("worker: releasing the lease failed", "job", job.ID, "err", err)
	}
}

// releaseRemaining releases the lease of every job still running.
func (r *Runner) releaseRemaining() {
	r.mu.Lock()
	remaining := make(map[string]string, len(r.running))
	for id, owner := range r.running {
		remaining[id] = owner
	}
	r.mu.Unlock()
	for id, owner := range remaining {
		r.release(queue.ClaimedJob{ID: id, LeaseOwner: owner})
	}
}
