-- name: CreateAuthToken :one
INSERT INTO auth_tokens (
  id,
  kind,
  token_hash,
  expires_at,
  created_at
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?
)
RETURNING
  id,
  kind,
  token_hash,
  expires_at,
  used_at,
  created_at;

-- name: GetAuthTokenByHash :one
SELECT
  id,
  kind,
  token_hash,
  expires_at,
  used_at,
  created_at
FROM auth_tokens
WHERE token_hash = ?
LIMIT 1;

-- name: MarkAuthTokenUsed :one
UPDATE auth_tokens
SET used_at = ?
WHERE id = ?
  AND used_at IS NULL
RETURNING
  id,
  kind,
  token_hash,
  expires_at,
  used_at,
  created_at;

-- name: DeleteSetupTokens :exec
DELETE FROM auth_tokens
WHERE kind = 'setup';

-- name: GetUnusedSetupToken :one
SELECT
  id,
  kind,
  token_hash,
  expires_at,
  used_at,
  created_at
FROM auth_tokens
WHERE kind = 'setup'
  AND used_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: CountGitHubApp :one
SELECT COUNT(*) FROM github_app;
