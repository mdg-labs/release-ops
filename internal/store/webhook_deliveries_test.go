package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
)

var deliveryAt = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func countDeliveries(t *testing.T, db *store.DB) int {
	t.Helper()
	var n int
	if err := db.Writer.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&n); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	return n
}

func TestWebhookDeliveryIsRecordedWithItsApply(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil {
		t.Fatal(err)
	}
	applied := 0
	dup, err := s.WebhookDeliveries().Receive(ctx, store.WebhookDelivery{GUID: "g1", Event: "installation", GitHubInstallationID: 1000, ReceivedAt: deliveryAt},
		func(ctx context.Context, tx *sql.Tx) error {
			applied++
			_, err := tx.ExecContext(ctx, `UPDATE installations SET account_login = 'renamed'`)
			return err
		})
	if dup || err != nil || applied != 1 {
		t.Fatalf("Receive = dup %v, err %v, applied %d, want a new delivery applied once", dup, err, applied)
	}

	var event, installation, receivedAt, login string
	if err := db.Writer.QueryRow(`SELECT d.event, d.installation_id, d.received_at, i.account_login FROM webhook_deliveries d JOIN installations i ON i.id = d.installation_id WHERE d.guid = 'g1'`).
		Scan(&event, &installation, &receivedAt, &login); err != nil {
		t.Fatalf("read delivery: %v", err)
	}
	if event != "installation" || receivedAt != "2026-11-14T12:00:00.000Z" || login != "renamed" {
		t.Errorf("row = %s %s %s", event, receivedAt, login)
	}
}

func TestWebhookDeliveryDuplicateDoesNotApply(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	d := store.WebhookDelivery{GUID: "g1", Event: "push", ReceivedAt: deliveryAt}
	if _, err := s.WebhookDeliveries().Receive(ctx, d, nil); err != nil {
		t.Fatal(err)
	}
	dup, err := s.WebhookDeliveries().Receive(ctx, d, func(context.Context, *sql.Tx) error {
		t.Error("apply ran for a GUID seen before")
		return nil
	})
	if !dup || err != nil || countDeliveries(t, db) != 1 {
		t.Fatalf("Receive = dup %v, err %v, rows %d, want a duplicate and one row", dup, err, countDeliveries(t, db))
	}
}

func TestWebhookDeliveryApplyFailureLeavesNothing(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.Installations().Record(ctx, sampleInstallation(1000)); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("enqueue failed")
	d := store.WebhookDelivery{GUID: "g1", Event: "installation", GitHubInstallationID: 1000, ReceivedAt: deliveryAt}
	_, err := s.WebhookDeliveries().Receive(ctx, d, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE installations SET account_login = 'renamed'`); err != nil {
			t.Fatal(err)
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Receive error = %v, want the apply error", err)
	}
	var login string
	if err := db.Writer.QueryRow(`SELECT account_login FROM installations`).Scan(&login); err != nil || login != "acme" {
		t.Errorf("login = %q, %v, want the apply's write rolled back", login, err)
	}
	if countDeliveries(t, db) != 0 {
		t.Fatalf("rows = %d, want the record rolled back", countDeliveries(t, db))
	}

	dup, err := s.WebhookDeliveries().Receive(ctx, d, nil)
	if dup || err != nil || countDeliveries(t, db) != 1 {
		t.Errorf("redelivery = dup %v, err %v, rows %d, want it processed", dup, err, countDeliveries(t, db))
	}
}

func TestWebhookDeliveryPrunedByRetentionIsProcessedAsNew(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	for _, d := range []store.WebhookDelivery{
		{GUID: "old", Event: "push", ReceivedAt: deliveryAt},
		{GUID: "edge", Event: "push", ReceivedAt: deliveryAt.Add(time.Hour)},
	} {
		if _, err := s.WebhookDeliveries().Receive(ctx, d, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Exactly one window after "edge" it is still inside; "old" is older than the window.
	now := deliveryAt.Add(time.Hour + store.DefaultWebhookDeliveryRetention)
	res, err := store.NewRetention(db, store.RetentionOptions{Now: func() time.Time { return now }}).Prune(ctx)
	if err != nil || res.Deliveries != 1 {
		t.Fatalf("Prune = %+v, %v, want exactly the old record deleted", res, err)
	}

	dup, err := s.WebhookDeliveries().Receive(ctx, store.WebhookDelivery{GUID: "old", Event: "push", ReceivedAt: now}, nil)
	if dup || err != nil {
		t.Errorf("pruned guid = dup %v, err %v, want it processed as new", dup, err)
	}
	applied := false
	dup, err = s.WebhookDeliveries().Receive(ctx, store.WebhookDelivery{GUID: "edge", Event: "push", ReceivedAt: now}, func(context.Context, *sql.Tx) error {
		applied = true
		return nil
	})
	if !dup || err != nil || applied {
		t.Errorf("guid inside the window = dup %v, err %v, applied %v, want a duplicate that applies nothing", dup, err, applied)
	}
}

func TestWebhookDeliveryRequestPathDoesNotPrune(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.WebhookDeliveries().Receive(ctx, store.WebhookDelivery{GUID: "old", Event: "push", ReceivedAt: deliveryAt}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WebhookDeliveries().Receive(ctx, store.WebhookDelivery{GUID: "new", Event: "push", ReceivedAt: deliveryAt.Add(365 * 24 * time.Hour)}, nil); err != nil {
		t.Fatal(err)
	}
	if n := countDeliveries(t, db); n != 2 {
		t.Fatalf("rows = %d, want both: receiving a delivery deletes nothing", n)
	}
}

func TestGitHubAppWebhookSecret(t *testing.T) {
	t.Parallel()

	s, db, _ := newGitHubAppStore(t)
	ctx := context.Background()
	if _, err := s.GitHubApp().WebhookSecret(ctx); !errors.Is(err, store.ErrGitHubAppNotFound) {
		t.Fatalf("before an App: error = %v, want ErrGitHubAppNotFound", err)
	}
	seedSetupToken(t, db)
	app := sampleApp()
	if err := s.GitHubApp().Create(ctx, app); err != nil {
		t.Fatal(err)
	}
	got, err := s.GitHubApp().WebhookSecret(ctx)
	if err != nil || string(got) != app.WebhookSecret {
		t.Fatalf("WebhookSecret = %q, %v, want the stored secret decrypted", got, err)
	}
}
