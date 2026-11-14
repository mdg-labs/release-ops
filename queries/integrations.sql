-- name: CreateIntegration :one
INSERT INTO integrations (
  id,
  kind,
  name,
  base_url,
  encrypted_payload,
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
RETURNING id, kind, name, base_url, encrypted_payload, created_at, updated_at, is_default;

-- name: GetIntegration :one
SELECT
  id,
  kind,
  name,
  base_url,
  encrypted_payload,
  created_at,
  updated_at,
  is_default
FROM integrations
WHERE id = ?
LIMIT 1;

-- name: ListIntegrations :many
SELECT
  id,
  kind,
  name,
  base_url,
  encrypted_payload,
  created_at,
  updated_at,
  is_default
FROM integrations
ORDER BY name;

-- name: UpdateIntegration :one
UPDATE integrations
SET
  name = ?,
  base_url = ?,
  encrypted_payload = ?,
  updated_at = ?
WHERE id = ?
RETURNING id, kind, name, base_url, encrypted_payload, created_at, updated_at, is_default;

-- name: UpdateIntegrationName :one
UPDATE integrations
SET
  name = ?,
  updated_at = ?
WHERE id = ?
RETURNING id, kind, name, base_url, encrypted_payload, created_at, updated_at, is_default;

-- name: DeleteIntegration :exec
DELETE FROM integrations
WHERE id = ?;

-- name: CountIntegrationReferences :one
SELECT COUNT(*) AS count
  FROM ticket_projects
  WHERE integration_id = ?;
