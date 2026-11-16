package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

var (
	// ErrUpdatePRNotFound is returned for an update PR that is not stored, or whose repository
	// was removed from its installation.
	ErrUpdatePRNotFound = errors.New("update PR not found")
	// ErrTicketExists is returned by BeginCreate when the PR already has its ticket.
	ErrTicketExists = errors.New("the update PR already has a ticket")
	// ErrCreateUnconfirmed is returned by BeginCreate when an earlier create left its pending
	// marker: a ticket may exist that Release Ops did not store.
	ErrCreateUnconfirmed = errors.New("an earlier ticket creation for the update PR is unconfirmed")
	// ErrUpdateTicketNotFound is returned for an update ticket that is not stored.
	ErrUpdateTicketNotFound = errors.New("update ticket not found")
	// ErrNoPendingMarker is returned by ClearMarker for a row with no pending marker to clear: it
	// has its ticket, or its marker was cleared already.
	ErrNoPendingMarker = errors.New("the update ticket has no pending marker")
	// ErrTicketNotSyncable is returned when a write to an existing ticket finds no row it may
	// change: the ticket is gone, has no external id, or was handed off.
	ErrTicketNotSyncable = errors.New("the update ticket cannot take this change")
)

// Update ticket states (db/schema.sql update_tickets.state).
const (
	TicketStateOpen       = "open"
	TicketStateDone       = "done"
	TicketStateCancelled  = "cancelled"
	TicketStateSuperseded = "superseded"
)

// TicketCandidate is an update PR as the ticket engine decides on it.
type TicketCandidate struct {
	ID           string
	RepositoryID string
	// FullName is the repository's owner/name.
	FullName string
	// GitHubInstallationID is GitHub's id of the installation the repository belongs to.
	GitHubInstallationID int64
	Number               int64
	URL                  string
	Title                string
	// State is open, merged, closed or superseded.
	State  string
	Labels []string
	// Automerge is nil while it is unknown, which counts as not automerged.
	Automerge    *bool
	ReleaseNotes string
	// SupersededByID is the PR that replaced this one; empty while none did.
	SupersededByID string
	// TicketProjectID is the repository's override, else the installation's default; empty when
	// neither is set.
	TicketProjectID string
	Dependencies    []UpdatePRDependency
	// Ticket is the PR's update_tickets row, nil while it has none.
	Ticket *UpdateTicket
}

// UpdateTicket is the update_tickets row of a PR, as far as the ticket engine reads it.
type UpdateTicket struct {
	ID              string
	TicketProjectID string
	// Created reports that the ticket is stored: its external id is known.
	Created bool
	// ExternalID and ExternalURL are the ticket's link; empty until Created.
	ExternalID  string
	ExternalURL string
	// State is the last state Release Ops put the ticket in: pending, open, done, cancelled or
	// superseded.
	State string
	// TargetState is a transition recorded before the ticket system was called and not yet
	// followed by State; empty when none is.
	TargetState string
	// SyncedHash is the hash of the dependency list the ticket content was last rendered for.
	SyncedHash string
	// HandsOff reports that the ticket was changed outside Release Ops, so no operation
	// touches it.
	HandsOff bool
	// Unconfirmed reports the pending marker: a create was started and its ticket never stored.
	Unconfirmed bool
	Attempts    int64
	// NextAttemptAt is when the next attempt may run; zero when none is waiting.
	NextAttemptAt time.Time
	// LastError is the error shown in the Problems view; empty when none.
	LastError string
	// PRCommentID is the id of the bot comment on the PR; 0 while none is stored.
	PRCommentID int64
	// PRCommentHash is the hash of the body last written to that comment; empty while the
	// comment is due for a write.
	PRCommentHash string
}

// PRCommentErrorPrefix starts the error recorded for a PR comment that could not be written, so
// it is told from the error of a ticket operation (queries/update_tickets.sql matches it).
const PRCommentErrorPrefix = "PR comment: "

// FailSyncInput records a failed operation on an existing ticket.
type FailSyncInput struct {
	ID string
	// Message is the redacted error shown in the Problems view.
	Message       string
	NextAttemptAt time.Time
	Now           time.Time
}

// BeginCreateInput starts the creation of a PR's ticket.
type BeginCreateInput struct {
	UpdatePRID      string
	TicketProjectID string
	Now             time.Time
}

// FailCreateInput records a failed creation attempt.
type FailCreateInput struct {
	UpdatePRID      string
	TicketProjectID string
	// Message is the redacted error shown in the Problems view.
	Message string
	// ClearMarker drops the pending marker; set only when the ticket system cannot have stored a
	// ticket.
	ClearMarker bool
	// NextAttemptAt is when the next attempt may run.
	NextAttemptAt time.Time
	Now           time.Time
}

// LinkCreatedInput stores the ticket that a create returned.
type LinkCreatedInput struct {
	ID          string
	ExternalID  string
	ExternalURL string
	// SyncedHash is the hash of the dependency list the ticket content was rendered for.
	SyncedHash string
	Now        time.Time
}

// UpdateTicketRepository reads update PRs for the ticket engine and writes their update_tickets
// rows.
type UpdateTicketRepository interface {
	// Candidate returns the update PR with its dependencies, its resolved ticket project and
	// its ticket row, read from one snapshot, or ErrUpdatePRNotFound.
	Candidate(ctx context.Context, updatePRID string) (TicketCandidate, error)
	// PendingPRs returns the ids of the open update PRs that have dependencies and no ticket,
	// no pending marker and no retry that is not due at now.
	PendingPRs(ctx context.Context, now time.Time) ([]string, error)
	// BeginCreate writes the pending marker, in one transaction with the row it belongs to: it
	// inserts the PR's update_tickets row, or arms the one a failed attempt left without a
	// marker, and returns the row id, the external key of the ticket. It returns
	// ErrTicketExists for a PR that has its ticket and ErrCreateUnconfirmed, with the row id,
	// for one whose marker is set; it writes nothing then.
	BeginCreate(ctx context.Context, in BeginCreateInput) (id string, err error)
	// FailCreate records a failed attempt on the PR's row, inserting a row without a marker when
	// the failure came before BeginCreate, and drops the marker only when asked to.
	FailCreate(ctx context.Context, in FailCreateInput) error
	// NoteError records message as the row's last error without counting an attempt.
	NoteError(ctx context.Context, id, message string, now time.Time) error
	// LinkCreated stores the ticket's external id and URL, the open state and the hash, and
	// clears the pending marker, in one write. It returns ErrTicketExists when the row has a
	// ticket already.
	LinkCreated(ctx context.Context, in LinkCreatedInput) error
	// ClearCreateError drops the error and retry of a row that has no ticket and no marker; a
	// row with either keeps them.
	ClearCreateError(ctx context.Context, id string, now time.Time) error

	// SyncablePRs returns the ids of the PRs whose ticket exists, was not handed off, is not
	// waiting for a retry at now and may have an operation to run.
	SyncablePRs(ctx context.Context, now time.Time) ([]string, error)
	// SupersededBy returns the ids of the PRs that the PR replaced.
	SupersededBy(ctx context.Context, updatePRID string) ([]string, error)
	// BeginTransition records the state the ticket is about to be moved to, before the ticket
	// system is called. It returns ErrTicketNotSyncable when the row may not take it.
	BeginTransition(ctx context.Context, id, state string, now time.Time) error
	// CompleteTransition stores the state a recorded transition led to and drops the record,
	// the error and the retry. It returns ErrTicketNotSyncable when the row may not take it.
	CompleteTransition(ctx context.Context, id, state string, now time.Time) error
	// MarkSynced stores the hash of the dependency list the ticket now shows, and drops the
	// error and the retry. It returns ErrTicketNotSyncable when the row may not take it.
	MarkSynced(ctx context.Context, id, hash string, now time.Time) error
	// FailSync records a failed operation on a ticket that exists; a recorded transition stays.
	FailSync(ctx context.Context, in FailSyncInput) error
	// HandOff records that the ticket was changed outside Release Ops: nothing touches it after.
	HandOff(ctx context.Context, id string, now time.Time) error
	// Repository returns the id of the repository of the update ticket, or
	// ErrUpdateTicketNotFound.
	Repository(ctx context.Context, id string) (string, error)
	// ClearMarker drops the pending marker, the error and the retry of a row that has no ticket,
	// so the next event of its PR may create one. It returns ErrUpdateTicketNotFound for an
	// unknown row and ErrNoPendingMarker, changing nothing, for a row without a marker.
	ClearMarker(ctx context.Context, id string, now time.Time) error

	// CommentablePRs returns the ids of the PRs whose ticket exists and whose PR comment is due:
	// never written, or changed or failed since.
	CommentablePRs(ctx context.Context) ([]string, error)
	// StorePRComment stores the id of the bot comment and the hash of the body written to it for
	// a ticket in state, and drops an error an earlier comment write recorded. When the ticket has
	// moved on from state meanwhile only the id is stored and the comment stays due. It returns
	// ErrTicketNotSyncable when the row has no ticket.
	StorePRComment(ctx context.Context, id string, commentID int64, state, hash string, now time.Time) error
	// NotePRCommentError records that the PR comment could not be written, for the Problems view,
	// and makes the write due again. It sets no retry time and counts no attempt, so the ticket's
	// operations are not held back, and it keeps the error of a ticket operation. It returns
	// ErrTicketNotSyncable when the row has no ticket.
	NotePRCommentError(ctx context.Context, id, message string, now time.Time) error
}

// UpdateTickets returns the update ticket repository.
func (s *Store) UpdateTickets() UpdateTicketRepository {
	return updateTicketRepo{store: s}
}

type updateTicketRepo struct {
	store *Store
}

func (r updateTicketRepo) Candidate(ctx context.Context, updatePRID string) (TicketCandidate, error) {
	// One read transaction, so the PR, its dependencies and its ticket come from the same
	// snapshot even when a write commits between the reads.
	tx, err := r.store.db.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TicketCandidate{}, fmt.Errorf("begin candidate read: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.reads.WithTx(tx)
	row, err := q.GetUpdatePRForTicket(ctx, updatePRID)
	if errors.Is(err, sql.ErrNoRows) {
		return TicketCandidate{}, ErrUpdatePRNotFound
	}
	if err != nil {
		return TicketCandidate{}, fmt.Errorf("read update PR: %w", err)
	}
	var labels []string
	if err := json.Unmarshal([]byte(row.Labels), &labels); err != nil {
		return TicketCandidate{}, fmt.Errorf("stored labels of the update PR: %w", err)
	}
	c := TicketCandidate{
		ID: row.ID, RepositoryID: row.RepositoryID, FullName: row.FullName, Number: row.Number, URL: row.Url,
		GitHubInstallationID: row.GithubInstallationID,
		Title:                row.Title, State: row.State, SupersededByID: row.SupersededByID.String, Labels: labels, ReleaseNotes: row.ReleaseNotes.String,
	}
	if row.Automerge.Valid {
		v := row.Automerge.Int64 == 1
		c.Automerge = &v
	}
	switch {
	case row.RepositoryTicketProjectID.Valid:
		c.TicketProjectID = row.RepositoryTicketProjectID.String
	case row.DefaultTicketProjectID.Valid:
		c.TicketProjectID = row.DefaultTicketProjectID.String
	}

	deps, err := q.ListUpdatePRDependencies(ctx, updatePRID)
	if err != nil {
		return TicketCandidate{}, fmt.Errorf("list update PR dependencies: %w", err)
	}
	for _, d := range deps {
		c.Dependencies = append(c.Dependencies, UpdatePRDependency{
			Name: d.Name, Manager: d.Manager.String, Datasource: d.Datasource.String, From: d.FromVersion.String,
			To: d.ToVersion.String, UpdateType: d.UpdateType.String, Source: d.Source,
		})
	}

	t, err := q.GetUpdateTicketByPR(ctx, updatePRID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return TicketCandidate{}, fmt.Errorf("read update ticket: %w", err)
	default:
		ticket := UpdateTicket{
			ID: t.ID, TicketProjectID: t.TicketProjectID, Created: t.ExternalID.Valid, ExternalID: t.ExternalID.String, ExternalURL: t.ExternalUrl.String,
			State: t.State, TargetState: t.TargetState.String, SyncedHash: t.SyncedHash.String, HandsOff: t.HandsOffAt.Valid,
			Unconfirmed: t.PendingSince.Valid, Attempts: t.Attempts, LastError: t.LastError.String,
			PRCommentID: t.PrCommentID.Int64, PRCommentHash: t.PrCommentHash.String,
		}
		if t.NextAttemptAt.Valid {
			if ticket.NextAttemptAt, err = time.Parse(timeLayout, t.NextAttemptAt.String); err != nil {
				return TicketCandidate{}, fmt.Errorf("stored next_attempt_at %q: %w", t.NextAttemptAt.String, err)
			}
		}
		c.Ticket = &ticket
	}
	return c, nil
}

func (r updateTicketRepo) PendingPRs(ctx context.Context, now time.Time) ([]string, error) {
	ids, err := r.store.reads.ListUpdatePRsToTicket(ctx, sql.NullString{String: formatTime(now), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list update PRs without a ticket: %w", err)
	}
	return ids, nil
}

func (r updateTicketRepo) BeginCreate(ctx context.Context, in BeginCreateInput) (string, error) {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin ticket marker: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)
	now := formatTime(in.Now)
	marker := sql.NullString{String: now, Valid: true}

	row, err := q.GetUpdateTicketByPR(ctx, in.UpdatePRID)
	var id string
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id = newID()
		if err := q.InsertUpdateTicket(ctx, db.InsertUpdateTicketParams{
			ID: id, UpdatePrID: in.UpdatePRID, TicketProjectID: in.TicketProjectID, PendingSince: marker, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			return "", fmt.Errorf("insert update ticket: %w", err)
		}
	case err != nil:
		return "", fmt.Errorf("read update ticket: %w", err)
	case row.ExternalID.Valid:
		return "", ErrTicketExists
	case row.PendingSince.Valid:
		return row.ID, ErrCreateUnconfirmed
	default:
		id = row.ID
		n, err := q.ArmUpdateTicketMarker(ctx, db.ArmUpdateTicketMarkerParams{
			TicketProjectID: in.TicketProjectID, PendingSince: marker, UpdatedAt: now, ID: id,
		})
		if err != nil {
			return "", fmt.Errorf("set ticket marker: %w", err)
		}
		if n == 0 {
			return "", fmt.Errorf("set ticket marker: the row changed under the write")
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit ticket marker: %w", err)
	}
	return id, nil
}

func (r updateTicketRepo) FailCreate(ctx context.Context, in FailCreateInput) error {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ticket failure: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)
	now := sql.NullString{String: formatTime(in.Now), Valid: true}
	next := sql.NullString{String: formatTime(in.NextAttemptAt), Valid: true}
	message := sql.NullString{String: in.Message, Valid: true}

	row, err := q.GetUpdateTicketByPR(ctx, in.UpdatePRID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := q.InsertUpdateTicket(ctx, db.InsertUpdateTicketParams{
			ID: newID(), UpdatePrID: in.UpdatePRID, TicketProjectID: in.TicketProjectID, Attempts: 1, NextAttemptAt: next,
			LastError: message, LastErrorAt: now, CreatedAt: now.String, UpdatedAt: now.String,
		}); err != nil {
			return fmt.Errorf("insert update ticket: %w", err)
		}
	case err != nil:
		return fmt.Errorf("read update ticket: %w", err)
	default:
		clear := int64(0)
		if in.ClearMarker {
			clear = 1
		}
		if _, err := q.FailUpdateTicket(ctx, db.FailUpdateTicketParams{
			ClearMarker: clear, NextAttemptAt: next, LastError: message, At: now, ID: row.ID,
		}); err != nil {
			return fmt.Errorf("record ticket failure: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ticket failure: %w", err)
	}
	return nil
}

func (r updateTicketRepo) NoteError(ctx context.Context, id, message string, now time.Time) error {
	at := formatTime(now)
	_, err := r.store.writes.NoteUpdateTicketError(ctx, db.NoteUpdateTicketErrorParams{
		LastError: sql.NullString{String: message, Valid: true}, LastErrorAt: sql.NullString{String: at, Valid: true}, UpdatedAt: at, ID: id,
		LastError_2: sql.NullString{String: message, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("record ticket error: %w", err)
	}
	return nil
}

func (r updateTicketRepo) LinkCreated(ctx context.Context, in LinkCreatedInput) error {
	n, err := r.store.writes.LinkUpdateTicket(ctx, db.LinkUpdateTicketParams{
		ExternalID: sql.NullString{String: in.ExternalID, Valid: true}, ExternalUrl: nullIfEmpty(in.ExternalURL),
		SyncedHash: nullIfEmpty(in.SyncedHash), UpdatedAt: formatTime(in.Now), ID: in.ID,
	})
	if err != nil {
		return fmt.Errorf("store ticket link: %w", err)
	}
	if n == 0 {
		return ErrTicketExists
	}
	return nil
}

func (r updateTicketRepo) ClearCreateError(ctx context.Context, id string, now time.Time) error {
	if _, err := r.store.writes.ClearUpdateTicketCreateError(ctx, db.ClearUpdateTicketCreateErrorParams{UpdatedAt: formatTime(now), ID: id}); err != nil {
		return fmt.Errorf("clear ticket error: %w", err)
	}
	return nil
}

func (r updateTicketRepo) SyncablePRs(ctx context.Context, now time.Time) ([]string, error) {
	ids, err := r.store.reads.ListUpdatePRsToSync(ctx, sql.NullString{String: formatTime(now), Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list update PRs to sync: %w", err)
	}
	return ids, nil
}

func (r updateTicketRepo) SupersededBy(ctx context.Context, updatePRID string) ([]string, error) {
	ids, err := r.store.reads.ListUpdatePRsSupersededBy(ctx, sql.NullString{String: updatePRID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list the PRs replaced by an update PR: %w", err)
	}
	return ids, nil
}

func (r updateTicketRepo) BeginTransition(ctx context.Context, id, state string, now time.Time) error {
	n, err := r.store.writes.BeginUpdateTicketTransition(ctx, db.BeginUpdateTicketTransitionParams{
		TargetState: sql.NullString{String: state, Valid: true}, UpdatedAt: formatTime(now), ID: id,
	})
	return syncWriteResult("record the ticket transition", n, err)
}

func (r updateTicketRepo) CompleteTransition(ctx context.Context, id, state string, now time.Time) error {
	n, err := r.store.writes.CompleteUpdateTicketTransition(ctx, db.CompleteUpdateTicketTransitionParams{State: state, At: formatTime(now), ID: id})
	return syncWriteResult("store the ticket state", n, err)
}

func (r updateTicketRepo) MarkSynced(ctx context.Context, id, hash string, now time.Time) error {
	n, err := r.store.writes.MarkUpdateTicketSynced(ctx, db.MarkUpdateTicketSyncedParams{
		SyncedHash: sql.NullString{String: hash, Valid: true}, UpdatedAt: formatTime(now), ID: id,
	})
	return syncWriteResult("store the ticket hash", n, err)
}

func (r updateTicketRepo) FailSync(ctx context.Context, in FailSyncInput) error {
	at := sql.NullString{String: formatTime(in.Now), Valid: true}
	n, err := r.store.writes.FailUpdateTicketSync(ctx, db.FailUpdateTicketSyncParams{
		NextAttemptAt: sql.NullString{String: formatTime(in.NextAttemptAt), Valid: true},
		LastError:     sql.NullString{String: in.Message, Valid: true}, At: at, ID: in.ID,
	})
	return syncWriteResult("record the ticket failure", n, err)
}

func (r updateTicketRepo) HandOff(ctx context.Context, id string, now time.Time) error {
	at := formatTime(now)
	n, err := r.store.writes.HandOffUpdateTicket(ctx, db.HandOffUpdateTicketParams{HandsOffAt: sql.NullString{String: at, Valid: true}, UpdatedAt: at, ID: id})
	return syncWriteResult("hand the ticket off", n, err)
}

func (r updateTicketRepo) Repository(ctx context.Context, id string) (string, error) {
	repositoryID, err := r.store.reads.GetUpdateTicketRepository(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUpdateTicketNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read update ticket repository: %w", err)
	}
	return repositoryID, nil
}

func (r updateTicketRepo) ClearMarker(ctx context.Context, id string, now time.Time) error {
	n, err := r.store.writes.ClearUpdateTicketMarker(ctx, db.ClearUpdateTicketMarkerParams{UpdatedAt: formatTime(now), ID: id})
	if err != nil {
		return fmt.Errorf("clear pending marker: %w", err)
	}
	if n > 0 {
		return nil
	}
	if _, err := r.Repository(ctx, id); err != nil {
		return err
	}
	return ErrNoPendingMarker
}

func (r updateTicketRepo) CommentablePRs(ctx context.Context) ([]string, error) {
	ids, err := r.store.reads.ListUpdatePRsToComment(ctx)
	if err != nil {
		return nil, fmt.Errorf("list update PRs whose comment is due: %w", err)
	}
	return ids, nil
}

func (r updateTicketRepo) StorePRComment(ctx context.Context, id string, commentID int64, state, hash string, now time.Time) error {
	n, err := r.store.writes.StoreUpdateTicketPRComment(ctx, db.StoreUpdateTicketPRCommentParams{
		CommentID: sql.NullInt64{Int64: commentID, Valid: true}, State: state, Hash: sql.NullString{String: hash, Valid: true}, At: formatTime(now), ID: id,
	})
	return syncWriteResult("store the PR comment", n, err)
}

func (r updateTicketRepo) NotePRCommentError(ctx context.Context, id, message string, now time.Time) error {
	n, err := r.store.writes.NoteUpdateTicketCommentError(ctx, db.NoteUpdateTicketCommentErrorParams{
		Message: sql.NullString{String: PRCommentErrorPrefix + message, Valid: true}, At: formatTime(now), ID: id,
	})
	return syncWriteResult("record the PR comment error", n, err)
}

// syncWriteResult turns the outcome of a write to an existing ticket into an error: a failed
// write is wrapped, and one that changed no row is ErrTicketNotSyncable.
func syncWriteResult(what string, rows int64, err error) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if rows == 0 {
		return fmt.Errorf("%s: %w", what, ErrTicketNotSyncable)
	}
	return nil
}
