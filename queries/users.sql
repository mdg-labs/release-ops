-- name: CreateUser :one
INSERT INTO users (
  id,
  email,
  password_hash,
  created_at,
  updated_at
) VALUES (
  ?,
  ?,
  ?,
  ?,
  ?
)
RETURNING id, email, password_hash, created_at, updated_at;

-- name: GetUserByEmail :one
SELECT
  id,
  email,
  password_hash,
  created_at,
  updated_at
FROM users
WHERE email = ?
LIMIT 1;

-- name: CountUsers :one
SELECT COUNT(*) AS count
FROM users;

-- name: ListUsers :many
SELECT
  id,
  email,
  created_at
FROM users
ORDER BY created_at ASC;

-- name: GetUserByID :one
SELECT
  id,
  email,
  password_hash,
  created_at,
  updated_at
FROM users
WHERE id = ?
LIMIT 1;

-- name: UpdateUserEmail :one
UPDATE users
SET
  email = ?,
  updated_at = ?
WHERE id = ?
RETURNING id, email, password_hash, created_at, updated_at;

-- name: UpdateUserPassword :one
UPDATE users
SET
  password_hash = ?,
  updated_at = ?
WHERE id = ?
RETURNING id, email, password_hash, created_at, updated_at;

-- name: DeleteUser :exec
DELETE FROM users
WHERE id = ?;
