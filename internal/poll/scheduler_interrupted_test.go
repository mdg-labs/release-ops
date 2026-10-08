package poll_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

type finishCall struct {
	runID, status string
	reposChecked  int64
	errorsJSON    string
	hasDeadline   bool
}

// recordingFinishRepo fails FinishRun the way a database does on a cancelled context.
type recordingFinishRepo struct {
	schedulerMockPollRepo
	mu    sync.Mutex
	calls []finishCall
}

func newRecordingFinishRepo() *recordingFinishRepo {
	r := &recordingFinishRepo{}
	r.finishRunFn = func(ctx context.Context, id, status string, reposChecked, _, _ int64, errorsJSON string) (*store.PollRun, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, hasDeadline := ctx.Deadline()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.calls = append(r.calls, finishCall{runID: id, status: status, reposChecked: reposChecked, errorsJSON: errorsJSON, hasDeadline: hasDeadline})
		return &store.PollRun{ID: id, Status: status}, nil
	}
	return r
}

func (r *recordingFinishRepo) finished() []finishCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]finishCall(nil), r.calls...)
}

func runErrorMessages(t *testing.T, raw string) []poll.RunErrorEntry {
	t.Helper()
	var entries []poll.RunErrorEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatalf("errors_json %q: %v", raw, err)
	}
	return entries
}

func TestSchedulerRunAllFinishesRunWhenContextCancelledMidway(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	polled := 0
	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(pollRepo),
		Repos:  &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}, {ID: "repo-2"}, {ID: "repo-3"}}},
		Poll:   pollRepo,
		PollRepo: func(_ context.Context, _ string, repo store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			polled++
			cancel()
			return &poll.RepoEvaluation{Actions: []string{poll.ActionSkip}, Repo: &repo}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if err := scheduler.RunAll(ctx, "run-1"); err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	if polled != 1 {
		t.Fatalf("polled %d repos after cancellation, want 1", polled)
	}
	calls := pollRepo.finished()
	if len(calls) != 1 {
		t.Fatalf("FinishRun calls = %d, want 1", len(calls))
	}
	got := calls[0]
	if got.runID != "run-1" || got.status != poll.RunStatusPartial || got.reposChecked != 1 {
		t.Fatalf("finish = %+v, want run-1 partial with 1 repo checked", got)
	}
	if !got.hasDeadline {
		t.Fatal("FinishRun context has no deadline, want a bounded write")
	}
	entries := runErrorMessages(t, got.errorsJSON)
	if len(entries) != 1 || !strings.Contains(entries[0].Message, "interrupted") {
		t.Fatalf("errors = %+v, want one interrupted entry", entries)
	}
}

func TestSchedulerRunAllKeepsRepoErrorsWhenInterrupted(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(pollRepo),
		Repos:  &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}, {ID: "repo-2"}}},
		Poll:   pollRepo,
		PollRepo: func(ctx context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			cancel()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if err := scheduler.RunAll(ctx, "run-1"); err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	calls := pollRepo.finished()
	if len(calls) != 1 || calls[0].status != poll.RunStatusFailed {
		t.Fatalf("finish calls = %+v, want one failed", calls)
	}
	entries := runErrorMessages(t, calls[0].errorsJSON)
	if len(entries) != 2 || entries[0].RepoID != "repo-1" {
		t.Fatalf("errors = %+v, want the repo-1 error then the interrupted entry", entries)
	}
}

type failingListRepos struct {
	schedulerMockReposRepo
}

func (*failingListRepos) ListEnabled(ctx context.Context) ([]store.MonitoredRepo, error) {
	return nil, ctx.Err()
}

func TestSchedulerRunAllFinishesRunWhenRepoListFails(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(pollRepo),
		Repos:  &failingListRepos{},
		Poll:   pollRepo,
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	err = scheduler.RunAll(ctx, "run-1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunAll error = %v, want the list error", err)
	}
	calls := pollRepo.finished()
	if len(calls) != 1 || calls[0].status != poll.RunStatusFailed || len(runErrorMessages(t, calls[0].errorsJSON)) != 1 {
		t.Fatalf("finish calls = %+v, want one failed run with one error", calls)
	}
}

func TestSchedulerShutdownFinishesRunningPoll(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(pollRepo),
		Settings: &schedulerMockSettingsRepo{pollIntervalMinutes: 360},
		Repos:    &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
		Poll:     pollRepo,
		PollRepo: func(ctx context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	runID, err := scheduler.Trigger(ctx)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	<-started
	cancel()

	deadline := time.Now().Add(2 * time.Second)
	for scheduler.IsPolling() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	calls := pollRepo.finished()
	if len(calls) != 1 || calls[0].runID != runID || calls[0].status != poll.RunStatusFailed {
		t.Fatalf("finish calls = %+v, want run %s failed", calls, runID)
	}
}

func TestSchedulerShutdownWaitsForManualRun(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(pollRepo),
		Settings: &schedulerMockSettingsRepo{pollIntervalMinutes: 360},
		Repos:    &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
		Poll:     pollRepo,
		PollRepo: func(ctx context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			close(started)
			<-ctx.Done()
			time.Sleep(200 * time.Millisecond)
			return nil, ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	runID, err := scheduler.Trigger(ctx)
	if err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	<-started
	cancel()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if err := scheduler.Shutdown(waitCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	calls := pollRepo.finished()
	if len(calls) != 1 || calls[0].runID != runID {
		t.Fatalf("finish calls after Shutdown = %+v, want run %s finished", calls, runID)
	}
}

func TestSchedulerShutdownGivesUpAtDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	release := make(chan struct{})
	pollRepo := newRecordingFinishRepo()
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(pollRepo),
		Settings: &schedulerMockSettingsRepo{pollIntervalMinutes: 360},
		Repos:    &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
		Poll:     pollRepo,
		PollRepo: func(context.Context, string, store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			close(started)
			<-release
			return nil, errors.New("late")
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := scheduler.Trigger(ctx); err != nil {
		t.Fatalf("Trigger: %v", err)
	}
	<-started
	cancel()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	if err := scheduler.Shutdown(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v, want deadline exceeded", err)
	}
	close(release)
}
