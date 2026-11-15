package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrRepositoryNotFound is returned for a repository that is not recorded, was removed from its
// installation, or belongs to an installation that was removed.
var ErrRepositoryNotFound = errors.New("repository not found")

// Repository is an installed repository as an admin acts on it.
type Repository struct {
	ID            string
	FullName      string
	DefaultBranch string
	Enabled       bool
	// ConfigState is unknown, present or missing.
	ConfigState string
	// InstallationStatus is active or suspended.
	InstallationStatus string
}

// RepositoryRepository reads and changes the repositories of the installation.
type RepositoryRepository interface {
	// Get returns the repository, or ErrRepositoryNotFound.
	Get(ctx context.Context, id string) (Repository, error)
	// SetEnabled pauses (false) or enables (true) the repository and returns it as stored. In
	// the same transaction a pause ends its queued job: one that already ran an attempt (a
	// retry waiting its turn) is marked failed and keeps its runs and events, one that never
	// ran is deleted. A running job is aborted at its next heartbeat. It returns
	// ErrRepositoryNotFound for a repository Get would not find.
	SetEnabled(ctx context.Context, id string, enabled bool) (Repository, error)
	// List returns the repositories an admin can act on with their ticket projects, filtered and
	// paged, and the number the filter matches in all.
	List(ctx context.Context, filter RepositoryFilter) ([]RepositoryListing, int, error)
	// Update applies the change in one transaction and returns the repository as stored: a
	// pause ends its queued job as SetEnabled does, and a ticket-project override is set or
	// cleared. It returns ErrRepositoryNotFound for a repository Get would not find and
	// ErrTicketProjectNotFound, with nothing changed, for an override that names no ticket
	// project.
	Update(ctx context.Context, id string, change RepositoryChange) (RepositoryListing, error)
	// ListScheduled returns the ids of the repositories the hourly safety net enqueues: enabled,
	// of an active installation, not recorded as having no config, and with no queued or
	// running job.
	ListScheduled(ctx context.Context) ([]string, error)
}

// ErrTicketProjectNotFound is returned for a ticket project id that names no ticket project.
var ErrTicketProjectNotFound = errors.New("ticket project not found")

// RepositoryListing is a repository with its ticket project settings.
type RepositoryListing struct {
	Repository
	// TicketProjectID is the repository's own override; empty when it has none.
	TicketProjectID string
	// EffectiveTicketProjectID and EffectiveTicketProjectName are the project its PRs get tickets
	// in: the override, else the installation's default; empty when neither is set, which
	// means no tickets.
	EffectiveTicketProjectID   string
	EffectiveTicketProjectName string
}

// RepositoryFilter selects and pages the repositories List returns; empty fields filter nothing.
type RepositoryFilter struct {
	// Search is a substring of the full name, matched without regard to case.
	Search string
	// Enabled, when set, keeps the repositories that are enabled (true) or paused (false).
	Enabled *bool
	// ConfigState is unknown, present or missing.
	ConfigState string
	// Page counts from 1.
	Page, PageSize int
}

// RepositoryChange is what Update applies; a zero field changes nothing.
type RepositoryChange struct {
	// Enabled pauses (false) or enables (true) the repository.
	Enabled *bool
	// SetTicketProject sets the override to TicketProjectID, or clears it when that is empty.
	SetTicketProject bool
	TicketProjectID  string
}

// WebhookRepository is the repository a webhook payload names.
type WebhookRepository struct {
	ID            string
	FullName      string
	DefaultBranch string
	Enabled       bool
}

// RepositoryByGitHubID returns the repository a webhook payload names by its GitHub id, read
// through the delivery's transaction so a repository the same delivery synced is found. It
// returns ErrRepositoryNotFound for a repository that is not recorded or was removed from
// its installation; a paused one is returned, with Enabled false.
func (s *Store) RepositoryByGitHubID(ctx context.Context, tx *sql.Tx, githubRepositoryID int64) (WebhookRepository, error) {
	row, err := s.writes.WithTx(tx).GetRepositoryByGitHubID(ctx, githubRepositoryID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && row.RemovedAt.Valid) {
		return WebhookRepository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return WebhookRepository{}, fmt.Errorf("read repository: %w", err)
	}
	return WebhookRepository{ID: row.ID, FullName: row.FullName, DefaultBranch: row.DefaultBranch, Enabled: row.Enabled == 1}, nil
}

// Repositories returns the repository repository.
func (s *Store) Repositories() RepositoryRepository {
	return repositoryRepo{store: s}
}

type repositoryRepo struct {
	store *Store
}

func repositoryFromRow(r db.GetManageableRepositoryRow) Repository {
	return Repository{
		ID:                 r.ID,
		FullName:           r.FullName,
		DefaultBranch:      r.DefaultBranch,
		Enabled:            r.Enabled == 1,
		ConfigState:        r.ConfigState,
		InstallationStatus: r.InstallationStatus,
	}
}

func (r repositoryRepo) Get(ctx context.Context, id string) (Repository, error) {
	row, err := r.store.reads.GetManageableRepository(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, ErrRepositoryNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("read repository: %w", err)
	}
	return repositoryFromRow(row), nil
}

func (r repositoryRepo) SetEnabled(ctx context.Context, id string, enabled bool) (Repository, error) {
	l, err := r.Update(ctx, id, RepositoryChange{Enabled: &enabled})
	return l.Repository, err
}

func (r repositoryRepo) Update(ctx context.Context, id string, change RepositoryChange) (RepositoryListing, error) {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return RepositoryListing{}, fmt.Errorf("begin repository update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := r.store.writes.WithTx(tx)

	if change.SetTicketProject {
		if change.TicketProjectID != "" {
			n, err := qtx.TicketProjectExists(ctx, change.TicketProjectID)
			if err != nil {
				return RepositoryListing{}, fmt.Errorf("read ticket project: %w", err)
			}
			if n == 0 {
				return RepositoryListing{}, ErrTicketProjectNotFound
			}
		}
		n, err := qtx.SetRepositoryTicketProject(ctx, db.SetRepositoryTicketProjectParams{
			TicketProjectID: nullIfEmpty(change.TicketProjectID), UpdatedAt: nowUTC(), ID: id,
		})
		if err != nil {
			return RepositoryListing{}, fmt.Errorf("update repository: %w", err)
		}
		if n == 0 {
			return RepositoryListing{}, ErrRepositoryNotFound
		}
	}
	if change.Enabled != nil {
		value := int64(0)
		if *change.Enabled {
			value = 1
		}
		n, err := qtx.SetRepositoryEnabled(ctx, db.SetRepositoryEnabledParams{Enabled: value, UpdatedAt: nowUTC(), ID: id})
		if err != nil {
			return RepositoryListing{}, fmt.Errorf("update repository: %w", err)
		}
		if n == 0 {
			return RepositoryListing{}, ErrRepositoryNotFound
		}
		if !*change.Enabled {
			if _, err := qtx.FailQueuedJobsWithRunsForRepository(ctx, db.FailQueuedJobsWithRunsForRepositoryParams{Now: nowUTC(), RepositoryID: id}); err != nil {
				return RepositoryListing{}, fmt.Errorf("end queued job with attempts: %w", err)
			}
			if _, err := qtx.DeleteQueuedJobsForRepository(ctx, id); err != nil {
				return RepositoryListing{}, fmt.Errorf("drop queued job: %w", err)
			}
		}
	}
	row, err := qtx.GetManageableRepositoryListing(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return RepositoryListing{}, ErrRepositoryNotFound
	}
	if err != nil {
		return RepositoryListing{}, fmt.Errorf("read repository: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RepositoryListing{}, fmt.Errorf("commit repository update: %w", err)
	}
	return listingFromRow(row.ID, row.FullName, row.DefaultBranch, row.Enabled, row.ConfigState, row.InstallationStatus, row.TicketProjectID, row.EffectiveTicketProjectID, row.EffectiveTicketProjectName), nil
}

func (r repositoryRepo) List(ctx context.Context, f RepositoryFilter) ([]RepositoryListing, int, error) {
	enabled := sql.NullInt64{}
	if f.Enabled != nil {
		enabled.Valid = true
		if *f.Enabled {
			enabled.Int64 = 1
		}
	}
	page, size := max(f.Page, 1), max(f.PageSize, 1)
	rows, err := r.store.reads.ListManageableRepositories(ctx, db.ListManageableRepositoriesParams{
		Search: f.Search, Enabled: nullable(enabled), ConfigState: f.ConfigState, PageSize: int64(size), PageOffset: int64((page - 1) * size),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	total, err := r.store.reads.CountManageableRepositories(ctx, db.CountManageableRepositoriesParams{
		Search: f.Search, Enabled: nullable(enabled), ConfigState: f.ConfigState,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count repositories: %w", err)
	}
	out := make([]RepositoryListing, 0, len(rows))
	for _, row := range rows {
		out = append(out, listingFromRow(row.ID, row.FullName, row.DefaultBranch, row.Enabled, row.ConfigState, row.InstallationStatus, row.TicketProjectID, row.EffectiveTicketProjectID, row.EffectiveTicketProjectName))
	}
	return out, int(total), nil
}

// nullable passes a NULL integer to a query argument that sqlc types as any.
func nullable(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func listingFromRow(id, fullName, defaultBranch string, enabled int64, configState, installationStatus string, override sql.NullString, effectiveID any, effectiveName sql.NullString) RepositoryListing {
	l := RepositoryListing{
		Repository:                 Repository{ID: id, FullName: fullName, DefaultBranch: defaultBranch, Enabled: enabled == 1, ConfigState: configState, InstallationStatus: installationStatus},
		TicketProjectID:            override.String,
		EffectiveTicketProjectName: effectiveName.String,
	}
	if v, ok := effectiveID.(string); ok {
		l.EffectiveTicketProjectID = v
	}
	return l
}

func (r repositoryRepo) ListScheduled(ctx context.Context) ([]string, error) {
	ids, err := r.store.reads.ListScheduledRunRepositoryIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scheduled repositories: %w", err)
	}
	return ids, nil
}
