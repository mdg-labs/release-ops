-- name: InsertRun :one
INSERT INTO poll_runs (
  id,
  started_at,
  status
) VALUES (
  ?,
  ?,
  'running'
)
RETURNING
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json;

-- name: FinishRun :one
UPDATE poll_runs
SET
  finished_at = ?,
  status = ?,
  repos_checked = ?,
  tickets_created = ?,
  tickets_superseded = ?,
  errors_json = ?
WHERE id = ?
RETURNING
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json;

-- name: IncrementPollRunTicketsSuperseded :one
UPDATE poll_runs
SET tickets_superseded = tickets_superseded + 1
WHERE id = ?
RETURNING
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json;

-- name: IncrementPollRunTicketsCreated :one
UPDATE poll_runs
SET tickets_created = tickets_created + 1
WHERE id = ?
RETURNING
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json;

-- name: IncrementPollRunReposChecked :one
UPDATE poll_runs
SET repos_checked = repos_checked + 1
WHERE id = ?
RETURNING
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json;

-- name: GetPollRun :one
SELECT
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json
FROM poll_runs
WHERE id = ?
LIMIT 1;

-- name: ListPollRuns :many
SELECT
  id,
  started_at,
  finished_at,
  status,
  repos_checked,
  tickets_created,
  tickets_superseded,
  errors_json
FROM poll_runs
ORDER BY started_at DESC
LIMIT ?
OFFSET ?;

-- name: InsertEvent :one
INSERT INTO poll_run_events (
  id,
  poll_run_id,
  monitored_repo_id,
  action,
  detail,
  created_at
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?,
  ?
)
RETURNING
  id,
  poll_run_id,
  monitored_repo_id,
  action,
  detail,
  created_at;

-- name: ListPollRunEventsByRunID :many
SELECT
  id,
  poll_run_id,
  monitored_repo_id,
  action,
  detail,
  created_at
FROM poll_run_events
WHERE poll_run_id = ?
ORDER BY created_at;
