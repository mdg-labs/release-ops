-- name: CreateMonitoredRepo :one
INSERT INTO monitored_repos (
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?
)
RETURNING
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at;

-- name: GetMonitoredRepo :one
SELECT
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at
FROM monitored_repos
WHERE id = ?
LIMIT 1;

-- name: ListMonitoredRepos :many
SELECT
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at
FROM monitored_repos
ORDER BY project_path;

-- name: ListEnabled :many
SELECT
  mr.id,
  mr.source_kind,
  mr.project_path,
  mr.enabled,
  mr.include_prereleases,
  mr.source_integration_id,
  mr.ticket_project_id,
  mr.open_ticket_external_id,
  mr.open_ticket_tag,
  mr.last_known_tag,
  mr.last_release_published_at,
  mr.last_polled_at,
  mr.last_error,
  mr.created_at,
  mr.updated_at,
  COALESCE(GROUP_CONCAT(mrn.notification_target_id), '') AS notification_target_ids
FROM monitored_repos mr
LEFT JOIN monitored_repo_notifications mrn ON mrn.monitored_repo_id = mr.id
WHERE mr.enabled = 1
GROUP BY mr.id
ORDER BY mr.project_path;

-- name: UpdateMonitoredRepo :one
UPDATE monitored_repos
SET
  source_kind = ?,
  project_path = ?,
  enabled = ?,
  include_prereleases = ?,
  source_integration_id = ?,
  ticket_project_id = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at;

-- name: SetMonitoredRepoEnabled :one
UPDATE monitored_repos
SET
  enabled = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at;

-- name: UpdatePollState :one
UPDATE monitored_repos
SET
  open_ticket_external_id = ?,
  open_ticket_tag = ?,
  last_known_tag = ?,
  last_release_published_at = ?,
  last_polled_at = ?,
  last_error = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  source_kind,
  project_path,
  enabled,
  include_prereleases,
  source_integration_id,
  ticket_project_id,
  open_ticket_external_id,
  open_ticket_tag,
  last_known_tag,
  last_release_published_at,
  last_polled_at,
  last_error,
  created_at,
  updated_at;

-- name: DeleteMonitoredRepo :exec
DELETE FROM monitored_repos
WHERE id = ?;
