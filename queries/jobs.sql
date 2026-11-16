-- name: GetConcurrencyCap :one
SELECT concurrency_cap
FROM instance_settings
WHERE id = 1;

-- name: ListRunningJobLeaseOwners :many
SELECT lease_owner
FROM jobs
WHERE state = 'running'
  AND lease_owner IS NOT NULL;

-- A queued job is claimable when its run_after has passed, its repository is enabled and not
-- removed, that repository's installation is active, the repository has no running job and the
-- number of running jobs is below the limit for its priority. The limit of a normal job leaves
-- one slot free for high-priority jobs when the cap is 2 or more. A job of a paused or removed
-- repository or of an installation that is not active stays queued. The repository and
-- installation condition is the one of HeartbeatJob and GetLeasedJob; change the three together.
-- A job of an installation whose rate-limit budget is exhausted (no request remaining before the
-- reset, specs.html section 2.8) stays queued until the reset has passed.
-- name: ClaimNextJob :one
UPDATE jobs
SET state = 'running',
    lease_owner = CAST(sqlc.arg(lease_owner) AS TEXT),
    lease_expires_at = CAST(sqlc.arg(lease_expires_at) AS TEXT),
    last_heartbeat_at = CAST(sqlc.arg(now) AS TEXT)
WHERE id = (
  SELECT j.id
  FROM jobs j
  JOIN repositories repo ON repo.id = j.repository_id
  JOIN installations inst ON inst.id = repo.installation_id
  WHERE j.state = 'queued'
    AND j.run_after <= sqlc.arg(now)
    AND repo.enabled = 1
    AND repo.removed_at IS NULL
    AND inst.status = 'active'
    AND NOT (
      inst.rate_limit_remaining IS NOT NULL
      AND inst.rate_limit_remaining <= 0
      AND inst.rate_limit_reset_at IS NOT NULL
      AND inst.rate_limit_reset_at > sqlc.arg(now)
    )
    AND NOT EXISTS (
      SELECT 1 FROM jobs r
      WHERE r.repository_id = j.repository_id AND r.state = 'running'
    )
    AND (SELECT COUNT(*) FROM jobs c WHERE c.state = 'running') <
      CASE j.priority WHEN 'high' THEN CAST(sqlc.arg(high_limit) AS INTEGER) ELSE CAST(sqlc.arg(normal_limit) AS INTEGER) END
  ORDER BY CASE j.priority WHEN 'high' THEN 0 ELSE 1 END, j.created_at, j.id
  LIMIT 1
)
RETURNING *;

-- name: GetJob :one
SELECT *
FROM jobs
WHERE id = ?;

-- name: GetRepositoryFullName :one
SELECT full_name
FROM repositories
WHERE id = ?;

-- name: GetQueuedJobForRepository :one
SELECT *
FROM jobs
WHERE repository_id = ?
  AND state = 'queued';

-- name: GetRunningJobForRepository :one
SELECT *
FROM jobs
WHERE repository_id = ?
  AND state = 'running';

-- name: InsertJob :exec
INSERT INTO jobs (
  id, kind, repository_id, priority, state, run_after, "trigger", matched_markers,
  webhook_received_at, created_at
) VALUES (?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?);

-- name: CoalesceQueuedJob :exec
UPDATE jobs
SET priority = ?,
    "trigger" = ?,
    matched_markers = ?,
    webhook_received_at = ?
WHERE id = ?
  AND state = 'queued';

-- name: SetJobRerunRequested :exec
UPDATE jobs
SET rerun_requested = 1,
    rerun_trigger = ?,
    rerun_markers = ?,
    rerun_webhook_received_at = ?
WHERE id = ?
  AND state = 'running';

-- A heartbeat renews the lease only while the job's repository is enabled and not removed and
-- its installation is active (the condition of ClaimNextJob and GetLeasedJob); otherwise no row
-- matches and the worker is told to abort.
-- name: HeartbeatJob :one
UPDATE jobs
SET last_heartbeat_at = sqlc.arg(now),
    lease_expires_at = CAST(sqlc.arg(lease_expires_at) AS TEXT),
    started_at = COALESCE(started_at, sqlc.arg(now))
WHERE jobs.id = sqlc.arg(id)
  AND jobs.state = 'running'
  AND jobs.lease_owner = sqlc.arg(lease_owner)
  AND jobs.repository_id IN (
    SELECT repo.id FROM repositories repo
    JOIN installations inst ON inst.id = repo.installation_id
    WHERE repo.enabled = 1
      AND repo.removed_at IS NULL
      AND inst.status = 'active'
  )
RETURNING id;

-- name: ListExpiredJobs :many
SELECT *
FROM jobs
WHERE state = 'running'
  AND lease_expires_at < ?
ORDER BY lease_expires_at, id;

-- name: FinishJob :execrows
UPDATE jobs
SET state = sqlc.arg(state),
    attempts = sqlc.arg(attempts),
    finished_at = CAST(sqlc.arg(now) AS TEXT)
WHERE id = sqlc.arg(id)
  AND state = 'running';

-- A requeued job is the follow-up of a trigger that arrived while it ran: the caller passes the
-- priority, trigger, markers and webhook arrival with that trigger merged in, and what the
-- mid-run triggers recorded is cleared.
-- name: RequeueJob :execrows
UPDATE jobs
SET state = 'queued',
    attempts = sqlc.arg(attempts),
    run_after = sqlc.arg(run_after),
    priority = sqlc.arg(priority),
    "trigger" = sqlc.arg(trigger),
    matched_markers = sqlc.arg(matched_markers),
    webhook_received_at = sqlc.arg(webhook_received_at),
    rerun_requested = 0,
    rerun_trigger = NULL,
    rerun_markers = NULL,
    rerun_webhook_received_at = NULL,
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    started_at = NULL
WHERE id = sqlc.arg(id)
  AND state = 'running';

-- The job a lease owner holds right now: running, leased to that owner, lease not yet expired,
-- its repository enabled and not removed, and that repository's installation active (the
-- condition of ClaimNextJob and HeartbeatJob).
-- name: GetLeasedJob :one
SELECT j.id, j.repository_id, r.full_name AS repository_full_name, i.github_installation_id
FROM jobs j
JOIN repositories r ON r.id = j.repository_id
JOIN installations i ON i.id = r.installation_id
WHERE j.id = sqlc.arg(id)
  AND j.state = 'running'
  AND j.lease_owner = CAST(sqlc.arg(lease_owner) AS TEXT)
  AND j.lease_expires_at > CAST(sqlc.arg(now) AS TEXT)
  AND r.enabled = 1
  AND r.removed_at IS NULL
  AND i.status = 'active';

-- The eligible queue per priority: queued jobs whose run_after has passed. oldest_eligible_at
-- is the time the oldest of them became eligible.
-- name: CountEligibleQueuedJobs :many
SELECT priority, COUNT(*) AS depth, CAST(MIN(run_after) AS TEXT) AS oldest_eligible_at
FROM jobs
WHERE state = 'queued'
  AND run_after <= ?
GROUP BY priority;

-- One row per attempt, written in the transaction that ends the attempt.
-- name: InsertJobRun :exec
INSERT INTO job_runs (
  id, job_id, attempt, started_at, finished_at, outcome, exit_code, error_code, error_message,
  retry_after, report_status, log_excerpt, report
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- The App's slug and the numeric id of its bot user, which Renovate's git author is made of.
-- name: GetBotIdentity :one
SELECT slug, bot_user_id
FROM github_app
WHERE id = 1;

-- A repository's state when a trigger asks for a job: no row means the repository is unknown.
-- name: GetRepositoryEnqueueState :one
SELECT enabled, removed_at
FROM repositories
WHERE id = ?;

-- Pausing a repository cancels its queued job; a running one is aborted at its next heartbeat.
-- A queued job that already ran an attempt is waiting for its retry: it ends as failed, so the
-- attempt's job_runs row and events are kept, as FinishJob ends an exhausted job.
-- name: FailQueuedJobsWithRunsForRepository :execrows
UPDATE jobs
SET state = 'failed',
    finished_at = CAST(sqlc.arg(now) AS TEXT)
WHERE repository_id = sqlc.arg(repository_id)
  AND state = 'queued'
  AND EXISTS (SELECT 1 FROM job_runs WHERE job_runs.job_id = jobs.id);

-- What is still queued once FailQueuedJobsWithRunsForRepository has run has no attempt history.
-- name: DeleteQueuedJobsForRepository :execrows
DELETE FROM jobs
WHERE repository_id = ?
  AND state = 'queued';

-- name: InsertJobEvent :exec
INSERT INTO job_events (id, job_id, job_run_id, at, kind, message)
VALUES (?, ?, ?, ?, ?, ?);

-- Retention (specs.html section 4.6): a job in a terminal state that finished before the cutoff is
-- deleted with its job_runs and job_events (the foreign keys cascade). A queued or running job is
-- never matched, whatever its age. The oldest go first and a batch is bounded, so a pass over a
-- long history takes a number of small deletes and never one large one.
-- name: PruneFinishedJobs :execrows
DELETE FROM jobs
WHERE id IN (
  SELECT old.id FROM jobs old
  WHERE old.state IN ('succeeded', 'failed')
    AND old.finished_at IS NOT NULL
    AND old.finished_at < sqlc.arg(cutoff)
  ORDER BY old.finished_at, old.id
  LIMIT sqlc.arg(batch)
);
