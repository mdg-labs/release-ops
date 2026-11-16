-- name: ListUsers :many
SELECT
  u.id,
  ai.login,
  u.created_at
FROM users u
JOIN auth_identities ai
  ON ai.user_id = u.id
 AND ai.provider = 'github'
ORDER BY u.created_at ASC, u.id ASC;

-- name: CreateGitHubUser :exec
INSERT INTO users (id, created_at, updated_at)
VALUES (?, ?, ?);

-- name: TouchUser :exec
UPDATE users
SET updated_at = ?
WHERE id = ?;

-- name: GetGitHubIdentity :one
SELECT id, user_id, provider, subject, login, created_at
FROM auth_identities
WHERE provider = 'github'
  AND subject = ?
LIMIT 1;

-- name: CreateGitHubIdentity :exec
INSERT INTO auth_identities (id, user_id, provider, subject, login, created_at)
VALUES (?, ?, 'github', ?, ?, ?);

-- name: UpdateGitHubIdentityLogin :exec
UPDATE auth_identities
SET login = ?
WHERE id = ?;

-- name: SetBootstrapAdmin :execrows
INSERT INTO instance_settings (id, bootstrap_admin_user_id, updated_at)
VALUES (1, ?, ?)
ON CONFLICT (id) DO UPDATE
SET bootstrap_admin_user_id = excluded.bootstrap_admin_user_id,
    updated_at = excluded.updated_at
WHERE instance_settings.bootstrap_admin_user_id IS NULL;

-- name: GetBootstrapAdminIdentity :one
SELECT ai.user_id, ai.subject, ai.login
FROM instance_settings s
JOIN auth_identities ai
  ON ai.user_id = s.bootstrap_admin_user_id
 AND ai.provider = 'github'
WHERE s.id = 1;
