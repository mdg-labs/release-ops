package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// Retention defaults (specs.html § 4.6 and § 2.10). A delivery record must outlast GitHub's
// three-day redelivery window.
const (
	// DefaultJobHistoryRetention is how long a finished job and its history are kept.
	DefaultJobHistoryRetention = 30 * 24 * time.Hour
	// DefaultWebhookDeliveryRetention is how long a delivery's GUID is remembered.
	DefaultWebhookDeliveryRetention = 7 * 24 * time.Hour
	// RetentionInterval is how often the retention pass runs.
	RetentionInterval = 24 * time.Hour
	// retentionResumeDelay is how soon the next pass runs when one stopped at its cap with
	// rows left.
	retentionResumeDelay = time.Minute

	// DefaultPruneBatch is how many rows one delete removes, and DefaultPruneMaxBatches how
	// many deletes of each kind one pass makes, so a pass touches at most their product of
	// rows however long the history is.
	DefaultPruneBatch      = 500
	DefaultPruneMaxBatches = 100
)

// RetentionOptions configures a Retention. Zero fields take their defaults.
type RetentionOptions struct {
	// JobHistory and WebhookDeliveries are the windows: finished jobs and delivery records
	// older than that are deleted.
	JobHistory, WebhookDeliveries time.Duration
	// Batch and MaxBatches bound a pass.
	Batch, MaxBatches int
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// Retention deletes finished job history and webhook delivery records past their windows, in
// bounded batches through the writer connection.
type Retention struct {
	db   *DB
	opts RetentionOptions
}

// NewRetention returns the retention task over d.
func NewRetention(d *DB, opts RetentionOptions) *Retention {
	if opts.JobHistory <= 0 {
		opts.JobHistory = DefaultJobHistoryRetention
	}
	if opts.WebhookDeliveries <= 0 {
		opts.WebhookDeliveries = DefaultWebhookDeliveryRetention
	}
	if opts.Batch <= 0 {
		opts.Batch = DefaultPruneBatch
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = DefaultPruneMaxBatches
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Retention{db: d, opts: opts}
}

// PruneResult counts what a pass deleted.
type PruneResult struct {
	// Jobs counts the finished jobs deleted; their job_runs and job_events went with them.
	Jobs int64
	// Deliveries counts the webhook delivery records deleted.
	Deliveries int64
	// More is true when the pass stopped at its cap with rows still past their window.
	More bool
}

// Prune runs one pass. It deletes the oldest finished jobs (succeeded or failed) that ended
// before the job-history window and the oldest delivery records received before the delivery
// window, each in batches up to the cap. A queued or running job is never deleted, however old,
// and neither is a record inside its window.
func (r *Retention) Prune(ctx context.Context) (PruneResult, error) {
	now := r.opts.Now().UTC()
	var res PruneResult
	q := db.New(r.db.Writer)

	jobCutoff := formatTime(now.Add(-r.opts.JobHistory))
	jobsMore, err := batches(ctx, r.opts, &res.Jobs, func() (int64, error) {
		return q.PruneFinishedJobs(ctx, db.PruneFinishedJobsParams{Cutoff: nullString(jobCutoff), Batch: int64(r.opts.Batch)})
	})
	if err != nil {
		return res, fmt.Errorf("prune job history: %w", err)
	}
	deliveryCutoff := formatTime(now.Add(-r.opts.WebhookDeliveries))
	deliveriesMore, err := batches(ctx, r.opts, &res.Deliveries, func() (int64, error) {
		return q.PruneWebhookDeliveries(ctx, db.PruneWebhookDeliveriesParams{Cutoff: deliveryCutoff, Batch: int64(r.opts.Batch)})
	})
	if err != nil {
		return res, fmt.Errorf("prune webhook deliveries: %w", err)
	}
	res.More = jobsMore || deliveriesMore
	return res, nil
}

// batches calls del until it deletes fewer rows than a batch or the cap is reached, adding the
// rows to total. more is true when the cap ended it.
func batches(ctx context.Context, opts RetentionOptions, total *int64, del func() (int64, error)) (more bool, err error) {
	for range opts.MaxBatches {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		n, err := del()
		if err != nil {
			return false, err
		}
		*total += n
		if n < int64(opts.Batch) {
			return false, nil
		}
	}
	return true, nil
}

// Run prunes at once and then every RetentionInterval until ctx ends, so a restart never waits
// a day for its first pass. A pass that stopped at its cap is followed by the next after a
// minute instead. A failing pass is logged and the loop goes on.
func (r *Retention) Run(ctx context.Context) {
	for {
		wait := RetentionInterval
		res, err := r.Prune(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Error("retention pass failed", "err", err)
		case err == nil && (res.Jobs > 0 || res.Deliveries > 0):
			slog.Info("retention: deleted old rows", "jobs", res.Jobs, "webhook_deliveries", res.Deliveries)
		}
		if err == nil && res.More {
			wait = retentionResumeDelay
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
