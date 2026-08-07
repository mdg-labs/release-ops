-- name: CreateAuthToken :one
INSERT INTO auth_tokens (
  id,
  kind,
  email,
  token_hash,
  new_email,
  invited_by_user_id,
  expires_at,
  created_at
) VALUES (
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
  kind,
  email,
  token_hash,
  new_email,
  invited_by_user_id,
  expires_at,
  used_at,
  created_at;

-- name: GetAuthTokenByHash :one
SELECT
  id,
  kind,
  email,
  token_hash,
  new_email,
  invited_by_user_id,
  expires_at,
  used_at,
  created_at
FROM auth_tokens
WHERE token_hash = ?
LIMIT 1;

-- name: GetAuthTokenByID :one
SELECT
  id,
  kind,
  email,
  token_hash,
  new_email,
  invited_by_user_id,
  expires_at,
  used_at,
  created_at
FROM auth_tokens
WHERE id = ?
LIMIT 1;

-- name: MarkAuthTokenUsed :one
UPDATE auth_tokens
SET used_at = ?
WHERE id = ?
RETURNING
  id,
  kind,
  email,
  token_hash,
  new_email,
  invited_by_user_id,
  expires_at,
  used_at,
  created_at;

-- name: InvalidatePasswordResetTokensForEmail :exec
UPDATE auth_tokens
SET used_at = ?
WHERE kind = 'password_reset'
  AND email = ?
  AND used_at IS NULL;

-- name: ListPendingInvitations :many
SELECT
  id,
  email,
  expires_at,
  created_at,
  invited_by_user_id
FROM auth_tokens
WHERE kind = 'invitation'
  AND used_at IS NULL
  AND expires_at > ?
ORDER BY created_at DESC;

-- name: DeletePendingInvitation :execrows
DELETE FROM auth_tokens
WHERE id = ?
  AND kind = 'invitation'
  AND used_at IS NULL;
