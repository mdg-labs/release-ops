package poll_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

func TestReconcileInterruptedRunsFinishesOnlyRunningRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	sqlDB := openIntegrationDB(t, filepath.Join(t.TempDir(), "app.db"))
	t.Cleanup(func() { _ = sqlDB.Close() })
	cipher, err := crypto.NewCipherFromHex(integrationTestKeyHex)
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	polls := store.New(sqlDB, cipher).Poll()

	stale, err := polls.InsertRun(ctx, store.PollTriggerSourceScheduled)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	finished, err := polls.InsertRun(ctx, store.PollTriggerSourceManual)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if _, err := polls.FinishRun(ctx, finished.ID, poll.RunStatusSuccess, 3, 1, 0, `[]`); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	n, err := poll.ReconcileInterruptedRuns(ctx, polls)
	if err != nil {
		t.Fatalf("ReconcileInterruptedRuns: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d runs, want 1", n)
	}

	got, err := polls.GetRun(ctx, stale.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != poll.RunStatusFailed || got.FinishedAt == nil {
		t.Fatalf("stale run status = %q finished_at = %v, want failed and finished", got.Status, got.FinishedAt)
	}
	var entries []poll.RunErrorEntry
	if err := json.Unmarshal([]byte(got.ErrorsJSON), &entries); err != nil || len(entries) != 1 || entries[0].Message == "" {
		t.Fatalf("errors_json = %q (%v), want one interrupted entry", got.ErrorsJSON, err)
	}

	other, err := polls.GetRun(ctx, finished.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if other.Status != poll.RunStatusSuccess || other.ReposChecked != 3 || other.ErrorsJSON != `[]` {
		t.Fatalf("finished run was changed: %+v", other)
	}

	n, err = poll.ReconcileInterruptedRuns(ctx, polls)
	if err != nil || n != 0 {
		t.Fatalf("second reconcile = %d, %v; want 0, nil", n, err)
	}
}
