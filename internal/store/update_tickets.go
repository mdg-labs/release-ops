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
)

// TicketCandidate is an update PR as the ticket engine decides on it.
type TicketCandidate struct {
	ID           string
	RepositoryID string
	// FullName is the repository's owner/name.
	FullName string
	Number   int64
	URL      string
	Title    string
	// State is open, merged, closed or superseded.
	State  string
	Labels []string
	// Automerge is nil while it is unknown, which counts as not automerged.
	Automerge    *bool
	ReleaseNotes string
	// TicketProjectID is the repository's override, else the installation's default; empty when
	// neither is set.
	TicketProjectID string
	Dependencies    []UpdatePRDependency
	// Ticket is the PR's update_tickets row, nil while it has none.
	Ticket *UpdateTicket
}

// UpdateTicket is the update_tickets row of a PR, as far as creation reads it.
type UpdateTicket struct {
	ID              string
	TicketProjectID string
	// Created reports that the ticket is stored: its external id is known.
	Created bool
	// Unconfirmed reports the pending marker: a create was started and its ticket never stored.
	Unconfirmed bool
	Attempts    int64
	// NextAttemptAt is when the next attempt may run; zero when none is waiting.
	NextAttemptAt time.Time
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
	// its ticket row, or ErrUpdatePRNotFound.
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
}

// UpdateTickets returns the update ticket repository.
func (s *Store) UpdateTickets() UpdateTicketRepository {
	return updateTicketRepo{store: s}
}

type updateTicketRepo struct {
	store *Store
}

func (r updateTicketRepo) Candidate(ctx context.Context, updatePRID string) (TicketCandidate, error) {
	q := r.store.reads
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
		Title: row.Title, State: row.State, Labels: labels, ReleaseNotes: row.ReleaseNotes.String,
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
			ID: t.ID, TicketProjectID: t.TicketProjectID, Created: t.ExternalID.Valid, Unconfirmed: t.PendingSince.Valid, Attempts: t.Attempts,
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
