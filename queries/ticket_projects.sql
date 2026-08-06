-- name: CreateTicketProject :one
INSERT INTO ticket_projects (
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
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
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
  created_at,
  updated_at;

-- name: UpsertTicketProject :one
INSERT INTO ticket_projects (
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
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
  on_open_ticket_policy = excluded.on_open_ticket_policy,
  updated_at = excluded.updated_at
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
  created_at,
  updated_at;

-- name: GetTicketProject :one
SELECT
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
  created_at,
  updated_at
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
  on_open_ticket_policy,
  created_at,
  updated_at
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
  on_open_ticket_policy,
  created_at,
  updated_at
FROM ticket_projects
WHERE integration_id = ?
ORDER BY name;

-- name: UpdateTicketProject :one
UPDATE ticket_projects
SET
  name = ?,
  create_config = ?,
  status_mapping = ?,
  on_open_ticket_policy = ?,
  updated_at = ?
WHERE id = ?
RETURNING
  id,
  integration_id,
  external_project_id,
  name,
  create_config,
  status_mapping,
  on_open_ticket_policy,
  created_at,
  updated_at;

-- name: DeleteTicketProject :exec
DELETE FROM ticket_projects
WHERE id = ?;

-- name: CountMonitoredReposByTicketProject :one
SELECT COUNT(*) AS count
FROM monitored_repos
WHERE ticket_project_id = ?;
