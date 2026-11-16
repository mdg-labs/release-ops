-- name: SetSessionUserID :exec
UPDATE sessions
SET user_id = ?
WHERE token = ?;

-- A repository that has left the installation drops out of the list at once; the row itself
-- goes with the session or with the repository.
-- name: ListSessionRepositoryIDs :many
SELECT sr.repository_id
FROM session_repositories sr
JOIN repositories r ON r.id = sr.repository_id
WHERE sr.session_token = ? AND r.removed_at IS NULL
ORDER BY sr.repository_id;

-- name: DeleteSessionRepositories :exec
DELETE FROM session_repositories WHERE session_token = ?;

-- name: AddSessionRepository :exec
INSERT INTO session_repositories (session_token, repository_id) VALUES (?, ?);
