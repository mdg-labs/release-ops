package store

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	mathrand "math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store/db"
)

const timeLayout = "2006-01-02T15:04:05.000Z"

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// JobQueueOptions configures a JobQueue. Zero fields take their defaults.
type JobQueueOptions struct {
	Config queue.Config
	// Now is the clock; tests replace it.
	Now func() time.Time
	// Rand returns a value in [0, 1) for the backoff jitter.
	Rand func() float64
	// OnLeaseExpired is called after Reap ends an attempt whose lease expired, with the full
	// name of the job's repository. The attempt ended without a result from the worker.
	OnLeaseExpired func(repository string)
	// OnJobStarted is called after a job's first heartbeat of an attempt, when the job was
	// triggered by a webhook, with the time from the webhook's arrival to that heartbeat.
	OnJobStarted func(lag time.Duration)
}

// JobQueue is the SQLite implementation of queue.Queue over the jobs table. Every write
// runs in a transaction on the single writer connection, so a claim is one atomic
// UPDATE ... RETURNING that no concurrent claim can interleave with.
type JobQueue struct {
	db       *DB
	cfg      queue.Config
	now      func() time.Time
	rand     func() float64
	notifier queue.Notifier
	// onLeaseExpired is nil when nobody counts expired attempts.
	onLeaseExpired func(repository string)
	// onJobStarted is nil when nobody records the webhook lag.
	onJobStarted func(lag time.Duration)
	// lastContact is the unix-nanosecond time of the last worker contact; 0 before the first.
	lastContact atomic.Int64
}

var _ queue.Queue = (*JobQueue)(nil)

// NewJobQueue returns the queue over d's writer connection.
func NewJobQueue(d *DB, opts JobQueueOptions) *JobQueue {
	q := &JobQueue{db: d, cfg: opts.Config, now: opts.Now, rand: opts.Rand, onLeaseExpired: opts.OnLeaseExpired, onJobStarted: opts.OnJobStarted}
	if q.cfg == (queue.Config{}) {
		q.cfg = queue.DefaultConfig()
	}
	if q.now == nil {
		q.now = time.Now
	}
	if q.rand == nil {
		q.rand = mathrand.Float64
	}
	return q
}

// Changed implements queue.Queue.
func (q *JobQueue) Changed() <-chan struct{} {
	return q.notifier.Changed()
}

// touch records a worker contact. The internal listener authenticates a request before any
// queue method runs, so every call here comes from a worker holding the secret.
func (q *JobQueue) touch() {
	q.lastContact.Store(q.now().UnixNano())
}

// LastWorkerContact implements queue.Queue. The contact is held in memory, as an idle worker
// writes nothing: after the app restarts it reads "none" until the worker's next claim.
func (q *JobQueue) LastWorkerContact(context.Context) (time.Time, bool, error) {
	ns := q.lastContact.Load()
	if ns == 0 {
		return time.Time{}, false, nil
	}
	return time.Unix(0, ns).UTC(), true, nil
}

// Stats implements queue.Queue.
func (q *JobQueue) Stats(ctx context.Context) (queue.Stats, error) {
	now := q.now().UTC()
	rows, err := db.New(q.db.Reader).CountEligibleQueuedJobs(ctx, formatTime(now))
	if err != nil {
		return queue.Stats{}, fmt.Errorf("queue stats: %w", err)
	}
	stats := queue.Stats{Depth: map[queue.Priority]int{queue.PriorityHigh: 0, queue.PriorityNormal: 0}}
	for _, row := range rows {
		eligibleAt, err := time.Parse(timeLayout, row.OldestEligibleAt)
		if err != nil {
			return queue.Stats{}, fmt.Errorf("queue stats: run_after %q: %w", row.OldestEligibleAt, err)
		}
		stats.Depth[queue.Priority(row.Priority)] = int(row.Depth)
		stats.OldestAge = max(stats.OldestAge, now.Sub(eligibleAt))
	}
	return stats, nil
}

func (q *JobQueue) inTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := q.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// Enqueue implements queue.Queue.
func (q *JobQueue) Enqueue(ctx context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error) {
	p, err := validEnqueue(p)
	if err != nil {
		return queue.EnqueueResult{}, err
	}

	var res queue.EnqueueResult
	err = q.inTx(ctx, func(qtx *db.Queries) error {
		var err error
		res, err = q.enqueue(ctx, qtx, p)
		return err
	})
	if err != nil {
		return queue.EnqueueResult{}, fmt.Errorf("enqueue: %w", err)
	}
	q.wake(res)()
	return res, nil
}

// EnqueueTx is Enqueue inside the caller's transaction, which must be one on the writer
// connection: the job is recorded when the caller commits and not at all when it rolls back.
// A waiting claim is woken by the returned function, which the caller runs after the commit
// and never before it, as a claim woken earlier cannot see the job. The function is non-nil
// whenever the error is nil, and a no-op when the enqueue changed nothing a claim could take.
func (q *JobQueue) EnqueueTx(ctx context.Context, tx *sql.Tx, p queue.EnqueueParams) (queue.EnqueueResult, func(), error) {
	p, err := validEnqueue(p)
	if err != nil {
		return queue.EnqueueResult{}, nil, err
	}
	res, err := q.enqueue(ctx, db.New(tx), p)
	if err != nil {
		return queue.EnqueueResult{}, nil, fmt.Errorf("enqueue: %w", err)
	}
	return res, q.wake(res), nil
}

// wake returns the function that wakes the waiting claims for an enqueue with this result:
// only a queued or grown queued job can become claimable.
func (q *JobQueue) wake(res queue.EnqueueResult) func() {
	if res.Outcome == queue.EnqueueQueued || res.Outcome == queue.EnqueueCoalesced {
		return q.notifier.Notify
	}
	return func() {}
}

func validEnqueue(p queue.EnqueueParams) (queue.EnqueueParams, error) {
	if p.Kind == "" {
		p.Kind = queue.KindRenovateRun
	}
	if p.RepositoryID == "" {
		return p, errors.New("enqueue: repository id is required")
	}
	if p.Priority != queue.PriorityHigh && p.Priority != queue.PriorityNormal {
		return p, fmt.Errorf("enqueue: invalid priority %q", p.Priority)
	}
	return p, nil
}

// enqueue records one trigger through qtx. It writes at most once, after every read.
func (q *JobQueue) enqueue(ctx context.Context, qtx *db.Queries, p queue.EnqueueParams) (queue.EnqueueResult, error) {
	state, err := qtx.GetRepositoryEnqueueState(ctx, p.RepositoryID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return queue.EnqueueResult{}, queue.ErrRepositoryNotFound
	case err != nil:
		return queue.EnqueueResult{}, err
	case state.Enabled != 1 || state.RemovedAt.Valid:
		return queue.EnqueueResult{}, queue.ErrRepositoryPaused
	}

	queued, err := qtx.GetQueuedJobForRepository(ctx, p.RepositoryID)
	switch {
	case err == nil:
		return queue.EnqueueResult{Outcome: queue.EnqueueCoalesced, JobID: queued.ID}, qtx.CoalesceQueuedJob(ctx, coalesceParams(queued, p))
	case !errors.Is(err, sql.ErrNoRows):
		return queue.EnqueueResult{}, err
	}

	running, err := qtx.GetRunningJobForRepository(ctx, p.RepositoryID)
	switch {
	case err == nil:
		if p.Priority != queue.PriorityHigh {
			return queue.EnqueueResult{Outcome: queue.EnqueueDropped, JobID: running.ID}, nil
		}
		return queue.EnqueueResult{Outcome: queue.EnqueueRerunRequested, JobID: running.ID}, qtx.SetJobRerunRequested(ctx, rerunParams(running, p))
	case !errors.Is(err, sql.ErrNoRows):
		return queue.EnqueueResult{}, err
	}

	markers, err := marshalMarkers(p.MatchedMarkers)
	if err != nil {
		return queue.EnqueueResult{}, err
	}
	now := q.now()
	webhook := sql.NullString{}
	if p.WebhookReceivedAt != nil {
		webhook = nullString(formatTime(*p.WebhookReceivedAt))
	}
	id := newID()
	return queue.EnqueueResult{Outcome: queue.EnqueueQueued, JobID: id}, qtx.InsertJob(ctx, db.InsertJobParams{
		ID:                id,
		Kind:              string(p.Kind),
		RepositoryID:      p.RepositoryID,
		Priority:          string(p.Priority),
		RunAfter:          formatTime(now),
		Trigger:           string(p.Trigger),
		MatchedMarkers:    markers,
		WebhookReceivedAt: webhook,
		CreatedAt:         formatTime(now),
	})
}

// coalesceParams folds a trigger into the queued job: the higher priority wins and brings
// its trigger, the markers are the union, the earliest webhook arrival is kept.
func coalesceParams(queued db.Job, p queue.EnqueueParams) db.CoalesceQueuedJobParams {
	out := db.CoalesceQueuedJobParams{
		ID:                queued.ID,
		Priority:          queued.Priority,
		Trigger:           queued.Trigger,
		MatchedMarkers:    queued.MatchedMarkers,
		WebhookReceivedAt: queued.WebhookReceivedAt,
	}
	if p.Priority == queue.PriorityHigh && queued.Priority == string(queue.PriorityNormal) {
		out.Priority = string(queue.PriorityHigh)
		out.Trigger = string(p.Trigger)
	}
	if len(p.MatchedMarkers) > 0 {
		out.MatchedMarkers = mergeMarkers(queued.MatchedMarkers, p.MatchedMarkers)
	}
	if p.WebhookReceivedAt != nil {
		arrived := formatTime(*p.WebhookReceivedAt)
		if !out.WebhookReceivedAt.Valid || arrived < out.WebhookReceivedAt.String {
			out.WebhookReceivedAt = nullString(arrived)
		}
	}
	return out
}

// rerunParams records a high-priority trigger that arrives while the job runs. Several of them
// collapse into one follow-up: the latest trigger wins, the markers are the union and the
// earliest webhook arrival is kept.
func rerunParams(running db.Job, p queue.EnqueueParams) db.SetJobRerunRequestedParams {
	out := db.SetJobRerunRequestedParams{
		ID:                     running.ID,
		RerunTrigger:           nullString(string(p.Trigger)),
		RerunMarkers:           running.RerunMarkers,
		RerunWebhookReceivedAt: running.RerunWebhookReceivedAt,
	}
	if len(p.MatchedMarkers) > 0 {
		out.RerunMarkers = mergeMarkers(running.RerunMarkers, p.MatchedMarkers)
	}
	if p.WebhookReceivedAt != nil {
		out.RerunWebhookReceivedAt = earlierTime(running.RerunWebhookReceivedAt, nullString(formatTime(*p.WebhookReceivedAt)))
	}
	return out
}

// mergeMarkers is the union of existing and added as the JSON stored in a column; existing is
// kept if the union cannot be marshalled.
func mergeMarkers(existing sql.NullString, added []string) sql.NullString {
	raw, err := json.Marshal(unionMarkers(existing, added))
	if err != nil {
		return existing
	}
	return nullString(string(raw))
}

// earlierTime returns the earlier of two stored timestamps, which share one fixed-width layout;
// a NULL loses to a value.
func earlierTime(a, b sql.NullString) sql.NullString {
	if !a.Valid || (b.Valid && b.String < a.String) {
		return b
	}
	return a
}

// foldRerun merges what the triggers that arrived while job ran recorded into the trigger state
// of the job that carries on after it (the follow-up or the retry), the same way a coalesce
// into a queued job merges: the job becomes high priority, a normal-priority job takes the
// recorded trigger while a high-priority one keeps its own, the markers are the union and the
// earliest webhook arrival is kept. A job with no rerun requested, or one requested before the
// recorded columns existed, keeps what it has, with the priority raised if it was requested.
func foldRerun(job db.Job, priority, trigger string, markers, webhook sql.NullString) (string, string, sql.NullString, sql.NullString) {
	if job.RerunRequested != 1 {
		return priority, trigger, markers, webhook
	}
	if job.RerunTrigger.Valid && priority == string(queue.PriorityNormal) {
		trigger = job.RerunTrigger.String
	}
	if added := parseMarkers(job.RerunMarkers); len(added) > 0 {
		markers = mergeMarkers(markers, added)
	}
	return string(queue.PriorityHigh), trigger, markers, earlierTime(webhook, job.RerunWebhookReceivedAt)
}

func parseMarkers(raw sql.NullString) []string {
	var markers []string
	if raw.Valid {
		_ = json.Unmarshal([]byte(raw.String), &markers)
	}
	return markers
}

func marshalMarkers(markers []string) (sql.NullString, error) {
	if len(markers) == 0 {
		return sql.NullString{}, nil
	}
	raw, err := json.Marshal(markers)
	if err != nil {
		return sql.NullString{}, err
	}
	return nullString(string(raw)), nil
}

func unionMarkers(existing sql.NullString, added []string) []string {
	var merged []string
	seen := map[string]bool{}
	if existing.Valid {
		_ = json.Unmarshal([]byte(existing.String), &merged)
		for _, m := range merged {
			seen[m] = true
		}
	}
	for _, m := range added {
		if !seen[m] {
			seen[m] = true
			merged = append(merged, m)
		}
	}
	return merged
}

// Claim implements queue.Queue. The slot a claim names is the lowest one below the cap that
// no running job holds; it is kept as the prefix of the lease owner, so it needs no column.
func (q *JobQueue) Claim(ctx context.Context) (*queue.ClaimedJob, error) {
	q.touch()
	var claimed *queue.ClaimedJob
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		limit, err := q.concurrencyCap(ctx, qtx)
		if err != nil {
			return err
		}
		slot, err := freeSlot(ctx, qtx, limit)
		if err != nil {
			return err
		}

		now := q.now().UTC()
		expires := now.Add(q.cfg.LeaseDuration)
		owner, err := newLeaseOwner(slot)
		if err != nil {
			return err
		}
		normalLimit := limit
		if limit >= 2 {
			normalLimit = limit - 1
		}
		job, err := qtx.ClaimNextJob(ctx, db.ClaimNextJobParams{
			LeaseOwner:     owner,
			LeaseExpiresAt: formatTime(expires),
			Now:            formatTime(now),
			HighLimit:      int64(limit),
			NormalLimit:    int64(normalLimit),
		})
		if errors.Is(err, sql.ErrNoRows) {
			return queue.ErrNoJob
		}
		if err != nil {
			return err
		}
		fullName, err := qtx.GetRepositoryFullName(ctx, job.RepositoryID)
		if err != nil {
			return err
		}
		gitAuthor, err := botGitAuthor(ctx, qtx)
		if err != nil {
			return err
		}
		claimed = &queue.ClaimedJob{
			ID:                 job.ID,
			Kind:               queue.Kind(job.Kind),
			RepositoryID:       job.RepositoryID,
			RepositoryFullName: fullName,
			Priority:           queue.Priority(job.Priority),
			Trigger:            queue.Trigger(job.Trigger),
			Attempt:            int(job.Attempts) + 1,
			Slot:               slot,
			LeaseOwner:         owner,
			LeaseExpiresAt:     expires,
			GitAuthor:          gitAuthor,
		}
		return nil
	})
	if errors.Is(err, queue.ErrNoJob) {
		return nil, queue.ErrNoJob
	}
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	return claimed, nil
}

// botGitAuthor is the git author of the App's bot user as Renovate takes it. It is empty until
// the bot user's numeric id is known: the id, not the login, is the identity.
func botGitAuthor(ctx context.Context, qtx *db.Queries) (string, error) {
	bot, err := qtx.GetBotIdentity(ctx)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !bot.BotUserID.Valid) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	login := bot.Slug + "[bot]"
	return fmt.Sprintf("%s <%d+%s@users.noreply.github.com>", login, bot.BotUserID.Int64, login), nil
}

func (q *JobQueue) concurrencyCap(ctx context.Context, qtx *db.Queries) (int, error) {
	limit, err := qtx.GetConcurrencyCap(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return q.cfg.DefaultConcurrencyCap, nil
	}
	if err != nil {
		return 0, err
	}
	return int(limit), nil
}

// freeSlot returns the lowest slot below the cap that no running job holds. No free slot means
// the running jobs already fill the cap, so nothing is claimable.
func freeSlot(ctx context.Context, qtx *db.Queries, limit int) (int, error) {
	owners, err := qtx.ListRunningJobLeaseOwners(ctx)
	if err != nil {
		return 0, err
	}
	used := map[int]bool{}
	for _, o := range owners {
		if slot, ok := slotOf(o.String); ok {
			used[slot] = true
		}
	}
	for slot := 0; slot < limit; slot++ {
		if !used[slot] {
			return slot, nil
		}
	}
	return 0, queue.ErrNoJob
}

func newLeaseOwner(slot int) (string, error) {
	var token [16]byte
	if _, err := cryptorand.Read(token[:]); err != nil {
		return "", err
	}
	return strconv.Itoa(slot) + "." + hex.EncodeToString(token[:]), nil
}

func slotOf(leaseOwner string) (int, bool) {
	prefix, _, ok := strings.Cut(leaseOwner, ".")
	if !ok {
		return 0, false
	}
	slot, err := strconv.Atoi(prefix)
	return slot, err == nil
}

// HeldJob implements queue.Queue.
func (q *JobQueue) HeldJob(ctx context.Context, jobID, leaseOwner string) (*queue.HeldJob, error) {
	q.touch()
	if leaseOwner == "" {
		return nil, queue.ErrLeaseLost
	}
	row, err := db.New(q.db.Reader).GetLeasedJob(ctx, db.GetLeasedJobParams{
		ID:         jobID,
		LeaseOwner: leaseOwner,
		Now:        formatTime(q.now()),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, queue.ErrLeaseLost
	}
	if err != nil {
		return nil, fmt.Errorf("held job: %w", err)
	}
	return &queue.HeldJob{
		ID:                 row.ID,
		RepositoryID:       row.RepositoryID,
		RepositoryFullName: row.RepositoryFullName,
		InstallationID:     row.GithubInstallationID,
	}, nil
}

// Heartbeat implements queue.Queue. A heartbeat that matches no row answers with an abort: the
// lease was lost, or the job's repository was paused or removed. The wire carries one reason
// for both, since the spec defines no separate code.
func (q *JobQueue) Heartbeat(ctx context.Context, jobID, leaseOwner string) (queue.HeartbeatResult, error) {
	q.touch()
	now := q.now().UTC()
	var firstOfAttempt bool
	var webhookReceivedAt sql.NullString
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		job, err := qtx.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		firstOfAttempt = !job.StartedAt.Valid
		webhookReceivedAt = job.WebhookReceivedAt
		_, err = qtx.HeartbeatJob(ctx, db.HeartbeatJobParams{
			ID:             jobID,
			LeaseOwner:     nullString(leaseOwner),
			Now:            nullString(formatTime(now)),
			LeaseExpiresAt: formatTime(now.Add(q.cfg.LeaseDuration)),
		})
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return queue.HeartbeatResult{Abort: true, Reason: queue.AbortLeaseLost}, nil
	}
	if err != nil {
		return queue.HeartbeatResult{}, fmt.Errorf("heartbeat: %w", err)
	}
	if firstOfAttempt && webhookReceivedAt.Valid && q.onJobStarted != nil {
		arrived, err := time.Parse(timeLayout, webhookReceivedAt.String)
		if err != nil {
			return queue.HeartbeatResult{}, fmt.Errorf("heartbeat: webhook_received_at %q: %w", webhookReceivedAt.String, err)
		}
		q.onJobStarted(now.Sub(arrived))
	}
	return queue.HeartbeatResult{}, nil
}

// heldJob loads the job and checks the caller's lease. finished reports a job that already
// ended under this same lease owner, which makes a repeated result a no-op.
func heldJob(ctx context.Context, qtx *db.Queries, jobID, leaseOwner string) (job db.Job, finished bool, err error) {
	job, err = qtx.GetJob(ctx, jobID)
	if errors.Is(err, sql.ErrNoRows) {
		return job, false, queue.ErrLeaseLost
	}
	if err != nil {
		return job, false, err
	}
	if leaseOwner == "" || !job.LeaseOwner.Valid || job.LeaseOwner.String != leaseOwner {
		return job, false, queue.ErrLeaseLost
	}
	switch job.State {
	case queue.StateRunning:
		return job, false, nil
	case queue.StateSucceeded, queue.StateFailed:
		return job, true, nil
	default:
		return job, false, queue.ErrLeaseLost
	}
}

// Complete implements queue.Queue.
func (q *JobQueue) Complete(ctx context.Context, jobID, leaseOwner string) error {
	return q.complete(ctx, jobID, leaseOwner, nil)
}

// CompleteRun implements queue.Queue.
func (q *JobQueue) CompleteRun(ctx context.Context, jobID, leaseOwner string, run queue.RunRecord) error {
	return q.complete(ctx, jobID, leaseOwner, &run)
}

func (q *JobQueue) complete(ctx context.Context, jobID, leaseOwner string, run *queue.RunRecord) error {
	q.touch()
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		job, finished, err := heldJob(ctx, qtx, jobID, leaseOwner)
		if err != nil {
			return err
		}
		if finished {
			if job.State == queue.StateSucceeded {
				return nil
			}
			return queue.ErrLeaseLost
		}
		_, err = q.endAttempt(ctx, qtx, job, attemptSucceeded, 0, run)
		return err
	})
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	q.notifier.Notify()
	return nil
}

// Fail implements queue.Queue.
func (q *JobQueue) Fail(ctx context.Context, jobID, leaseOwner string, p queue.FailParams) (queue.FailOutcome, error) {
	q.touch()
	var outcome queue.FailOutcome
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		job, finished, err := heldJob(ctx, qtx, jobID, leaseOwner)
		if err != nil {
			return err
		}
		if finished {
			if job.State == queue.StateFailed {
				outcome = queue.FailExhausted
				return nil
			}
			return queue.ErrLeaseLost
		}
		outcome, err = q.endAttempt(ctx, qtx, job, attemptFailed, p.RetryAfter, p.Run)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("fail: %w", err)
	}
	q.notifier.Notify()
	return outcome, nil
}

// Release implements queue.Queue.
func (q *JobQueue) Release(ctx context.Context, jobID, leaseOwner string) error {
	q.touch()
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		job, finished, err := heldJob(ctx, qtx, jobID, leaseOwner)
		if err != nil {
			return err
		}
		if finished {
			return queue.ErrLeaseLost
		}
		return requeue(ctx, qtx, job, job.Attempts, q.now())
	})
	if err != nil {
		return fmt.Errorf("release: %w", err)
	}
	q.notifier.Notify()
	return nil
}

// Reap implements queue.Queue.
func (q *JobQueue) Reap(ctx context.Context) (queue.ReapResult, error) {
	var res queue.ReapResult
	var expiredRepos []string
	err := q.inTx(ctx, func(qtx *db.Queries) error {
		expired, err := qtx.ListExpiredJobs(ctx, nullString(formatTime(q.now())))
		if err != nil {
			return err
		}
		expiredRepos = expiredRepos[:0]
		for _, job := range expired {
			fullName, err := qtx.GetRepositoryFullName(ctx, job.RepositoryID)
			if err != nil {
				return err
			}
			expiredRepos = append(expiredRepos, fullName)
			outcome, err := q.endAttempt(ctx, qtx, job, attemptFailed, 0, nil)
			if err != nil {
				return err
			}
			if outcome == queue.FailRequeued {
				res.Requeued++
			} else {
				res.Failed++
			}
		}
		return nil
	})
	if err != nil {
		return queue.ReapResult{}, fmt.Errorf("reap: %w", err)
	}
	if q.onLeaseExpired != nil {
		for _, repository := range expiredRepos {
			q.onLeaseExpired(repository)
		}
	}
	if res.Requeued+res.Failed > 0 {
		q.notifier.Notify()
	}
	return res, nil
}

type attemptResult int

const (
	attemptSucceeded attemptResult = iota
	attemptFailed
)

// endAttempt ends the running job's attempt and counts it. A success or an exhausted job is
// terminal and enqueues the follow-up a mid-run trigger asked for; any other failure puts
// the job back with a backoff, and that retry is the follow-up. A run record is stored as the
// attempt's job_runs row in the same transaction.
func (q *JobQueue) endAttempt(ctx context.Context, qtx *db.Queries, job db.Job, result attemptResult, retryAfter time.Duration, run *queue.RunRecord) (queue.FailOutcome, error) {
	now := q.now().UTC()
	used := job.Attempts + 1

	state := queue.StateSucceeded
	outcome := queue.FailOutcome("")
	switch {
	case result == attemptSucceeded:
	case int(used) >= q.cfg.MaxAttempts:
		state = queue.StateFailed
		outcome = queue.FailExhausted
	default:
		delay := max(q.cfg.Backoff(int(used), q.rand()), retryAfter)
		if err := requeue(ctx, qtx, job, used, now.Add(delay)); err != nil {
			return "", err
		}
		if err := insertRun(ctx, qtx, job, used, queue.OutcomeFailed, retryAfter, run, now); err != nil {
			return "", err
		}
		return queue.FailRequeued, nil
	}

	n, err := qtx.FinishJob(ctx, db.FinishJobParams{ID: job.ID, State: state, Attempts: used, Now: formatTime(now)})
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", queue.ErrLeaseLost
	}
	runOutcome := queue.OutcomeSucceeded
	if result == attemptFailed {
		runOutcome = queue.OutcomeFailed
	}
	if err := insertRun(ctx, qtx, job, used, runOutcome, retryAfter, run, now); err != nil {
		return "", err
	}
	if job.RerunRequested == 1 {
		if err := q.enqueueFollowUp(ctx, qtx, job, now); err != nil {
			return "", err
		}
	}
	return outcome, nil
}

// insertRun stores the attempt's record as its job_runs row; without a record nothing is stored.
func insertRun(ctx context.Context, qtx *db.Queries, job db.Job, attempt int64, outcome string, retryAfter time.Duration, run *queue.RunRecord, now time.Time) error {
	if run == nil {
		return nil
	}
	row := db.InsertJobRunParams{
		ID:           newID(),
		JobID:        job.ID,
		Attempt:      attempt,
		StartedAt:    formatTime(now),
		FinishedAt:   nullString(formatTime(now)),
		Outcome:      outcome,
		ErrorCode:    nullString(run.ErrorCode),
		ErrorMessage: nullString(run.ErrorMessage),
		ReportStatus: nullString(run.ReportStatus),
		LogExcerpt:   nullString(run.LogExcerpt),
		Report:       nullString(run.Report),
	}
	if job.StartedAt.Valid {
		row.StartedAt = job.StartedAt.String
	}
	if run.ExitCode != nil {
		row.ExitCode = sql.NullInt64{Int64: int64(*run.ExitCode), Valid: true}
	}
	if retryAfter > 0 {
		row.RetryAfter = nullString(strconv.FormatInt(int64(retryAfter/time.Second), 10))
	}
	return qtx.InsertJobRun(ctx, row)
}

// requeue puts the running job back in the queue. When a trigger arrived while it ran, the job
// is the follow-up that trigger asked for and takes what the trigger recorded.
func requeue(ctx context.Context, qtx *db.Queries, job db.Job, attempts int64, runAfter time.Time) error {
	priority, trigger, markers, webhook := foldRerun(job, job.Priority, job.Trigger, job.MatchedMarkers, job.WebhookReceivedAt)
	n, err := qtx.RequeueJob(ctx, db.RequeueJobParams{
		ID:                job.ID,
		Attempts:          attempts,
		RunAfter:          formatTime(runAfter),
		Priority:          priority,
		Trigger:           trigger,
		MatchedMarkers:    markers,
		WebhookReceivedAt: webhook,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		return queue.ErrLeaseLost
	}
	return nil
}

// enqueueFollowUp queues the one high-priority job that follows a run during which a
// high-priority trigger arrived. It carries what those triggers recorded, and the finished
// job's own trigger and markers when the trigger was requested before the columns existed.
func (q *JobQueue) enqueueFollowUp(ctx context.Context, qtx *db.Queries, job db.Job, now time.Time) error {
	queued, err := qtx.GetQueuedJobForRepository(ctx, job.RepositoryID)
	switch {
	case err == nil:
		priority, trigger, markers, webhook := foldRerun(job, queued.Priority, queued.Trigger, queued.MatchedMarkers, queued.WebhookReceivedAt)
		return qtx.CoalesceQueuedJob(ctx, db.CoalesceQueuedJobParams{
			ID:                queued.ID,
			Priority:          priority,
			Trigger:           trigger,
			MatchedMarkers:    markers,
			WebhookReceivedAt: webhook,
		})
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	follow := db.InsertJobParams{
		ID:           newID(),
		Kind:         job.Kind,
		RepositoryID: job.RepositoryID,
		Priority:     string(queue.PriorityHigh),
		RunAfter:     formatTime(now),
		Trigger:      job.Trigger,
		CreatedAt:    formatTime(now),
	}
	if job.RerunTrigger.Valid {
		follow.Trigger = job.RerunTrigger.String
		follow.MatchedMarkers = job.RerunMarkers
		follow.WebhookReceivedAt = job.RerunWebhookReceivedAt
	} else {
		follow.MatchedMarkers = job.MatchedMarkers
	}
	return qtx.InsertJob(ctx, follow)
}
