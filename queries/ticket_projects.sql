-- name: CreateTicketProject :one
INSERT INTO ticket_projects (
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter
) VALUES (
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
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter;

-- name: UpsertTicketProject :one
INSERT INTO ticket_projects (
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at
) VALUES (
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
ON CONFLICT (integration_id, external_project_id) DO UPDATE SET
  name = excluded.name,
  create_config = excluded.create_config,
  status_mapping = excluded.status_mapping,
  content_templates = excluded.content_templates,
  updated_at = excluded.updated_at
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter;

-- name: GetTicketProject :one
SELECT
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter
FROM ticket_projects
WHERE id = ?
LIMIT 1;

-- name: ListTicketProjects :many
SELECT
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter
FROM ticket_projects
ORDER BY name;

-- name: ListTicketProjectsByIntegration :many
SELECT
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter
FROM ticket_projects
WHERE integration_id = ?
ORDER BY name;

-- name: UpdateTicketProject :one
UPDATE ticket_projects
SET
  name = ?,
  create_config = ?,
  status_mapping = ?,
  content_templates = ?,
  filter = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  content_templates,
  created_at,
  updated_at,
  filter;

-- name: DeleteTicketProject :exec
DELETE FROM ticket_projects
WHERE id = ?;

-- name: CountTicketProjectDefaultAndOverrideReferences :one
SELECT
  (SELECT COUNT(*) FROM repositories WHERE ticket_project_id = CAST(sqlc.arg(id) AS TEXT))
  + (SELECT COUNT(*) FROM installation_settings WHERE default_ticket_project_id = CAST(sqlc.arg(id) AS TEXT))
  AS referenced;
