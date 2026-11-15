-- name: CreateLoginLinkToken :exec
INSERT INTO login_link_tokens (id, token_hash, expires_at, created_at)
VALUES (?, ?, ?, ?);

-- name: ConsumeLoginLinkToken :execrows
UPDATE login_link_tokens
SET consumed_at = CAST(sqlc.arg(now) AS TEXT)
WHERE token_hash = sqlc.arg(token_hash)
  AND consumed_at IS NULL
  AND expires_at > CAST(sqlc.arg(now) AS TEXT);
