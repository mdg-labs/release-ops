-- The repositories of an installation as GitHub reports them. enabled is never written on
-- conflict, so a paused repository stays paused; removed_at is cleared, so a repository that
-- returns to the installation is the same row. An empty default_branch or an unknown
-- config_state in the incoming row keeps what is stored, because a webhook payload carries
-- neither. The WHERE leaves an unchanged row, and its updated_at, alone.
-- name: UpsertRepository :execrows
INSERT INTO repositories (
  id,
  installation_id,
  github_repository_id,
  full_name,
  default_branch,
  config_state,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (github_repository_id) DO UPDATE SET
  installation_id = excluded.installation_id,
  full_name = excluded.full_name,
  default_branch = CASE WHEN excluded.default_branch = '' THEN repositories.default_branch ELSE excluded.default_branch END,
  config_state = CASE WHEN excluded.config_state = 'unknown' THEN repositories.config_state ELSE excluded.config_state END,
  removed_at = NULL,
  updated_at = excluded.updated_at
WHERE repositories.installation_id <> excluded.installation_id
   OR repositories.full_name <> excluded.full_name
   OR (excluded.default_branch <> '' AND repositories.default_branch <> excluded.default_branch)
   OR (excluded.config_state <> 'unknown' AND repositories.config_state <> excluded.config_state)
   OR repositories.removed_at IS NOT NULL;

-- A repository that was renamed or transferred away may still hold the name another one now
-- has in the same installation; the name is released so the new holder can take it, and the
-- old row gets its own name back when it is listed.
-- name: ReleaseRepositoryName :execrows
UPDATE repositories
SET full_name = full_name || '~' || github_repository_id
WHERE installation_id = ? AND full_name = ? AND github_repository_id <> ?;

-- name: ListActiveRepositoryConfigStates :many
SELECT github_repository_id, config_state
FROM repositories
WHERE installation_id = ? AND removed_at IS NULL;

-- name: MarkRepositoryRemoved :execrows
UPDATE repositories
SET removed_at = ?, updated_at = ?
WHERE installation_id = ? AND github_repository_id = ? AND removed_at IS NULL;

-- name: MarkInstallationRepositoriesRemoved :execrows
UPDATE repositories
SET removed_at = ?, updated_at = ?
WHERE installation_id = ? AND removed_at IS NULL;

-- name: ListActiveRepositoryIDs :many
SELECT id, github_repository_id
FROM repositories
WHERE installation_id = ? AND removed_at IS NULL;

-- A repository an admin can act on: not removed, and of an installation that is not removed.
-- name: GetManageableRepository :one
SELECT r.id, r.full_name, r.default_branch, r.enabled, r.config_state, i.status AS installation_status
FROM repositories r
JOIN installations i ON i.id = r.installation_id
WHERE r.id = ?
  AND r.removed_at IS NULL
  AND i.status <> 'removed';

-- name: SetRepositoryEnabled :execrows
UPDATE repositories
SET enabled = sqlc.arg(enabled), updated_at = sqlc.arg(updated_at)
WHERE repositories.id = sqlc.arg(id)
  AND repositories.removed_at IS NULL
  AND repositories.installation_id IN (SELECT i.id FROM installations i WHERE i.status <> 'removed');

-- The repositories the hourly safety net enqueues (specs.html section 2.9): enabled, not removed, of an
-- active installation, not recorded as having no config, and without a queued or running job.
-- name: ListScheduledRunRepositoryIDs :many
SELECT r.id
FROM repositories r
JOIN installations i ON i.id = r.installation_id
WHERE r.enabled = 1
  AND r.removed_at IS NULL
  AND i.status = 'active'
  AND r.config_state <> 'missing'
  AND NOT EXISTS (
    SELECT 1 FROM jobs j
    WHERE j.repository_id = r.id AND j.state IN ('queued', 'running')
  )
ORDER BY r.full_name, r.id;
