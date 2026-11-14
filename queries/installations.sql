-- name: ListLiveInstallationIDs :many
SELECT github_installation_id FROM installations WHERE status <> 'removed' ORDER BY github_installation_id;

-- A removed row with the same GitHub installation id is the only conflict the caller can
-- reach, because it records an installation only while no live one exists.
-- name: UpsertInstallation :exec
INSERT INTO installations (
  id,
  github_installation_id,
  account_id,
  account_login,
  account_type,
  status,
  repository_selection,
  permissions,
  created_at,
  updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (github_installation_id) DO UPDATE SET
  account_id = excluded.account_id,
  account_login = excluded.account_login,
  account_type = excluded.account_type,
  status = excluded.status,
  repository_selection = excluded.repository_selection,
  permissions = excluded.permissions,
  updated_at = excluded.updated_at
WHERE installations.status = 'removed';

-- name: GetInstallationByGitHubID :one
SELECT id, status FROM installations WHERE github_installation_id = ?;

-- Writes only when something differs, so a replayed event or an unchanged sync leaves
-- updated_at alone. A removed installation is never revived here; UpsertInstallation does that.
-- name: UpdateInstallation :execrows
UPDATE installations
SET account_id = sqlc.arg(account_id),
    account_login = sqlc.arg(account_login),
    account_type = sqlc.arg(account_type),
    status = sqlc.arg(status),
    repository_selection = sqlc.arg(repository_selection),
    permissions = sqlc.arg(permissions),
    updated_at = sqlc.arg(updated_at)
WHERE github_installation_id = sqlc.arg(github_installation_id)
  AND status <> 'removed'
  AND (account_id <> sqlc.arg(account_id)
    OR account_login <> sqlc.arg(account_login)
    OR account_type <> sqlc.arg(account_type)
    OR status <> sqlc.arg(status)
    OR repository_selection <> sqlc.arg(repository_selection)
    OR permissions <> sqlc.arg(permissions));

-- name: MarkInstallationRemoved :execrows
UPDATE installations
SET status = 'removed', updated_at = ?
WHERE github_installation_id = ? AND status <> 'removed';

-- The one installation that is not removed (idx_installations_one_not_removed).
-- name: GetLiveInstallation :one
SELECT id, github_installation_id, account_id, account_login, account_type, status
FROM installations
WHERE status <> 'removed';
