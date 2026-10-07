package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// PollStateUpdate holds monitored repo fields updated after a poll cycle.
type PollStateUpdate struct {
	OpenTicketExternalID   *string
	OpenTicketTag          *string
	LastKnownTag           *string
	LastReleasePublishedAt *string
	LastPolledAt           *string
	LastError              *string
}

// Poll trigger provenance values (poll_runs.trigger_source).
const (
	PollTriggerSourceManual    = "manual"
	PollTriggerSourceScheduled = "scheduled"
)

// PollRun is an audit record for a poll execution.
type PollRun struct {
	ID                string
	StartedAt         string
	FinishedAt        *string
	Status            string
	TriggerSource     string
	ReposChecked      int64
	TicketsCreated    int64
	TicketsSuperseded int64
	ErrorsJSON        string
}

// PollRunEvent is a per-repo action logged during a poll run.
type PollRunEvent struct {
	ID              string
	PollRunID       string
	MonitoredRepoID *string
	Action          string
	Detail          *string
	CreatedAt       string

	TicketExternalID *string
	TicketURL        *string
	ReleaseTag       *string

	// SourceKind and ProjectPath come from the monitored repo the event belongs to;
	// both are nil once the repo is deleted. They are read-only and never written.
	SourceKind  *string
	ProjectPath *string
}

// PollEventRef is the ticket and release an event refers to (poll_run_events.ticket_external_id,
// ticket_url, release_tag); every field is optional.
type PollEventRef struct {
	TicketExternalID *string
	TicketURL        *string
	ReleaseTag       *string
}

// ErrNotificationTargetsUnread is returned by UpdatePollState together with the
// updated repo when the state was stored but the repo's notification targets could
// not be read afterwards. The repo's NotificationTargetIDs are unset in that case:
// callers keep the ids they already hold.
var ErrNotificationTargetsUnread = errors.New("notification targets unread after poll state write")

// PollRepository updates poll state on monitored repos and manages poll run audit rows.
type PollRepository interface {
	// UpdatePollState stores update and returns the repo with its notification target ids.
	// An error that wraps ErrNotificationTargetsUnread comes with a non-nil repo and means
	// the state is stored; any other error means nothing was stored.
	UpdatePollState(ctx context.Context, repoID string, update PollStateUpdate) (*MonitoredRepo, error)
	InsertRun(ctx context.Context, triggerSource string) (*PollRun, error)
	FinishRun(ctx context.Context, id string, status string, reposChecked, ticketsCreated, ticketsSuperseded int64, errorsJSON string) (*PollRun, error)
	GetRun(ctx context.Context, id string) (*PollRun, error)
	ListRuns(ctx context.Context, limit, offset int64) ([]PollRun, error)
	InsertEvent(ctx context.Context, pollRunID string, monitoredRepoID *string, action string, detail *string, ref PollEventRef) (*PollRunEvent, error)
	ListEventsByRunID(ctx context.Context, pollRunID string) ([]PollRunEvent, error)
}

type pollRepo struct {
	store *Store
}

func (r pollRepo) UpdatePollState(ctx context.Context, repoID string, update PollStateUpdate) (*MonitoredRepo, error) {
	row, err := r.store.q.UpdatePollState(ctx, db.UpdatePollStateParams{
		OpenTicketExternalID:   stringPtrToNull(update.OpenTicketExternalID),
		OpenTicketTag:          stringPtrToNull(update.OpenTicketTag),
		LastKnownTag:           stringPtrToNull(update.LastKnownTag),
		LastReleasePublishedAt: stringPtrToNull(update.LastReleasePublishedAt),
		LastPolledAt:           stringPtrToNull(update.LastPolledAt),
		LastError:              stringPtrToNull(update.LastError),
		UpdatedAt:              nowUTC(),
		ID:                     repoID,
	})
	if err != nil {
		return nil, err
	}

	repo := monitoredRepoFromRow(monitoredRepoRow(row))
	// The UPDATE above is already committed: a failing read must not look like a failed write.
	ids, err := r.store.q.ListNotificationTargetIDsForRepo(ctx, repoID)
	if err != nil {
		return repo, fmt.Errorf("%w: %w", ErrNotificationTargetsUnread, err)
	}
	repo.NotificationTargetIDs = ids
	return repo, nil
}

func (r pollRepo) InsertRun(ctx context.Context, triggerSource string) (*PollRun, error) {
	row, err := r.store.q.InsertRun(ctx, db.InsertRunParams{
		ID:            newID(),
		StartedAt:     nowUTC(),
		TriggerSource: triggerSource,
	})
	if err != nil {
		return nil, err
	}
	return pollRunFromRow(pollRunRow(row)), nil
}

func (r pollRepo) FinishRun(
	ctx context.Context,
	id string,
	status string,
	reposChecked, ticketsCreated, ticketsSuperseded int64,
	errorsJSON string,
) (*PollRun, error) {
	row, err := r.store.q.FinishRun(ctx, db.FinishRunParams{
		FinishedAt:        sql.NullString{String: nowUTC(), Valid: true},
		Status:            status,
		ReposChecked:      reposChecked,
		TicketsCreated:    ticketsCreated,
		TicketsSuperseded: ticketsSuperseded,
		ErrorsJson:        errorsJSON,
		ID:                id,
	})
	if err != nil {
		return nil, err
	}
	return pollRunFromRow(pollRunRow(row)), nil
}

func (r pollRepo) GetRun(ctx context.Context, id string) (*PollRun, error) {
	row, err := r.store.q.GetPollRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return pollRunFromRow(row), nil
}

func (r pollRepo) ListRuns(ctx context.Context, limit, offset int64) ([]PollRun, error) {
	rows, err := r.store.q.ListPollRuns(ctx, db.ListPollRunsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		return nil, err
	}
	out := make([]PollRun, len(rows))
	for i, row := range rows {
		out[i] = *pollRunFromRow(pollRunRow(row))
	}
	return out, nil
}

func (r pollRepo) InsertEvent(
	ctx context.Context,
	pollRunID string,
	monitoredRepoID *string,
	action string,
	detail *string,
	ref PollEventRef,
) (*PollRunEvent, error) {
	row, err := r.store.q.InsertEvent(ctx, db.InsertEventParams{
		ID:               newID(),
		PollRunID:        pollRunID,
		MonitoredRepoID:  stringPtrToNull(monitoredRepoID),
		Action:           action,
		Detail:           stringPtrToNull(detail),
		CreatedAt:        nowUTC(),
		TicketExternalID: stringPtrToNull(ref.TicketExternalID),
		TicketUrl:        stringPtrToNull(ref.TicketURL),
		ReleaseTag:       stringPtrToNull(ref.ReleaseTag),
	})
	if err != nil {
		return nil, err
	}
	return pollRunEventFromRow(row), nil
}

func (r pollRepo) ListEventsByRunID(ctx context.Context, pollRunID string) ([]PollRunEvent, error) {
	rows, err := r.store.q.ListPollRunEventsByRunID(ctx, pollRunID)
	if err != nil {
		return nil, err
	}
	out := make([]PollRunEvent, len(rows))
	for i, row := range rows {
		ev := pollRunEventFromRow(db.PollRunEvent{
			ID:               row.ID,
			PollRunID:        row.PollRunID,
			MonitoredRepoID:  row.MonitoredRepoID,
			Action:           row.Action,
			Detail:           row.Detail,
			CreatedAt:        row.CreatedAt,
			TicketExternalID: row.TicketExternalID,
			TicketUrl:        row.TicketUrl,
			ReleaseTag:       row.ReleaseTag,
		})
		ev.SourceKind = nullStringPtr(row.SourceKind)
		ev.ProjectPath = nullStringPtr(row.ProjectPath)
		out[i] = *ev
	}
	return out, nil
}

// pollRunRow is the shape every poll_runs query returns; sqlc emits one identical
// row type per query, so the others convert to it.
type pollRunRow = db.GetPollRunRow

func pollRunFromRow(row pollRunRow) *PollRun {
	return &PollRun{
		ID:                row.ID,
		StartedAt:         row.StartedAt,
		FinishedAt:        nullStringPtr(row.FinishedAt),
		Status:            row.Status,
		TriggerSource:     row.TriggerSource,
		ReposChecked:      row.ReposChecked,
		TicketsCreated:    row.TicketsCreated,
		TicketsSuperseded: row.TicketsSuperseded,
		ErrorsJSON:        row.ErrorsJson,
	}
}

func pollRunEventFromRow(row db.PollRunEvent) *PollRunEvent {
	return &PollRunEvent{
		ID:               row.ID,
		PollRunID:        row.PollRunID,
		MonitoredRepoID:  nullStringPtr(row.MonitoredRepoID),
		Action:           row.Action,
		Detail:           nullStringPtr(row.Detail),
		CreatedAt:        row.CreatedAt,
		TicketExternalID: nullStringPtr(row.TicketExternalID),
		TicketURL:        nullStringPtr(row.TicketUrl),
		ReleaseTag:       nullStringPtr(row.ReleaseTag),
	}
}
