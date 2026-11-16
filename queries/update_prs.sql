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

-- Closes an open row GitHub no longer knows; a row in any other state is left as it is, and so is
-- one whose last_seen_at is no longer the one the caller read, because a newer report from GitHub
-- stored since. The last_seen_at stays, so a later report from GitHub is ordered as it was before.
-- name: CloseGoneUpdatePR :execrows
UPDATE update_prs
SET state = 'closed', closed_at = ?, updated_at = ?
WHERE repository_id = ? AND number = ? AND state = 'open' AND last_seen_at = ?;

-- name: ListOpenUpdatePRs :many
SELECT number, last_seen_at FROM update_prs WHERE repository_id = ? AND state = 'open' ORDER BY number;

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

-- The update PRs a dashboard lists, with the PR that replaced one, its ticket and its
-- dependencies, all from one statement so the three belong to the same snapshot. Only the
-- repositories of the scope are read: all_repositories is 1 for the admin scope, and otherwise
-- a PR needs a repository in scope_ids, a JSON array of repository ids where an empty one
-- matches none. A removed repository, or one of a removed installation, has no PRs here. Empty
-- repository_id and update_type arguments filter nothing; sort is one of openedAt, -openedAt,
-- mergedAt and -mergedAt, computed into the two sort columns (an empty string stands for a
-- missing time) because sqlc does not rewrite arguments in an ORDER BY. The ticket's error
-- text is never selected, only whether it has one.
-- name: ListScopedUpdatePRs :many
SELECT p.id, p.repository_id, r.full_name AS repository_full_name, p.number, p.url, p.title, p.state,
       p.opened_at, p.merged_at, p.closed_at,
       sp.id AS superseded_by_id, sp.number AS superseded_by_number, sp.url AS superseded_by_url,
       t.state AS ticket_state, t.external_id AS ticket_external_id, t.external_url AS ticket_external_url,
       CAST(t.last_error IS NOT NULL OR t.pending_since IS NOT NULL AS INTEGER) AS ticket_has_error,
       CAST((
         SELECT json_group_array(json_object('name', d.name, 'from', d.from_version, 'to', d.to_version, 'updateType', d.update_type))
         FROM (
           SELECT name, from_version, to_version, update_type
           FROM update_pr_dependencies
           WHERE update_pr_id = p.id
           ORDER BY rowid
         ) d
       ) AS TEXT) AS dependencies,
       CAST(COALESCE(CASE WHEN sqlc.arg(sort) = 'openedAt' THEN p.opened_at WHEN sqlc.arg(sort) = 'mergedAt' THEN p.merged_at END, '') AS TEXT) AS sort_ascending,
       CAST(COALESCE(CASE WHEN sqlc.arg(sort) = '-openedAt' THEN p.opened_at WHEN sqlc.arg(sort) = '-mergedAt' THEN p.merged_at END, '') AS TEXT) AS sort_descending
FROM update_prs p
JOIN repositories r ON r.id = p.repository_id
JOIN installations i ON i.id = r.installation_id
LEFT JOIN update_prs sp ON sp.id = p.superseded_by_id
LEFT JOIN update_tickets t ON t.update_pr_id = p.id
WHERE r.removed_at IS NULL
  AND i.status <> 'removed'
  AND (sqlc.arg(all_repositories) = 1 OR p.repository_id IN (SELECT j.value FROM json_each(sqlc.arg(scope_ids)) j))
  AND p.state = sqlc.arg(state)
  AND (sqlc.arg(repository_id) = '' OR p.repository_id = sqlc.arg(repository_id))
  AND (sqlc.arg(update_type) = '' OR EXISTS (
    SELECT 1 FROM update_pr_dependencies d WHERE d.update_pr_id = p.id AND d.update_type = sqlc.arg(update_type)
  ))
ORDER BY sort_ascending ASC, sort_descending DESC, p.id
LIMIT sqlc.arg(page_size) OFFSET sqlc.arg(page_offset);

-- name: CountScopedUpdatePRs :one
SELECT COUNT(*)
FROM update_prs p
JOIN repositories r ON r.id = p.repository_id
JOIN installations i ON i.id = r.installation_id
WHERE r.removed_at IS NULL
  AND i.status <> 'removed'
  AND (sqlc.arg(all_repositories) = 1 OR p.repository_id IN (SELECT j.value FROM json_each(sqlc.arg(scope_ids)) j))
  AND p.state = sqlc.arg(state)
  AND (sqlc.arg(repository_id) = '' OR p.repository_id = sqlc.arg(repository_id))
  AND (sqlc.arg(update_type) = '' OR EXISTS (
    SELECT 1 FROM update_pr_dependencies d WHERE d.update_pr_id = p.id AND d.update_type = sqlc.arg(update_type)
  ));
