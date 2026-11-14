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
-- removed, the repository has no running job and the number of running jobs is below the limit
-- for its priority. The limit of a normal job leaves one slot free for high-priority jobs when
-- the cap is 2 or more. A job of a paused or removed repository stays queued.
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
  WHERE j.state = 'queued'
    AND j.run_after <= sqlc.arg(now)
    AND repo.enabled = 1
    AND repo.removed_at IS NULL
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
SET rerun_requested = 1
WHERE id = ?
  AND state = 'running';

-- A heartbeat renews the lease only while the job's repository is enabled and not removed; for
-- a paused or removed repository no row matches and the worker is told to abort.
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
    WHERE repo.enabled = 1
      AND repo.removed_at IS NULL
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

-- A requeued job keeps the high priority of a trigger that arrived while it ran: the retry
-- is the follow-up that trigger asked for.
-- name: RequeueJob :execrows
UPDATE jobs
SET state = 'queued',
    attempts = sqlc.arg(attempts),
    run_after = sqlc.arg(run_after),
    priority = CASE WHEN rerun_requested = 1 THEN 'high' ELSE priority END,
    rerun_requested = 0,
    lease_owner = NULL,
    lease_expires_at = NULL,
    last_heartbeat_at = NULL,
    started_at = NULL
WHERE id = sqlc.arg(id)
  AND state = 'running';

-- The job a lease owner holds right now: running, leased to that owner, lease not yet expired,
-- its repository enabled and not removed, and that repository's installation active.
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
