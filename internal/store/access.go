package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// InstallationAccount is the account the instance's one live installation is on.
type InstallationAccount struct {
	// ID is the row id; GitHubInstallationID is GitHub's id of the installation.
	ID                   string
	GitHubInstallationID int64
	AccountID            int64
	AccountLogin         string
	// AccountType is User or Organization.
	AccountType string
}

// AccessRepository reads what a role and a viewer's repository scope are decided from.
type AccessRepository interface {
	// LiveInstallation returns the installation that is not removed; ok is false while none
	// is recorded.
	LiveInstallation(ctx context.Context) (account InstallationAccount, ok bool, err error)
	// InstalledRepositoryIDs maps the GitHub id of each repository of the installation that
	// has not been removed from it to its row id.
	InstalledRepositoryIDs(ctx context.Context, installationRowID string) (map[int64]string, error)
	// SessionRepositoryIDs returns the row ids of the repositories stored for the session
	// that are still installed. A session with no stored list has none, which is no access.
	SessionRepositoryIDs(ctx context.Context, sessionToken string) ([]string, error)
}

// Access returns the access repository.
func (s *Store) Access() AccessRepository {
	return accessRepo{store: s}
}

type accessRepo struct {
	store *Store
}

func (r accessRepo) LiveInstallation(ctx context.Context) (InstallationAccount, bool, error) {
	row, err := r.store.reads.GetLiveInstallation(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return InstallationAccount{}, false, nil
	}
	if err != nil {
		return InstallationAccount{}, false, fmt.Errorf("read installation: %w", err)
	}
	return InstallationAccount{
		ID:                   row.ID,
		GitHubInstallationID: row.GithubInstallationID,
		AccountID:            row.AccountID,
		AccountLogin:         row.AccountLogin,
		AccountType:          row.AccountType,
	}, true, nil
}

func (r accessRepo) InstalledRepositoryIDs(ctx context.Context, installationRowID string) (map[int64]string, error) {
	rows, err := r.store.reads.ListActiveRepositoryIDs(ctx, installationRowID)
	if err != nil {
		return nil, fmt.Errorf("list installed repositories: %w", err)
	}
	ids := make(map[int64]string, len(rows))
	for _, row := range rows {
		ids[row.GithubRepositoryID] = row.ID
	}
	return ids, nil
}

func (r accessRepo) SessionRepositoryIDs(ctx context.Context, sessionToken string) ([]string, error) {
	if sessionToken == "" {
		return nil, nil
	}
	ids, err := r.store.reads.ListSessionRepositoryIDs(ctx, sessionToken)
	if err != nil {
		return nil, fmt.Errorf("list session repositories: %w", err)
	}
	return ids, nil
}
