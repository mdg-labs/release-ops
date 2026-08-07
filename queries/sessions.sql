-- name: SetSessionUserID :exec
UPDATE sessions
SET user_id = ?
WHERE token = ?;

-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions
WHERE user_id = ?;

-- name: DeleteOtherSessionsByUserID :exec
DELETE FROM sessions
WHERE user_id = ?
  AND token != ?;
