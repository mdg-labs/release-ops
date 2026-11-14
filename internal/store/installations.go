package store

import (
	"context"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// NewInstallation is a GitHub App installation an instance records after GitHub has
// confirmed it.
type NewInstallation struct {
	GitHubInstallationID int64
	AccountID            int64
	AccountLogin         string
	AccountType          string
	// Status is active or suspended.
	Status              string
	RepositorySelection string
	// Permissions is the JSON object of the installation's permissions; empty means "{}".
	Permissions string
}

// InstallationOutcome says what Record did.
type InstallationOutcome int

const (
	// InstallationRecorded stored the installation.
	InstallationRecorded InstallationOutcome = iota
	// InstallationKnown found the same installation already recorded and changed nothing.
	InstallationKnown
	// InstallationRefused found another installation recorded and changed nothing: the
	// instance has exactly one.
	InstallationRefused
)

// InstallationRepository stores the instance's GitHub App installation.
type InstallationRepository interface {
	// LiveIDs returns the GitHub ids of the installations that are not removed.
	LiveIDs(ctx context.Context) ([]int64, error)
	// Record stores the installation unless one that is not removed already exists, which
	// it reports as InstallationKnown for the same GitHub id and InstallationRefused for
	// another. A removed row with the same GitHub id is replaced.
	Record(ctx context.Context, in NewInstallation) (InstallationOutcome, error)
}

// Installations returns the installation repository.
func (s *Store) Installations() InstallationRepository {
	return installationRepo{store: s}
}

type installationRepo struct {
	store *Store
}

func (r installationRepo) LiveIDs(ctx context.Context) ([]int64, error) {
	ids, err := r.store.reads.ListLiveInstallationIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	return ids, nil
}

func (r installationRepo) Record(ctx context.Context, in NewInstallation) (InstallationOutcome, error) {
	permissions := in.Permissions
	if permissions == "" {
		permissions = "{}"
	}

	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin installation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	live, err := q.ListLiveInstallationIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("list installations: %w", err)
	}
	if len(live) > 0 {
		if live[0] == in.GitHubInstallationID {
			return InstallationKnown, nil
		}
		return InstallationRefused, nil
	}

	now := nowUTC()
	err = q.UpsertInstallation(ctx, db.UpsertInstallationParams{
		ID:                   newID(),
		GithubInstallationID: in.GitHubInstallationID,
		AccountID:            in.AccountID,
		AccountLogin:         in.AccountLogin,
		AccountType:          in.AccountType,
		Status:               in.Status,
		RepositorySelection:  in.RepositorySelection,
		Permissions:          permissions,
		CreatedAt:            now,
		UpdatedAt:            now,
	})
	if err != nil {
		return 0, fmt.Errorf("store installation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit installation: %w", err)
	}
	return InstallationRecorded, nil
}
