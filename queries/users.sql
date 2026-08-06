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
