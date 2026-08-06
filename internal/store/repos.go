package store

import (
	"context"

	"github.com/mdguggenbichler/release-ops/internal/store/db"
)

// MonitoredRepo is a repository monitored for new releases.
type MonitoredRepo struct {
	ID                    string
	SourceKind            string
	ProjectPath           string
	Enabled               bool
	SourceIntegrationID   *string
	TicketProjectID       string
	OpenTicketExternalID  *string
	OpenTicketTag         *string
	LastKnownTag          *string
	LastPolledAt          *string
	LastError             *string
	NotificationTargetIDs []string
	CreatedAt             string
	UpdatedAt             string
}

// CreateMonitoredRepoInput holds fields for a new monitored repo.
type CreateMonitoredRepoInput struct {
	SourceKind            string
	ProjectPath           string
	Enabled               bool
	SourceIntegrationID   *string
	TicketProjectID       string
	NotificationTargetIDs []string
}

// UpdateMonitoredRepoInput holds updatable monitored repo fields.
type UpdateMonitoredRepoInput struct {
	SourceKind            string
	ProjectPath           string
	Enabled               bool
	SourceIntegrationID   *string
	TicketProjectID       string
	NotificationTargetIDs []string
}

// MonitoredRepoRepository manages monitored_repos and notification join rows.
type MonitoredRepoRepository interface {
	Create(ctx context.Context, input CreateMonitoredRepoInput) (*MonitoredRepo, error)
	Get(ctx context.Context, id string) (*MonitoredRepo, error)
	List(ctx context.Context) ([]MonitoredRepo, error)
	ListEnabled(ctx context.Context) ([]MonitoredRepo, error)
	Update(ctx context.Context, id string, input UpdateMonitoredRepoInput) (*MonitoredRepo, error)
	SetEnabled(ctx context.Context, id string, enabled bool) (*MonitoredRepo, error)
	Delete(ctx context.Context, id string) error
}

type monitoredRepoRepo struct {
	store *Store
}

func (r monitoredRepoRepo) Create(ctx context.Context, input CreateMonitoredRepoInput) (*MonitoredRepo, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	q := r.store.q.WithTx(tx)
	now := nowUTC()
	id := newID()

	row, err := q.CreateMonitoredRepo(ctx, db.CreateMonitoredRepoParams{
		ID:                  id,
		SourceKind:          input.SourceKind,
		ProjectPath:         input.ProjectPath,
		Enabled:             boolToInt64(input.Enabled),
		SourceIntegrationID: stringPtrToNull(input.SourceIntegrationID),
		TicketProjectID:     input.TicketProjectID,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		return nil, err
	}

	if err := replaceNotificationTargets(ctx, q, id, input.NotificationTargetIDs); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.repoWithNotificationIDs(ctx, row, input.NotificationTargetIDs)
}

func (r monitoredRepoRepo) Get(ctx context.Context, id string) (*MonitoredRepo, error) {
	row, err := r.store.q.GetMonitoredRepo(ctx, id)
	if err != nil {
		return nil, err
	}
	return r.repoWithNotificationIDs(ctx, row, nil)
}

func (r monitoredRepoRepo) List(ctx context.Context) ([]MonitoredRepo, error) {
	rows, err := r.store.q.ListMonitoredRepos(ctx)
	if err != nil {
		return nil, err
	}
	return r.reposWithNotificationIDs(ctx, rows)
}

func (r monitoredRepoRepo) ListEnabled(ctx context.Context) ([]MonitoredRepo, error) {
	rows, err := r.store.q.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]MonitoredRepo, len(rows))
	for i, row := range rows {
		out[i] = monitoredRepoFromListEnabledRow(row)
	}
	return out, nil
}

func (r monitoredRepoRepo) Update(ctx context.Context, id string, input UpdateMonitoredRepoInput) (*MonitoredRepo, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	q := r.store.q.WithTx(tx)
	row, err := q.UpdateMonitoredRepo(ctx, db.UpdateMonitoredRepoParams{
		SourceKind:          input.SourceKind,
		ProjectPath:         input.ProjectPath,
		Enabled:             boolToInt64(input.Enabled),
		SourceIntegrationID: stringPtrToNull(input.SourceIntegrationID),
		TicketProjectID:     input.TicketProjectID,
		UpdatedAt:           nowUTC(),
		ID:                  id,
	})
	if err != nil {
		return nil, err
	}

	if err := replaceNotificationTargets(ctx, q, id, input.NotificationTargetIDs); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return r.repoWithNotificationIDs(ctx, row, input.NotificationTargetIDs)
}

func (r monitoredRepoRepo) SetEnabled(ctx context.Context, id string, enabled bool) (*MonitoredRepo, error) {
	row, err := r.store.q.SetMonitoredRepoEnabled(ctx, db.SetMonitoredRepoEnabledParams{
		Enabled:   boolToInt64(enabled),
		UpdatedAt: nowUTC(),
		ID:        id,
	})
	if err != nil {
		return nil, err
	}
	return r.repoWithNotificationIDs(ctx, row, nil)
}

func (r monitoredRepoRepo) Delete(ctx context.Context, id string) error {
	return r.store.q.DeleteMonitoredRepo(ctx, id)
}

func (r monitoredRepoRepo) repoWithNotificationIDs(
	ctx context.Context,
	row db.MonitoredRepo,
	knownIDs []string,
) (*MonitoredRepo, error) {
	repo := monitoredRepoFromRow(row)
	if knownIDs != nil {
		repo.NotificationTargetIDs = append([]string(nil), knownIDs...)
		return repo, nil
	}
	ids, err := r.store.q.ListNotificationTargetIDsForRepo(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	repo.NotificationTargetIDs = ids
	return repo, nil
}

func (r monitoredRepoRepo) reposWithNotificationIDs(ctx context.Context, rows []db.MonitoredRepo) ([]MonitoredRepo, error) {
	out := make([]MonitoredRepo, len(rows))
	for i, row := range rows {
		repo, err := r.repoWithNotificationIDs(ctx, row, nil)
		if err != nil {
			return nil, err
		}
		out[i] = *repo
	}
	return out, nil
}

func replaceNotificationTargets(ctx context.Context, q *db.Queries, repoID string, targetIDs []string) error {
	if err := q.DeleteMonitoredRepoNotificationsByRepo(ctx, repoID); err != nil {
		return err
	}
	for _, targetID := range targetIDs {
		if err := q.LinkMonitoredRepoNotification(ctx, db.LinkMonitoredRepoNotificationParams{
			MonitoredRepoID:      repoID,
			NotificationTargetID: targetID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func monitoredRepoFromRow(row db.MonitoredRepo) *MonitoredRepo {
	return &MonitoredRepo{
		ID:                   row.ID,
		SourceKind:           row.SourceKind,
		ProjectPath:          row.ProjectPath,
		Enabled:              int64ToBool(row.Enabled),
		SourceIntegrationID:  nullStringPtr(row.SourceIntegrationID),
		TicketProjectID:      row.TicketProjectID,
		OpenTicketExternalID: nullStringPtr(row.OpenTicketExternalID),
		OpenTicketTag:        nullStringPtr(row.OpenTicketTag),
		LastKnownTag:         nullStringPtr(row.LastKnownTag),
		LastPolledAt:         nullStringPtr(row.LastPolledAt),
		LastError:            nullStringPtr(row.LastError),
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func monitoredRepoFromListEnabledRow(row db.ListEnabledRow) MonitoredRepo {
	repo := MonitoredRepo{
		ID:                   row.ID,
		SourceKind:           row.SourceKind,
		ProjectPath:          row.ProjectPath,
		Enabled:              int64ToBool(row.Enabled),
		SourceIntegrationID:  nullStringPtr(row.SourceIntegrationID),
		TicketProjectID:      row.TicketProjectID,
		OpenTicketExternalID: nullStringPtr(row.OpenTicketExternalID),
		OpenTicketTag:        nullStringPtr(row.OpenTicketTag),
		LastKnownTag:         nullStringPtr(row.LastKnownTag),
		LastPolledAt:         nullStringPtr(row.LastPolledAt),
		LastError:            nullStringPtr(row.LastError),
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
	if raw, ok := row.NotificationTargetIds.(string); ok && raw != "" {
		// GROUP_CONCAT returns comma-separated IDs; split for callers.
		for _, part := range splitCommaSeparated(raw) {
			repo.NotificationTargetIDs = append(repo.NotificationTargetIDs, part)
		}
	}
	return repo
}

func splitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
