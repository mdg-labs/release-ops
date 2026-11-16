package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store/db"
)

// RateLimits keeps the shared GitHub rate-limit budget of an installation on its row
// (installations.rate_limit_*, specs.html § 2.8), so a queued job of an exhausted installation
// stays queued until the reset and the budget survives a restart.
type RateLimits struct {
	store *Store
}

var _ ratelimit.Store = RateLimits{}

// RateLimits returns the rate-limit budget store.
func (s *Store) RateLimits() RateLimits {
	return RateLimits{store: s}
}

// SaveRateLimit implements ratelimit.Store. A budget of an installation that is not recorded is
// dropped without an error: the installation is gone.
func (r RateLimits) SaveRateLimit(ctx context.Context, s ratelimit.State) error {
	_, err := r.store.writes.UpdateInstallationRateLimit(ctx, db.UpdateInstallationRateLimitParams{
		RateLimitRemaining:   sql.NullInt64{Int64: s.Remaining, Valid: true},
		RateLimitLimit:       sql.NullInt64{Int64: s.Limit, Valid: s.Limit > 0},
		RateLimitResetAt:     sql.NullString{String: formatTime(s.ResetAt), Valid: !s.ResetAt.IsZero()},
		GithubInstallationID: s.InstallationID,
	})
	if err != nil {
		return fmt.Errorf("save rate limit: %w", err)
	}
	return nil
}

// Load returns the stored budgets of the installations that are not removed. A budget whose
// stored reset time cannot be read comes back with a zero ResetAt.
func (r RateLimits) Load(ctx context.Context) ([]ratelimit.State, error) {
	rows, err := r.store.reads.ListInstallationRateLimits(ctx)
	if err != nil {
		return nil, fmt.Errorf("load rate limits: %w", err)
	}
	out := make([]ratelimit.State, 0, len(rows))
	for _, row := range rows {
		resetAt, err := time.Parse(timeLayout, row.RateLimitResetAt.String)
		if err != nil {
			// A reset time that cannot be read must not stop the server from starting. The budget
			// comes back without one, so it holds nothing back and is written over at the next flush.
			slog.Warn("rate limit: stored reset time is not readable, ignoring it",
				"installation_id", row.GithubInstallationID)
			resetAt = time.Time{}
		}
		out = append(out, ratelimit.State{
			InstallationID: row.GithubInstallationID,
			Remaining:      row.RateLimitRemaining.Int64,
			Limit:          row.RateLimitLimit.Int64,
			ResetAt:        resetAt,
		})
	}
	return out, nil
}
