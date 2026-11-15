package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrInstallationNotLive is returned when a change names an installation that is not
// recorded or has been removed.
var ErrInstallationNotLive = errors.New("installation is not recorded or was removed")

// Config states of a repository (db/schema.sql repositories.config_state).
const (
	ConfigUnknown = "unknown"
	ConfigPresent = "present"
	ConfigMissing = "missing"
)

// SyncedRepository is a repository of an installation as GitHub reports it, from the API or
// from a webhook payload.
type SyncedRepository struct {
	GitHubID int64
	// FullName is owner/name.
	FullName string
	// DefaultBranch is empty when the source does not carry it (a webhook payload); a
	// stored branch is then kept.
	DefaultBranch string
	// ConfigState is ConfigPresent or ConfigMissing when the source looked for a Renovate
	// config file; empty or ConfigUnknown keeps the stored state.
	ConfigState string
}

// InstallationSync applies what GitHub reports about the installation and its repositories.
// The startup and periodic sync and the webhook handlers all go through it. Every method
// writes through the transaction it is given, so a caller makes one delivery's or one
// sync's changes atomic; Transaction opens one for the callers that have none.
type InstallationSync struct {
	store *Store
}

// InstallationSync returns the installation sync.
func (s *Store) InstallationSync() InstallationSync {
	return InstallationSync{store: s}
}

// Transaction runs fn in a transaction on the writer and commits when it returns nil.
func (s InstallationSync) Transaction(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin installation sync: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit installation sync: %w", err)
	}
	return nil
}

// LiveInstallationIDs returns the GitHub ids of the installations that are not removed.
func (s InstallationSync) LiveInstallationIDs(ctx context.Context) ([]int64, error) {
	return s.store.Installations().LiveIDs(ctx)
}

// ApplyInstallation records the installation, or refreshes the recorded one with what
// GitHub reports: account, status, repository selection and permissions. A row that already
// holds those values is left untouched. InstallationRefused means another installation is
// recorded and nothing changed.
func (s InstallationSync) ApplyInstallation(ctx context.Context, tx *sql.Tx, in NewInstallation) (InstallationOutcome, error) {
	q := s.store.writes.WithTx(tx)
	outcome, err := recordInstallation(ctx, q, in)
	if err != nil || outcome != InstallationKnown {
		return outcome, err
	}
	permissions := in.Permissions
	if permissions == "" {
		permissions = "{}"
	}
	if _, err := q.UpdateInstallation(ctx, db.UpdateInstallationParams{
		AccountID:            in.AccountID,
		AccountLogin:         in.AccountLogin,
		AccountType:          in.AccountType,
		Status:               in.Status,
		RepositorySelection:  in.RepositorySelection,
		Permissions:          permissions,
		UpdatedAt:            nowUTC(),
		GithubInstallationID: in.GitHubInstallationID,
	}); err != nil {
		return 0, fmt.Errorf("update installation: %w", err)
	}
	return outcome, nil
}

// RemoveInstallation marks the installation removed and its repositories as having left it.
// The rows stay, so update PR history survives; a removed installation hands out no work.
// An installation that is not recorded, or already removed, is left alone.
func (s InstallationSync) RemoveInstallation(ctx context.Context, tx *sql.Tx, githubInstallationID int64) error {
	q := s.store.writes.WithTx(tx)
	inst, err := q.GetInstallationByGitHubID(ctx, githubInstallationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read installation: %w", err)
	}
	if inst.Status == "removed" {
		return nil
	}
	now := nowUTC()
	if _, err := q.MarkInstallationRepositoriesRemoved(ctx, db.MarkInstallationRepositoriesRemovedParams{
		RemovedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, InstallationID: inst.ID,
	}); err != nil {
		return fmt.Errorf("remove installation repositories: %w", err)
	}
	if _, err := q.MarkInstallationRemoved(ctx, db.MarkInstallationRemovedParams{UpdatedAt: now, GithubInstallationID: githubInstallationID}); err != nil {
		return fmt.Errorf("remove installation: %w", err)
	}
	return nil
}

// UpsertRepositories adds the repositories to the installation, or refreshes the ones it
// has: a repository that returns to the installation is the same row again, and a paused
// one stays paused. New repositories are enabled. It returns ErrInstallationNotLive when the
// installation is not recorded or was removed.
func (s InstallationSync) UpsertRepositories(ctx context.Context, tx *sql.Tx, githubInstallationID int64, repos []SyncedRepository) error {
	q := s.store.writes.WithTx(tx)
	installationID, err := liveInstallationRowID(ctx, q, githubInstallationID)
	if err != nil {
		return err
	}
	return upsertRepositories(ctx, q, installationID, repos)
}

// RemoveRepositories marks the repositories as having left the installation. They keep
// their rows and history and get no work. One that is unknown or already removed is skipped.
func (s InstallationSync) RemoveRepositories(ctx context.Context, tx *sql.Tx, githubInstallationID int64, githubRepositoryIDs []int64) error {
	q := s.store.writes.WithTx(tx)
	installationID, err := liveInstallationRowID(ctx, q, githubInstallationID)
	if err != nil {
		return err
	}
	now := nowUTC()
	for _, id := range githubRepositoryIDs {
		if _, err := q.MarkRepositoryRemoved(ctx, db.MarkRepositoryRemovedParams{
			RemovedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, InstallationID: installationID, GithubRepositoryID: id,
		}); err != nil {
			return fmt.Errorf("remove repository: %w", err)
		}
	}
	return nil
}

// ReplaceRepositories makes the installation's repositories exactly the given list: they
// are upserted, and every other repository of the installation is marked removed. The list
// must be complete; a partial one removes what it leaves out.
func (s InstallationSync) ReplaceRepositories(ctx context.Context, tx *sql.Tx, githubInstallationID int64, repos []SyncedRepository) error {
	q := s.store.writes.WithTx(tx)
	installationID, err := liveInstallationRowID(ctx, q, githubInstallationID)
	if err != nil {
		return err
	}
	if err := upsertRepositories(ctx, q, installationID, repos); err != nil {
		return err
	}

	listed := make(map[int64]bool, len(repos))
	for _, r := range repos {
		listed[r.GitHubID] = true
	}
	stored, err := q.ListActiveRepositoryConfigStates(ctx, installationID)
	if err != nil {
		return fmt.Errorf("list repositories: %w", err)
	}
	now := nowUTC()
	for _, row := range stored {
		if listed[row.GithubRepositoryID] {
			continue
		}
		if _, err := q.MarkRepositoryRemoved(ctx, db.MarkRepositoryRemovedParams{
			RemovedAt: sql.NullString{String: now, Valid: true}, UpdatedAt: now, InstallationID: installationID, GithubRepositoryID: row.GithubRepositoryID,
		}); err != nil {
			return fmt.Errorf("remove repository: %w", err)
		}
	}
	return nil
}

// RepositoryConfigStates returns the config state of each repository of the installation
// that has not left it, by GitHub repository id.
func (s InstallationSync) RepositoryConfigStates(ctx context.Context, githubInstallationID int64) (map[int64]string, error) {
	inst, err := s.store.reads.GetInstallationByGitHubID(ctx, githubInstallationID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInstallationNotLive
	}
	if err != nil {
		return nil, fmt.Errorf("read installation: %w", err)
	}
	rows, err := s.store.reads.ListActiveRepositoryConfigStates(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	states := make(map[int64]string, len(rows))
	for _, row := range rows {
		states[row.GithubRepositoryID] = row.ConfigState
	}
	return states, nil
}

func liveInstallationRowID(ctx context.Context, q *db.Queries, githubInstallationID int64) (string, error) {
	inst, err := q.GetInstallationByGitHubID(ctx, githubInstallationID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInstallationNotLive
	}
	if err != nil {
		return "", fmt.Errorf("read installation: %w", err)
	}
	if inst.Status == "removed" {
		return "", ErrInstallationNotLive
	}
	return inst.ID, nil
}

func upsertRepositories(ctx context.Context, q *db.Queries, installationID string, repos []SyncedRepository) error {
	now := nowUTC()
	for _, r := range repos {
		state := r.ConfigState
		if state == "" {
			state = ConfigUnknown
		}
		if _, err := q.ReleaseRepositoryName(ctx, db.ReleaseRepositoryNameParams{
			InstallationID: installationID, FullName: r.FullName, GithubRepositoryID: r.GitHubID,
		}); err != nil {
			return fmt.Errorf("release repository name: %w", err)
		}
		if _, err := q.UpsertRepository(ctx, db.UpsertRepositoryParams{
			ID:                 newID(),
			InstallationID:     installationID,
			GithubRepositoryID: r.GitHubID,
			FullName:           r.FullName,
			DefaultBranch:      r.DefaultBranch,
			ConfigState:        state,
			CreatedAt:          now,
			UpdatedAt:          now,
		}); err != nil {
			return fmt.Errorf("store repository: %w", err)
		}
	}
	return nil
}
