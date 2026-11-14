-- name: CreateGitHubApp :execrows
INSERT INTO github_app (
  id,
  app_id,
  slug,
  client_id,
  html_url,
  owner_login,
  owner_type,
  webhook_active,
  encrypted_private_key,
  encrypted_client_secret,
  encrypted_webhook_secret,
  key_id,
  created_at,
  updated_at
) VALUES (
  1,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?,
  ?
)
ON CONFLICT (id) DO NOTHING;

-- name: GetGitHubAppSlug :one
SELECT slug FROM github_app WHERE id = 1;

-- name: CountLiveInstallations :one
SELECT COUNT(*) FROM installations WHERE status <> 'removed';

-- name: GetGitHubAppSigningKey :one
SELECT app_id, encrypted_private_key FROM github_app WHERE id = 1;

-- name: GetGitHubAppWebhookSecret :one
SELECT encrypted_webhook_secret FROM github_app WHERE id = 1;

-- name: GetGitHubAppSignIn :one
SELECT client_id, encrypted_client_secret, owner_login, owner_type FROM github_app WHERE id = 1;
