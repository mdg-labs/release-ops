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
