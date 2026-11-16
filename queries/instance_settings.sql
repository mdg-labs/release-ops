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
