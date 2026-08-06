-- name: CreateNotificationTarget :one
INSERT INTO notification_targets (
  id,
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at
) VALUES (
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
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at;

-- name: GetNotificationTarget :one
SELECT
  id,
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at
FROM notification_targets
WHERE id = ?
LIMIT 1;

-- name: ListNotificationTargets :many
SELECT
  id,
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at
FROM notification_targets
ORDER BY name;

-- name: ListEnabledNotificationTargets :many
SELECT
  id,
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at
FROM notification_targets
WHERE enabled = 1
ORDER BY name;

-- name: UpdateNotificationTarget :one
UPDATE notification_targets
SET
  name = ?,
  shoutrrr_url_encrypted = ?,
  events_json = ?,
  enabled = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  name,
  shoutrrr_url_encrypted,
  events_json,
  enabled,
  created_at,
  updated_at;

-- name: DeleteNotificationTarget :exec
DELETE FROM notification_targets
WHERE id = ?;
