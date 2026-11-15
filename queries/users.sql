-- A user who signs in with GitHub only has no email and no password_hash, so the queries of
-- the local-account code read both through COALESCE and keep plain strings. The writes return
-- only what they generate: sqlc cannot give a COALESCE in RETURNING a name.

-- name: CreateUser :one
INSERT INTO users (
  id,
  email,
  password_hash,
  created_at,
  updated_at
) VALUES (
  sqlc.arg(id),
  CAST(sqlc.arg(email) AS TEXT),
  CAST(sqlc.arg(password_hash) AS TEXT),
  sqlc.arg(created_at),
  sqlc.arg(updated_at)
)
RETURNING id, created_at, updated_at;

-- name: GetUserByEmail :one
SELECT
  id,
  CAST(COALESCE(email, '') AS TEXT) AS email,
  CAST(COALESCE(password_hash, '') AS TEXT) AS password_hash,
  created_at,
  updated_at
FROM users
WHERE lower(email) = lower(sqlc.arg(email))
LIMIT 1;

-- name: CountUsers :one
SELECT COUNT(*) AS count
FROM users;

-- name: ListUsers :many
SELECT
  id,
  CAST(email AS TEXT) AS email,
  created_at
FROM users
WHERE email IS NOT NULL
ORDER BY created_at ASC;

-- name: GetUserByID :one
SELECT
  id,
  CAST(COALESCE(email, '') AS TEXT) AS email,
  CAST(COALESCE(password_hash, '') AS TEXT) AS password_hash,
  created_at,
  updated_at
FROM users
WHERE id = ?
LIMIT 1;

-- name: UpdateUserEmail :one
UPDATE users
SET
  email = CAST(sqlc.arg(email) AS TEXT),
  updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING id, created_at, updated_at;

-- name: UpdateUserPassword :one
UPDATE users
SET
  password_hash = CAST(sqlc.arg(password_hash) AS TEXT),
  updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
RETURNING id, created_at, updated_at;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = ?;

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
