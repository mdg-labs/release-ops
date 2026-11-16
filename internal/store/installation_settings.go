package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// ErrInstallationNotFound is returned for an installation that is not recorded or was removed.
var ErrInstallationNotFound = errors.New("installation not found")

// InstallationSummary is an installation as an admin reads it (specs.html section 7.2).
type InstallationSummary struct {
	ID                  string
	AccountLogin        string
	AccountType         string
	Status              string
	RepositorySelection string
	RepositoryCount     int64
	// Permissions is the JSON object GitHub reports for the installation.
	Permissions string
	// RateLimit fields are nil until GitHub's budget has been read.
	RateLimitRemaining, RateLimitLimit *int64
	RateLimitResetAt                   *string
	// DefaultTicketProjectID is empty when the installation has none.
	DefaultTicketProjectID string
}

// InstallationSettingsRepository reads installations and sets their default ticket project.
type InstallationSettingsRepository interface {
	// List returns the installations, newest first: the live one and a removed one kept for
	// history.
	List(ctx context.Context) ([]InstallationSummary, error)
	// SetDefaultTicketProject sets (id) or clears (empty) the default ticket project of the
	// installation, creating its settings row when it has none. It returns
	// ErrInstallationNotFound for an installation that is unknown or removed and
	// ErrTicketProjectNotFound, with nothing changed, for a project that does not exist.
	SetDefaultTicketProject(ctx context.Context, installationID, ticketProjectID string) error
}

// InstallationSettings returns the installation settings repository.
func (s *Store) InstallationSettings() InstallationSettingsRepository {
	return installationSettingsRepo{store: s}
}

type installationSettingsRepo struct {
	store *Store
}

func (r installationSettingsRepo) List(ctx context.Context) ([]InstallationSummary, error) {
	rows, err := r.store.reads.ListInstallationsForAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("list installations: %w", err)
	}
	out := make([]InstallationSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, InstallationSummary{
			ID: row.ID, AccountLogin: row.AccountLogin, AccountType: row.AccountType, Status: row.Status,
			RepositorySelection: row.RepositorySelection, RepositoryCount: row.RepositoryCount, Permissions: row.Permissions,
			RateLimitRemaining: nullInt64Ptr(row.RateLimitRemaining), RateLimitLimit: nullInt64Ptr(row.RateLimitLimit),
			RateLimitResetAt: nullStringPtr(row.RateLimitResetAt), DefaultTicketProjectID: row.DefaultTicketProjectID.String,
		})
	}
	return out, nil
}

func (r installationSettingsRepo) SetDefaultTicketProject(ctx context.Context, installationID, ticketProjectID string) error {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin installation settings: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	status, err := q.GetInstallationStatus(ctx, installationID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && status == "removed") {
		return ErrInstallationNotFound
	}
	if err != nil {
		return fmt.Errorf("read installation: %w", err)
	}
	if ticketProjectID != "" {
		n, err := q.TicketProjectExists(ctx, ticketProjectID)
		if err != nil {
			return fmt.Errorf("read ticket project: %w", err)
		}
		if n == 0 {
			return ErrTicketProjectNotFound
		}
	}
	if err := q.UpsertInstallationDefaultTicketProject(ctx, db.UpsertInstallationDefaultTicketProjectParams{
		InstallationID: installationID, DefaultTicketProjectID: nullIfEmpty(ticketProjectID), UpdatedAt: nowUTC(),
	}); err != nil {
		return fmt.Errorf("store installation settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit installation settings: %w", err)
	}
	return nil
}

func nullInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}
