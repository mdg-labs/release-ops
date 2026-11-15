-- name: GetUpdatePR :one
SELECT id, state, node_id, url, title, head_ref, head_sha, superseded_by_id, labels,
       opened_at, merged_at, closed_at, last_seen_at
FROM update_prs
WHERE repository_id = ? AND number = ?;

-- name: InsertUpdatePR :exec
INSERT INTO update_prs (
  id, repository_id, number, node_id, url, title, head_ref, head_sha, state, labels,
  opened_at, merged_at, closed_at, last_seen_at, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateUpdatePR :exec
UPDATE update_prs
SET node_id = ?, url = ?, title = ?, head_ref = ?, head_sha = ?, state = ?, superseded_by_id = ?,
    labels = ?, opened_at = ?, merged_at = ?, closed_at = ?, last_seen_at = ?, updated_at = ?
WHERE id = ?;

-- name: ListOpenUpdatePRNumbers :many
SELECT number FROM update_prs WHERE repository_id = ? AND state = 'open' ORDER BY number;

-- One row per repository that is not removed, with the number of its open update PRs.
-- name: CountOpenUpdatePRsByRepository :many
SELECT r.full_name, COUNT(p.id) AS open_count
FROM repositories r
LEFT JOIN update_prs p ON p.repository_id = r.id AND p.state = 'open'
WHERE r.removed_at IS NULL
GROUP BY r.id, r.full_name
ORDER BY r.full_name;

-- The repository a PR reconcile reads, with what it needs to call GitHub for it: not removed,
-- enabled and of an active installation.
-- name: GetReconcileRepository :one
SELECT r.id, r.full_name, i.github_installation_id
FROM repositories r
JOIN installations i ON i.id = r.installation_id
WHERE r.id = ?
  AND r.removed_at IS NULL
  AND r.enabled = 1
  AND i.status = 'active';

-- name: ListReconcileRepositoryIDs :many
SELECT r.id
FROM repositories r
JOIN installations i ON i.id = r.installation_id
WHERE r.removed_at IS NULL
  AND r.enabled = 1
  AND i.status = 'active'
ORDER BY r.full_name, r.id;
