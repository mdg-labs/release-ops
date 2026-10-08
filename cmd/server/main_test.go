package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

func TestReconcilePollRunsFinishesRunsLeftRunning(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)
	sqlDB, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	cipher, err := crypto.NewCipherFromHex("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewCipherFromHex: %v", err)
	}
	polls := store.New(sqlDB, cipher).Poll()

	run, err := polls.InsertRun(ctx, store.PollTriggerSourceScheduled)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	if err := reconcilePollRuns(ctx, polls); err != nil {
		t.Fatalf("reconcilePollRuns: %v", err)
	}

	got, err := polls.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != "failed" || got.FinishedAt == nil {
		t.Fatalf("run status = %q finished_at = %v, want failed and finished", got.Status, got.FinishedAt)
	}
}
