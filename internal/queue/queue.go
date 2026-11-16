// Package queue defines the persistent job queue of the update engine (specs.html § 2.8), the
// wire types the app and the worker exchange over the internal listener (§ 2.1) and the
// backoff curve of failed attempts. The queue itself is owned by the app process, the only
// database writer; the SQLite implementation lives in internal/store. The worker packages
// import this package and never the store.
package queue

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

// Kind is the type of work a job asks for. v2.0 has one.
type Kind string

// KindRenovateRun is a Renovate run of one repository.
const KindRenovateRun Kind = "renovate_run"

// Priority orders claims: high before normal.
type Priority string

const (
	// PriorityHigh is for webhook triggers and admin "run now".
	PriorityHigh Priority = "high"
	// PriorityNormal is for the hourly safety net and the polling fallback.
	PriorityNormal Priority = "normal"
)

// Trigger is what enqueued a job (specs.html § 2.9).
type Trigger string

const (
	TriggerDashboardCheckbox Trigger = "dashboard_checkbox"
	TriggerPRCheckbox        Trigger = "pr_checkbox"
	TriggerPRClosed          Trigger = "pr_closed"
	TriggerPush              Trigger = "push"
	TriggerRepositoryAdded   Trigger = "repository_added"
	TriggerSchedule          Trigger = "schedule"
	TriggerPolling           Trigger = "polling"
	TriggerRunNow            Trigger = "run_now"
)

// Job states of the jobs table.
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

var (
	// ErrNoJob is returned by Claim when no job is eligible.
	ErrNoJob = errors.New("no job is claimable")
	// ErrLeaseLost is returned when the caller does not hold the job's lease: the job is
	// unknown, not running, or leased to another claim.
	ErrLeaseLost = errors.New("lease is not held for this job")
	// ErrRepositoryPaused is returned by Enqueue for a repository that is paused or removed:
	// a paused repository gets no job from any trigger (specs.html § 2.7).
	ErrRepositoryPaused = errors.New("repository is paused or removed")
	// ErrRepositoryNotFound is returned by Enqueue for a repository id that is not recorded.
	ErrRepositoryNotFound = errors.New("repository is not recorded")
)

const (
	// MaxMarkerBytes and MaxMarkers bound the markers one trigger stores with a job. Renovate's
	// markers are short names and branch names; anything longer is not one.
	MaxMarkerBytes = 256
	MaxMarkers     = 32
	// MaxMergedMarkerBytes bounds the markers of one job however many triggers coalesce into
	// it; together with MaxMarkers it is what a stored marker list may hold in total.
	MaxMergedMarkerBytes = MaxMarkers * MaxMarkerBytes
)

// EnqueueParams describes one trigger for a repository.
type EnqueueParams struct {
	RepositoryID string
	Kind         Kind
	Priority     Priority
	Trigger      Trigger
	// MatchedMarkers are the checkbox markers of a tick (specs.html § 2.9).
	MatchedMarkers []string
	// WebhookReceivedAt is when the triggering webhook arrived; nil for scheduled jobs.
	WebhookReceivedAt *time.Time
}

// EnqueueOutcome says what an enqueue did.
type EnqueueOutcome string

const (
	// EnqueueQueued created a new queued job.
	EnqueueQueued EnqueueOutcome = "queued"
	// EnqueueCoalesced added the trigger to the repository's queued job.
	EnqueueCoalesced EnqueueOutcome = "coalesced"
	// EnqueueRerunRequested flagged the repository's running job for one follow-up run.
	EnqueueRerunRequested EnqueueOutcome = "rerun_requested"
	// EnqueueDropped ignored a normal-priority trigger: the run in progress covers it.
	EnqueueDropped EnqueueOutcome = "dropped"
)

// EnqueueResult is the outcome of an enqueue and the job it landed on.
type EnqueueResult struct {
	Outcome EnqueueOutcome
	JobID   string
}

// ClaimedJob is a job leased to a worker. It is the body of a successful claim.
type ClaimedJob struct {
	ID                 string    `json:"id"`
	Kind               Kind      `json:"kind"`
	RepositoryID       string    `json:"repository_id"`
	RepositoryFullName string    `json:"repository_full_name"`
	Priority           Priority  `json:"priority"`
	Trigger            Trigger   `json:"trigger"`
	Attempt            int       `json:"attempt"`
	Slot               int       `json:"slot"`
	LeaseOwner         string    `json:"lease_owner"`
	LeaseExpiresAt     time.Time `json:"lease_expires_at"`
	// GitAuthor is the App's bot identity as Renovate takes it, "<slug>[bot] <<id>+<slug>[bot]@
	// users.noreply.github.com>"; empty while the bot user's id is not known. The worker has no
	// GitHub identity of its own, so the app sends it with the job.
	GitAuthor string `json:"git_author,omitempty"`
	// GlobalConfigRepo and GlobalConfigRef locate the global Renovate config and tools to
	// fetch at job start (owner/repo and a branch, tag or commit); empty when the instance
	// mounts them into the worker instead.
	GlobalConfigRepo string `json:"global_config_repo,omitempty"`
	GlobalConfigRef  string `json:"global_config_ref,omitempty"`
}

// HeldJob is a job as its lease owner holds it: running, leased to that owner, with the lease
// not yet expired.
type HeldJob struct {
	ID                 string
	RepositoryID       string
	RepositoryFullName string
	// InstallationID is the GitHub installation id of the repository's installation.
	InstallationID int64
}

// HeartbeatResult answers a heartbeat. Abort tells the worker to stop the job.
type HeartbeatResult struct {
	Abort  bool   `json:"abort"`
	Reason string `json:"reason,omitempty"`
}

// AbortLeaseLost is the Reason of an abort because the lease is no longer held.
const AbortLeaseLost = "lease_lost"

// Run report statuses of a job_runs row.
const (
	ReportOK         = "ok"
	ReportMissing    = "missing"
	ReportUnreadable = "unreadable"
)

// ErrorCodeRateLimited is the error code of an attempt that ended because GitHub's rate limit
// was exhausted, whether Renovate hit it or the app refused to start the run for it.
const ErrorCodeRateLimited = "rate_limited"

// Caps on what a run report stores.
const (
	// MaxLogExcerptBytes caps the stored log excerpt; the tail of the log is kept.
	MaxLogExcerptBytes = 64 << 10
	// MaxReportBytes caps the stored Renovate report.
	MaxReportBytes = 64 << 10
	// MaxErrorMessageBytes caps the stored failure message.
	MaxErrorMessageBytes = 1 << 10
)

// MaxGlobalConfigBytes caps the global config text a result carries: the size the worker
// accepts a config file at.
const MaxGlobalConfigBytes = 1 << 20

// Sources of a global Renovate config, as GlobalConfigCapture names them.
const (
	GlobalConfigSourceRepository = "repository"
	GlobalConfigSourceMounted    = "mounted"
)

// GlobalConfigCapture is the global Renovate config text an attempt ran with: the file as
// found, never parsed, with token-shaped strings already replaced.
type GlobalConfigCapture struct {
	// Source is GlobalConfigSourceRepository or GlobalConfigSourceMounted. The repository and
	// ref of the first are the instance's own configuration, which the app knows.
	Source  string `json:"source"`
	Content string `json:"content"`
}

// RunRecord is what the worker reports of one attempt; the app stores it as a job_runs row in
// the transaction that ends the attempt, and the global config it carries on the settings of
// the job's installation in the same one. Every string is redacted before it is sent and again
// before it is stored.
type RunRecord struct {
	// ExitCode is Renovate's exit status; nil when the process did not run to an exit.
	ExitCode *int `json:"exit_code,omitempty"`
	// ErrorCode and ErrorMessage describe a failed attempt: a short code and a short message.
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
	// ReportStatus is ReportOK, ReportMissing or ReportUnreadable.
	ReportStatus string `json:"report_status,omitempty"`
	// LogExcerpt is the redacted tail of Renovate's JSON log.
	LogExcerpt string `json:"log_excerpt,omitempty"`
	// Report is Renovate's report file as written, when it is valid JSON.
	Report string `json:"report,omitempty"`
	// GlobalConfig is the global config the attempt ran with, when it got as far as reading
	// one. Without it the stored copy stays as it is.
	GlobalConfig *GlobalConfigCapture `json:"global_config,omitempty"`
	// NoConfig marks an attempt Renovate skipped because the repository has no config
	// (specs.html section 2.7). It is not part of the wire record: the result's outcome says
	// so (OutcomeNoConfig), and the app sets it from there.
	NoConfig bool `json:"-"`
}

// FailParams describes a failed attempt.
type FailParams struct {
	// RetryAfter is the minimum delay an upstream asked for (Retry-After, a rate-limit
	// reset); zero when there is none.
	RetryAfter time.Duration
	// RateLimited marks an attempt that ended because GitHub or another upstream refused it for
	// a rate limit. Such an attempt is deferred with Config.RateLimitDelay and has
	// Config.RateLimitMaxAttempts attempts instead of Config.MaxAttempts: waiting out a limit
	// is not the job failing.
	RateLimited bool
	// Run is the attempt's record. Every attempt leaves a job_runs row: nil stores one holding
	// only the outcome, the times and the retry delay.
	Run *RunRecord
}

// FailOutcome says what became of a failed or expired attempt.
type FailOutcome string

const (
	// FailRequeued put the job back in the queue with a later run_after.
	FailRequeued FailOutcome = "requeued"
	// FailExhausted ended the job in the failed state: no attempts are left.
	FailExhausted FailOutcome = "failed"
)

// ReapResult counts what a reap pass did.
type ReapResult struct {
	Requeued int
	Failed   int
}

// WorkerContactWindow is how recent the last worker contact must be for the app to count a
// worker as connected, and for /readyz to answer ready. A connected worker has a claim
// long-poll open (up to DefaultClaimWait on the internal listener) or a job heartbeating
// every 30 seconds, so a healthy one is heard from well inside it.
const WorkerContactWindow = 90 * time.Second

// Stats is a snapshot of the eligible queue: the queued jobs whose run_after has passed.
type Stats struct {
	// Depth counts the eligible queued jobs per priority; both priorities are always present.
	Depth map[Priority]int
	// OldestAge is how long the oldest eligible job has been eligible; zero when none is.
	OldestAge time.Duration
}

// Queue is the job queue. The app owns the only implementation in v2.0 (SQLite); the
// interface keeps a Postgres one possible. Every method that names a lease takes the
// lease owner the claim returned and answers ErrLeaseLost when it no longer holds; a
// heartbeat reports that as an abort instead.
type Queue interface {
	// Enqueue records a trigger for a repository: one queued job per repository, a
	// high-priority trigger during a run flags the running job for one follow-up.
	Enqueue(ctx context.Context, p EnqueueParams) (EnqueueResult, error)
	// Claim leases the next eligible job, or returns ErrNoJob. It never blocks.
	Claim(ctx context.Context) (*ClaimedJob, error)
	// Heartbeat renews the lease and marks the job started on its first call.
	Heartbeat(ctx context.Context, jobID, leaseOwner string) (HeartbeatResult, error)
	// Complete ends the job as succeeded; a repeat for an already finished job is a no-op.
	Complete(ctx context.Context, jobID, leaseOwner string) error
	// CompleteRun is Complete that also stores the attempt's record in the same transaction.
	CompleteRun(ctx context.Context, jobID, leaseOwner string, run RunRecord) error
	// Fail ends the attempt: the job is requeued with backoff, or failed when no attempts
	// are left. A repeat for an already failed job is a no-op.
	Fail(ctx context.Context, jobID, leaseOwner string, p FailParams) (FailOutcome, error)
	// Release gives the lease back without using an attempt.
	Release(ctx context.Context, jobID, leaseOwner string) error
	// HeldJob returns the job the lease owner holds under a live lease, or ErrLeaseLost when
	// the job is unknown, not running, leased to another owner, past its lease expiry, or of
	// an installation that is not active. It changes nothing.
	HeldJob(ctx context.Context, jobID, leaseOwner string) (*HeldJob, error)
	// Reap requeues, or fails, every running job whose lease has expired.
	Reap(ctx context.Context) (ReapResult, error)
	// LastWorkerContact returns when the app last heard from a worker: a claim, a token
	// request, a heartbeat, a result or a release. ok is false before the first contact since
	// the process started: idle workers send no heartbeats, so the claim poll counts.
	LastWorkerContact(ctx context.Context) (at time.Time, ok bool, err error)
	// Stats reads the eligible queue.
	Stats(ctx context.Context) (Stats, error)
	// Changed returns a channel that is closed when the queue next changes in a way that
	// can make a job claimable. Take it before trying a claim, so no change is missed.
	Changed() <-chan struct{}
}

// Notifier is a broadcast wake-up: every waiter holding the current channel is released
// by the next Notify.
type Notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

// Changed returns the channel the next Notify closes.
func (n *Notifier) Changed() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch == nil {
		n.ch = make(chan struct{})
	}
	return n.ch
}

// Notify wakes every waiter.
func (n *Notifier) Notify() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch != nil {
		close(n.ch)
		n.ch = nil
	}
}

// Config holds the queue's numeric defaults (specs.html § 2.12 leaves their values to the
// first deployment).
type Config struct {
	// LeaseDuration is how long a claim or heartbeat keeps a lease alive.
	LeaseDuration time.Duration
	// MaxAttempts is the number of attempts a job gets before it ends failed.
	MaxAttempts int
	// BackoffBase is the delay after the first failed attempt; it doubles per attempt.
	BackoffBase time.Duration
	// BackoffMax caps the delay.
	BackoffMax time.Duration
	// BackoffJitter is the fraction of the delay the jitter spreads it by, in [0, 1).
	BackoffJitter float64
	// RateLimitMaxAttempts is the number of attempts a job gets when its attempts end rate
	// limited, before it ends failed.
	RateLimitMaxAttempts int
	// RateLimitMaxDelay caps the exponential backoff of rate-limited attempts: it is the one
	// hour an installation token lives. A delay an upstream asked for (Retry-After, the reset
	// time) is never shortened by it.
	RateLimitMaxDelay time.Duration
	// DefaultConcurrencyCap applies while instance_settings holds no row.
	DefaultConcurrencyCap int
}

// DefaultConfig returns the defaults.
func DefaultConfig() Config {
	return Config{
		LeaseDuration:         2 * time.Minute,
		MaxAttempts:           3,
		BackoffBase:           30 * time.Second,
		BackoffMax:            15 * time.Minute,
		BackoffJitter:         0.2,
		RateLimitMaxAttempts:  8,
		RateLimitMaxDelay:     time.Hour,
		DefaultConcurrencyCap: 2,
	}
}

// Backoff returns the delay before the retry that follows attemptsUsed failed attempts:
// BackoffBase doubled per attempt up to BackoffMax, spread by the jitter. rnd is a value
// in [0, 1); it maps to a factor in [1-jitter, 1+jitter].
func (c Config) Backoff(attemptsUsed int, rnd float64) time.Duration {
	d := c.BackoffBase
	for i := 1; i < attemptsUsed && d < c.BackoffMax; i++ {
		d *= 2
	}
	if d > c.BackoffMax {
		d = c.BackoffMax
	}
	factor := 1 - c.BackoffJitter + 2*c.BackoffJitter*rnd
	return time.Duration(float64(d) * factor)
}

// RateLimitDelay returns the delay before the retry that follows attemptsUsed attempts, the
// last of which was rate limited: BackoffBase doubled per attempt up to RateLimitMaxDelay, or
// retryAfter when that is longer, spread upward by the jitter so that jobs held back by the
// same limit do not all return at the reset. It is never shorter than retryAfter. rnd is a value
// in [0, 1).
func (c Config) RateLimitDelay(attemptsUsed int, retryAfter time.Duration, rnd float64) time.Duration {
	d := c.BackoffBase
	for i := 1; i < attemptsUsed && d < c.RateLimitMaxDelay; i++ {
		d *= 2
	}
	d = min(d, c.RateLimitMaxDelay)
	return ratelimit.Jittered(max(d, retryAfter), c.BackoffJitter, rnd)
}

// RunReaper reaps expired leases once at start and then every interval until ctx ends, so
// a job left running by a previous app process is reclaimed once its lease has expired.
// A failing pass is passed to onError and the loop goes on.
func RunReaper(ctx context.Context, q Queue, interval time.Duration, onError func(error)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := q.Reap(ctx); err != nil && ctx.Err() == nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
