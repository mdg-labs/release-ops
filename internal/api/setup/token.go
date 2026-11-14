package setup

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

// neverExpires is the expires_at of a setup token: it stays valid until it is redeemed or
// an App is stored, not for a fixed time.
const neverExpires = "9999-12-31T23:59:59.000Z"

const isoTimeFormat = "2006-01-02T15:04:05.000Z"

// Required reports whether the instance is in setup mode: no GitHub App is stored.
func Required(ctx context.Context, q *storedb.Queries) (bool, error) {
	n, err := q.CountGitHubApp(ctx)
	if err != nil {
		return false, fmt.Errorf("count github app: %w", err)
	}
	return n == 0, nil
}

// EnsureToken runs at startup. While no App is configured it stores the hash of the setup
// token, replacing any earlier one, and tells the operator how to use it through logf:
// configured (SETUP_TOKEN) when set, otherwise a freshly generated token that is printed
// once, here, and never stored. Only the SHA-256 hash reaches the database. With an App
// configured it does nothing.
func EnsureToken(ctx context.Context, db *sql.DB, configured, appURL string, logf func(format string, args ...any)) error {
	q := storedb.New(db)
	required, err := Required(ctx, q)
	if err != nil {
		return err
	}
	if !required {
		return nil
	}

	raw := configured
	if raw == "" {
		raw, err = auth.GenerateRawToken()
		if err != nil {
			return err
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin setup token: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	qtx := q.WithTx(tx)
	if err := qtx.DeleteSetupTokens(ctx); err != nil {
		return fmt.Errorf("replace setup token: %w", err)
	}
	if _, err := qtx.CreateAuthToken(ctx, storedb.CreateAuthTokenParams{
		ID:        uuid.NewString(),
		Kind:      auth.TokenKindSetup,
		TokenHash: auth.HashToken(raw),
		ExpiresAt: neverExpires,
		CreatedAt: nowUTC(),
	}); err != nil {
		return fmt.Errorf("store setup token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit setup token: %w", err)
	}

	if configured != "" {
		logf("setup required: open %s/setup and enter the token set in SETUP_TOKEN", appURL)
		return nil
	}
	logf("setup required: open %s/setup and enter this one-time setup token: %s", appURL, raw)
	return nil
}

func nowUTC() string {
	return time.Now().UTC().Format(isoTimeFormat)
}
