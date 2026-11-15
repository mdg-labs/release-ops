package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mdg-labs/release-ops/internal/store/db"
)

// WebhookDeliveryRetention is how long a delivery's GUID is remembered. It outlasts the
// redelivery lookback of the recovery pass (specs.html § 2.11), so a redelivered delivery
// is still recognised.
const WebhookDeliveryRetention = 7 * 24 * time.Hour

// WebhookDelivery is one accepted delivery of GitHub's webhook.
type WebhookDelivery struct {
	// GUID is the X-GitHub-Delivery value.
	GUID  string
	Event string
	// GitHubInstallationID is the installation id of the payload, 0 when it names none.
	// The row keeps the instance's installation id when that installation is recorded.
	GitHubInstallationID int64
	ReceivedAt           time.Time
}

// WebhookDeliveryRepository records the deliveries the webhook endpoint has accepted.
type WebhookDeliveryRepository interface {
	// Receive records the delivery and runs apply in the same transaction, so the GUID is
	// never remembered without the work it caused: when apply fails, nothing is recorded
	// and the error is returned. A GUID recorded before changes nothing, does not run
	// apply and reports duplicate. Records older than WebhookDeliveryRetention before
	// the delivery's ReceivedAt are pruned in the same transaction.
	Receive(ctx context.Context, d WebhookDelivery, apply func(ctx context.Context, tx *sql.Tx) error) (duplicate bool, err error)
}

// WebhookDeliveries returns the webhook delivery repository.
func (s *Store) WebhookDeliveries() WebhookDeliveryRepository {
	return webhookDeliveryRepo{store: s}
}

type webhookDeliveryRepo struct {
	store *Store
}

func (r webhookDeliveryRepo) Receive(ctx context.Context, d WebhookDelivery, apply func(ctx context.Context, tx *sql.Tx) error) (bool, error) {
	tx, err := r.store.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin webhook delivery: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	q := r.store.writes.WithTx(tx)

	if _, err := q.PruneWebhookDeliveries(ctx, d.ReceivedAt.Add(-WebhookDeliveryRetention).UTC().Format(timeLayout)); err != nil {
		return false, fmt.Errorf("prune webhook deliveries: %w", err)
	}
	inserted, err := q.InsertWebhookDelivery(ctx, db.InsertWebhookDeliveryParams{
		Guid:                 d.GUID,
		Event:                d.Event,
		GithubInstallationID: d.GitHubInstallationID,
		ReceivedAt:           d.ReceivedAt.UTC().Format(timeLayout),
	})
	if err != nil {
		return false, fmt.Errorf("record webhook delivery: %w", err)
	}
	if inserted == 0 {
		return true, nil
	}
	if apply != nil {
		if err := apply(ctx, tx); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit webhook delivery: %w", err)
	}
	return false, nil
}
