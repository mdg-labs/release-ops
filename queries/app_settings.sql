-- name: EnsureAppSettings :exec
INSERT OR IGNORE INTO app_settings (id, poll_interval_minutes, updated_at)
VALUES (1, 360, ?);

-- name: GetAppSettings :one
SELECT
  id,
  poll_interval_minutes,
  updated_at
FROM app_settings
WHERE id = 1;

-- name: UpdatePollInterval :one
UPDATE app_settings
SET
  poll_interval_minutes = ?,
  updated_at = ?
WHERE id = 1
RETURNING id, poll_interval_minutes, updated_at;
