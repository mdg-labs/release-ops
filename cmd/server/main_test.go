package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/poll"
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

// enabledRepos serves a fixed repo list; the scheduler calls nothing else on it here.
type enabledRepos struct {
	store.MonitoredRepoRepository
	repos []store.MonitoredRepo
}

func (r enabledRepos) ListEnabled(context.Context) ([]store.MonitoredRepo, error) {
	return r.repos, nil
}

func TestShutdownSchedulerWaitsForRunToWriteItsFinish(t *testing.T) {
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
	appStore := store.New(sqlDB, cipher)
	if err := appStore.Settings().EnsureDefault(context.Background()); err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	polls := appStore.Poll()

	lifecycle, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(polls),
		Settings: appStore.Settings(),
		Repos:    enabledRepos{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
		Poll:     polls,
		PollRepo: func(ctx context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			close(started)
			<-ctx.Done()
			// The run is still winding down when the shutdown wait begins.
			time.Sleep(300 * time.Millisecond)
			return nil, context.Canceled
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.Start(lifecycle); err != nil {
		t.Fatalf("Start: %v", err)
	}
	runID, err := scheduler.Trigger(lifecycle)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	<-started

	cancel()
	shutdownScheduler(scheduler)

	got, err := polls.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != poll.RunStatusFailed || got.FinishedAt == nil {
		t.Fatalf("run status = %q finished_at = %v, want failed and finished", got.Status, got.FinishedAt)
	}
	if !strings.Contains(got.ErrorsJSON, "repo-1") || got.ReposChecked != 1 {
		t.Fatalf("run errors = %s repos_checked = %d, want the repo-1 error and 1 repo checked", got.ErrorsJSON, got.ReposChecked)
	}
}
