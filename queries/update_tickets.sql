-- The update PR the ticket engine looks at, with the ticket project of its repository and of
-- its installation; the engine takes the repository's. A removed repository, or one of a
-- removed installation, is not found.
-- name: GetUpdatePRForTicket :one
SELECT p.id, p.repository_id, p.number, p.url, p.title, p.state, p.superseded_by_id, p.labels, p.automerge, p.release_notes,
       r.full_name, r.ticket_project_id AS repository_ticket_project_id,
       s.default_ticket_project_id AS default_ticket_project_id
FROM update_prs p
JOIN repositories r ON r.id = p.repository_id
JOIN installations i ON i.id = r.installation_id
LEFT JOIN installation_settings s ON s.installation_id = r.installation_id
WHERE p.id = ?
  AND r.removed_at IS NULL
  AND i.status <> 'removed';

-- name: GetUpdateTicketByPR :one
SELECT id, ticket_project_id, external_id, external_url, state, pending_since, synced_hash, attempts, next_attempt_at, last_error,
       target_state, hands_off_at
FROM update_tickets
WHERE update_pr_id = ?;

-- name: InsertUpdateTicket :exec
INSERT INTO update_tickets (
  id, update_pr_id, ticket_project_id, state, pending_since, attempts, next_attempt_at, last_error, last_error_at,
  created_at, updated_at
) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?);

-- Sets the pending marker of a row that has neither a ticket nor a marker. The project is
-- stored again because nothing was created under the old one.
-- name: ArmUpdateTicketMarker :execrows
UPDATE update_tickets
SET ticket_project_id = ?, pending_since = ?, updated_at = ?
WHERE id = ? AND external_id IS NULL AND pending_since IS NULL;

-- Records a failed attempt and when the next one is due; clear_marker drops the marker only
-- when the ticket system cannot have stored a ticket. A row that has its ticket is left alone.
-- name: FailUpdateTicket :execrows
UPDATE update_tickets
SET pending_since = CASE WHEN sqlc.arg(clear_marker) = 1 THEN NULL ELSE pending_since END,
    attempts = attempts + 1,
    next_attempt_at = sqlc.arg(next_attempt_at),
    last_error = sqlc.arg(last_error),
    last_error_at = sqlc.arg(at),
    updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND external_id IS NULL;

-- Records an error that is not a failed attempt: the marker stays and nothing is counted. An
-- unchanged message leaves the row alone.
-- name: NoteUpdateTicketError :execrows
UPDATE update_tickets
SET last_error = ?, last_error_at = ?, updated_at = ?
WHERE id = ? AND (last_error IS NULL OR last_error <> ?);

-- Stores the ticket and clears the marker in one write; only a row without a ticket takes it.
-- name: LinkUpdateTicket :execrows
UPDATE update_tickets
SET external_id = ?, external_url = ?, state = 'open', pending_since = NULL, synced_hash = ?,
    attempts = 0, next_attempt_at = NULL, last_error = NULL, last_error_at = NULL, updated_at = ?
WHERE id = ? AND external_id IS NULL;

-- The open update PRs that have dependencies and no ticket, no marker and no retry waiting; the
-- engine looks at each of them again, so a PR whose event was lost is still ticketed.
-- name: ListUpdatePRsToTicket :many
SELECT p.id
FROM update_prs p
JOIN repositories r ON r.id = p.repository_id
JOIN installations i ON i.id = r.installation_id
WHERE p.state = 'open'
  AND r.removed_at IS NULL
  AND i.status <> 'removed'
  AND EXISTS (SELECT 1 FROM update_pr_dependencies d WHERE d.update_pr_id = p.id)
  AND NOT EXISTS (
    SELECT 1 FROM update_tickets t
    WHERE t.update_pr_id = p.id
      AND (t.external_id IS NOT NULL OR t.pending_since IS NOT NULL OR t.next_attempt_at > sqlc.arg(now))
  )
ORDER BY p.created_at, p.id;

-- The update PRs whose ticket exists, was not changed outside Release Ops and may have something
-- to do: a transition that was recorded and not finished, an open ticket (a new version, or the
-- merge, close or replacement of its PR), or a won't-do ticket whose PR a later check found
-- replaced. A ticket in a retry that is not due is left out. The engine decides from the rows
-- what, if anything, to do; most of these answers are "nothing" and call no ticket system.
-- name: ListUpdatePRsToSync :many
SELECT p.id
FROM update_tickets t
JOIN update_prs p ON p.id = t.update_pr_id
JOIN repositories r ON r.id = p.repository_id
JOIN installations i ON i.id = r.installation_id
WHERE t.external_id IS NOT NULL
  AND t.hands_off_at IS NULL
  AND r.removed_at IS NULL
  AND i.status <> 'removed'
  AND (t.next_attempt_at IS NULL OR t.next_attempt_at <= sqlc.arg(now))
  AND (
    t.target_state IS NOT NULL
    OR t.state = 'open'
    OR (t.state = 'cancelled' AND p.state = 'superseded')
  )
ORDER BY p.updated_at, p.id;

-- The PRs that the PR with this id replaced.
-- name: ListUpdatePRsSupersededBy :many
SELECT id FROM update_prs WHERE superseded_by_id = ? ORDER BY number;

-- The repository of an update ticket.
-- name: GetUpdateTicketRepository :one
SELECT p.repository_id
FROM update_tickets t
JOIN update_prs p ON p.id = t.update_pr_id
WHERE t.id = ?;

-- Records the transition about to be made in the ticket system. Only a ticket that exists and
-- that nobody changed outside Release Ops takes it.
-- name: BeginUpdateTicketTransition :execrows
UPDATE update_tickets
SET target_state = ?, updated_at = ?
WHERE id = ? AND external_id IS NOT NULL AND hands_off_at IS NULL;

-- Stores the state a recorded transition led to and drops the record and the retry.
-- name: CompleteUpdateTicketTransition :execrows
UPDATE update_tickets
SET state = sqlc.arg(state), target_state = NULL, attempts = 0, next_attempt_at = NULL,
    last_error = NULL, last_error_at = NULL, updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND external_id IS NOT NULL AND hands_off_at IS NULL;

-- Stores the hash of the dependency list the ticket now shows and drops the retry.
-- name: MarkUpdateTicketSynced :execrows
UPDATE update_tickets
SET synced_hash = ?, attempts = 0, next_attempt_at = NULL, last_error = NULL, last_error_at = NULL, updated_at = ?
WHERE id = ? AND external_id IS NOT NULL AND hands_off_at IS NULL;

-- Records a failed operation on an existing ticket and when the next attempt is due. A recorded
-- transition stays.
-- name: FailUpdateTicketSync :execrows
UPDATE update_tickets
SET attempts = attempts + 1, next_attempt_at = sqlc.arg(next_attempt_at), last_error = sqlc.arg(last_error),
    last_error_at = sqlc.arg(at), updated_at = sqlc.arg(at)
WHERE id = sqlc.arg(id) AND external_id IS NOT NULL;

-- Marks a ticket as changed outside Release Ops: no operation touches it afterwards.
-- name: HandOffUpdateTicket :execrows
UPDATE update_tickets
SET hands_off_at = ?, target_state = NULL, attempts = 0, next_attempt_at = NULL,
    last_error = NULL, last_error_at = NULL, updated_at = ?
WHERE id = ? AND external_id IS NOT NULL AND hands_off_at IS NULL;

-- Drops the pending marker of a row that has no ticket, together with its error and retry, so
-- the next event of the PR may create one. Only the operator, who checked the ticket system,
-- asks for it.
-- name: ClearUpdateTicketMarker :execrows
UPDATE update_tickets
SET pending_since = NULL, attempts = 0, next_attempt_at = NULL, last_error = NULL, last_error_at = NULL, updated_at = ?
WHERE id = ? AND external_id IS NULL AND pending_since IS NOT NULL;

-- Drops the error of a failed create whose PR will not be ticketed. A row that has a ticket or
-- a marker keeps its error.
-- name: ClearUpdateTicketCreateError :execrows
UPDATE update_tickets
SET attempts = 0, next_attempt_at = NULL, last_error = NULL, last_error_at = NULL, updated_at = ?
WHERE id = ? AND external_id IS NULL AND pending_since IS NULL AND last_error IS NOT NULL;
