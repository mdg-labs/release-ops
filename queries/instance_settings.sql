-- The row may not exist yet, so the first write creates it; a write never touches the
-- bootstrap admin of an existing row.
-- name: UpsertConcurrencyCap :exec
INSERT INTO instance_settings (id, concurrency_cap, updated_at)
VALUES (1, ?, ?)
ON CONFLICT (id) DO UPDATE
SET concurrency_cap = excluded.concurrency_cap,
    updated_at = excluded.updated_at;

-- The global Renovate config as the worker last used it for the installation that is not
-- removed.
-- name: GetLiveInstallationGlobalConfig :one
SELECT s.global_config_content, s.global_config_source, s.global_config_captured_at
FROM installation_settings s
JOIN installations i ON i.id = s.installation_id
WHERE i.status <> 'removed'
LIMIT 1;

-- Stores the global Renovate config a worker used for a job, on the settings row of the
-- installation of the job's own repository: the installation is read from the app's rows,
-- never from the worker. The row may not exist yet, so the first capture creates it; the
-- row's other columns are untouched.
-- name: CaptureJobGlobalConfig :exec
INSERT INTO installation_settings (installation_id, global_config_content, global_config_source, global_config_captured_at, updated_at)
SELECT r.installation_id, CAST(sqlc.arg(content) AS TEXT), CAST(sqlc.arg(source) AS TEXT), CAST(sqlc.arg(captured_at) AS TEXT), CAST(sqlc.arg(captured_at) AS TEXT)
FROM repositories r
WHERE r.id = sqlc.arg(repository_id)
ON CONFLICT (installation_id) DO UPDATE SET
  global_config_content = excluded.global_config_content,
  global_config_source = excluded.global_config_source,
  global_config_captured_at = excluded.global_config_captured_at,
  updated_at = excluded.updated_at;
