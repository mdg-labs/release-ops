-- name: CreateIntegration :one
INSERT INTO integrations (
  id,
  kind,
  name,
  base_url,
  encrypted_payload,
  created_at,
  updated_at,
  is_default
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

-- name: ListByKind :many
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
WHERE kind = ?
ORDER BY name;

-- name: UpdateIntegration :one
UPDATE integrations
SET
  name = ?,
  base_url = ?,
  encrypted_payload = ?,
  is_default = ?,
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

-- name: ClearDefaultIntegrationForKind :exec
UPDATE integrations
SET is_default = 0
WHERE kind = ?
  AND is_default = 1
  AND id <> ?;

-- name: DeleteIntegration :exec
DELETE FROM integrations
WHERE id = ?;

-- name: CountIntegrationReferences :one
SELECT
  (
    SELECT COUNT(*)
    FROM monitored_repos
    WHERE source_integration_id = ?
  ) + (
    SELECT COUNT(*)
    FROM ticket_projects
    WHERE integration_id = ?
  ) AS count;
