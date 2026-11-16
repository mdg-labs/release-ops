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

-- The installations an admin reads: the live one and a removed one kept for history, newest
-- first, with the count of their repositories that are not removed and their settings.
-- name: ListInstallationsForAdmin :many
SELECT i.id, i.account_login, i.account_type, i.status, i.repository_selection, i.permissions,
       i.rate_limit_remaining, i.rate_limit_limit, i.rate_limit_reset_at,
       s.default_ticket_project_id,
       (SELECT COUNT(*) FROM repositories r WHERE r.installation_id = i.id AND r.removed_at IS NULL) AS repository_count
FROM installations i
LEFT JOIN installation_settings s ON s.installation_id = i.id
ORDER BY i.created_at DESC, i.id;

-- name: GetInstallationStatus :one
SELECT status FROM installations WHERE id = ?;

-- The settings row may not exist yet, so the first write creates it.
-- name: UpsertInstallationDefaultTicketProject :exec
INSERT INTO installation_settings (installation_id, default_ticket_project_id, updated_at)
VALUES (?, ?, ?)
ON CONFLICT (installation_id) DO UPDATE SET
  default_ticket_project_id = excluded.default_ticket_project_id,
  updated_at = excluded.updated_at;

-- name: TicketProjectExists :one
SELECT COUNT(*) FROM ticket_projects WHERE id = ?;

-- The installation's shared GitHub rate-limit budget as last seen in a response (specs.html
-- section 2.8). The row's other columns are untouched: a budget is not a change of the
-- installation.
-- name: UpdateInstallationRateLimit :execrows
UPDATE installations
SET rate_limit_remaining = ?,
    rate_limit_limit = ?,
    rate_limit_reset_at = ?
WHERE github_installation_id = ?;

-- The stored budgets of the installations that are not removed, read at start so an exhausted
-- budget is still honoured after a restart.
-- name: ListInstallationRateLimits :many
SELECT github_installation_id, rate_limit_remaining, rate_limit_limit, rate_limit_reset_at
FROM installations
WHERE status <> 'removed'
  AND rate_limit_remaining IS NOT NULL
  AND rate_limit_reset_at IS NOT NULL
ORDER BY github_installation_id;

-- What the installation-level problems (rate_limited, permissions_lost; specs.html section 7.7)
-- are derived from: the stored state and permissions, the budget, and the end of the latest
-- finished attempt of the installation's repositories with its error code. The live installation
-- is listed before a removed one.
-- name: ListInstallationProblemInputs :many
SELECT i.id, i.status, i.permissions, i.rate_limit_remaining, i.rate_limit_reset_at, i.updated_at,
       (SELECT jr.error_code FROM job_runs jr
          JOIN jobs j ON j.id = jr.job_id
          JOIN repositories r ON r.id = j.repository_id
          WHERE r.installation_id = i.id AND jr.finished_at IS NOT NULL
          ORDER BY jr.finished_at DESC, jr.id DESC LIMIT 1) AS latest_attempt_error_code,
       (SELECT jr.finished_at FROM job_runs jr
          JOIN jobs j ON j.id = jr.job_id
          JOIN repositories r ON r.id = j.repository_id
          WHERE r.installation_id = i.id AND jr.finished_at IS NOT NULL
          ORDER BY jr.finished_at DESC, jr.id DESC LIMIT 1) AS latest_attempt_finished_at
FROM installations i
ORDER BY CASE WHEN i.status = 'removed' THEN 1 ELSE 0 END, i.created_at DESC, i.id;
