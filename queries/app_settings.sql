-- name: EnsureAppSettings :exec
INSERT OR IGNORE INTO app_settings (
  id,
  invite_token_expiry_hours,
  password_reset_token_expiry_minutes,
  updated_at
)
VALUES (1, 168, 60, ?);

-- name: GetAppSettings :one
SELECT
  id,
  invite_token_expiry_hours,
  password_reset_token_expiry_minutes,
  updated_at
FROM app_settings
WHERE id = 1;

-- name: UpdateTokenExpirySettings :one
UPDATE app_settings
SET
  invite_token_expiry_hours = ?,
  password_reset_token_expiry_minutes = ?,
  updated_at = ?
WHERE id = 1
RETURNING
  id,
  invite_token_expiry_hours,
  password_reset_token_expiry_minutes,
  updated_at;
