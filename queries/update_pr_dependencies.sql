-- The newest Renovate report stored for a repository: a run that ended with a readable report.
-- name: GetLatestRenovateReport :one
SELECT r.full_name, jr.report
FROM job_runs jr
JOIN jobs j ON j.id = jr.job_id
JOIN repositories r ON r.id = j.repository_id
WHERE j.repository_id = ?
  AND jr.report_status = 'ok'
  AND jr.report IS NOT NULL
  AND jr.outcome <> 'running'
ORDER BY jr.started_at DESC, jr.attempt DESC
LIMIT 1;

-- name: GetUpdatePRForDependencies :one
SELECT id, number, state, last_seen_at, release_notes, report_sha
FROM update_prs
WHERE repository_id = ? AND number = ?;

-- name: ListUpdatePRDependencies :many
SELECT name, manager, datasource, from_version, to_version, update_type, source
FROM update_pr_dependencies
WHERE update_pr_id = ?
ORDER BY rowid;

-- name: DeleteUpdatePRDependencies :exec
DELETE FROM update_pr_dependencies WHERE update_pr_id = ?;

-- name: InsertUpdatePRDependency :exec
INSERT INTO update_pr_dependencies (
  id, update_pr_id, name, manager, datasource, from_version, to_version, update_type, source
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetUpdatePRNotes :exec
UPDATE update_prs
SET release_notes = ?, report_sha = ?, updated_at = ?
WHERE id = ?;

-- The closed, unmerged PRs of the repository that an open PR replaces: a lower number, and a
-- dependency of the same name whose manager and datasource do not differ where both are known.
-- name: ListSupersededByOpenPR :many
SELECT DISTINCT old.id, old.number
FROM update_prs old
JOIN update_pr_dependencies od ON od.update_pr_id = old.id
JOIN update_pr_dependencies nd ON nd.update_pr_id = sqlc.arg(open_id)
WHERE old.repository_id = sqlc.arg(repository_id)
  AND old.state = 'closed'
  AND old.number < sqlc.arg(open_number)
  AND od.name = nd.name
  AND (od.manager IS NULL OR nd.manager IS NULL OR od.manager = nd.manager)
  AND (od.datasource IS NULL OR nd.datasource IS NULL OR od.datasource = nd.datasource)
ORDER BY old.number;

-- The open PR that replaces a closed one: the lowest-numbered open PR of the repository with a
-- higher number and an overlapping dependency.
-- name: GetReplacementOfClosedPR :one
SELECT nw.id, nw.number
FROM update_prs nw
JOIN update_pr_dependencies nd ON nd.update_pr_id = nw.id
JOIN update_pr_dependencies od ON od.update_pr_id = sqlc.arg(closed_id)
WHERE nw.repository_id = sqlc.arg(repository_id)
  AND nw.state = 'open'
  AND nw.number > sqlc.arg(closed_number)
  AND od.name = nd.name
  AND (od.manager IS NULL OR nd.manager IS NULL OR od.manager = nd.manager)
  AND (od.datasource IS NULL OR nd.datasource IS NULL OR od.datasource = nd.datasource)
ORDER BY nw.number
LIMIT 1;

-- name: SupersedeUpdatePR :execrows
UPDATE update_prs
SET state = 'superseded', superseded_by_id = ?, updated_at = ?
WHERE id = ? AND state = 'closed';
