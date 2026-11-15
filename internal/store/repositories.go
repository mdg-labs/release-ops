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
	// ListScheduled returns the ids of the repositories the hourly safety net enqueues: enabled,
	// of an active installation, not recorded as having no config, and with no queued or
	// running job.
	ListScheduled(ctx context.Context) ([]string, error)
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
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := r.store.writes.WithTx(tx)

	value := int64(0)
	if enabled {
		value = 1
	}
	n, err := qtx.SetRepositoryEnabled(ctx, db.SetRepositoryEnabledParams{Enabled: value, UpdatedAt: nowUTC(), ID: id})
	if err != nil {
		return Repository{}, fmt.Errorf("update repository: %w", err)
	}
	if n == 0 {
		return Repository{}, ErrRepositoryNotFound
	}
	if !enabled {
		if _, err := qtx.FailQueuedJobsWithRunsForRepository(ctx, db.FailQueuedJobsWithRunsForRepositoryParams{Now: nowUTC(), RepositoryID: id}); err != nil {
			return Repository{}, fmt.Errorf("end queued job with attempts: %w", err)
		}
		if _, err := qtx.DeleteQueuedJobsForRepository(ctx, id); err != nil {
			return Repository{}, fmt.Errorf("drop queued job: %w", err)
		}
	}
	row, err := qtx.GetManageableRepository(ctx, id)
	if err != nil {
		return Repository{}, fmt.Errorf("read repository: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, fmt.Errorf("commit repository update: %w", err)
	}
	return repositoryFromRow(row), nil
}

func (r repositoryRepo) ListScheduled(ctx context.Context) ([]string, error) {
	ids, err := r.store.reads.ListScheduledRunRepositoryIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list scheduled repositories: %w", err)
	}
	return ids, nil
}
