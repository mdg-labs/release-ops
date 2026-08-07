package poll_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

type schedulerMockPollRepo struct {
	insertRunFn func(ctx context.Context) (*store.PollRun, error)
	finishRunFn func(
		ctx context.Context,
		id, status string,
		reposChecked, ticketsCreated, ticketsSuperseded int64,
		errorsJSON string,
	) (*store.PollRun, error)
	insertEventFn func(ctx context.Context, pollRunID string, monitoredRepoID *string, action string, detail *string) (*store.PollRunEvent, error)
}

func (m *schedulerMockPollRepo) UpdatePollState(context.Context, string, store.PollStateUpdate) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockPollRepo) InsertRun(ctx context.Context) (*store.PollRun, error) {
	if m.insertRunFn != nil {
		return m.insertRunFn(ctx)
	}
	return &store.PollRun{ID: "run-1", Status: "running"}, nil
}

func (m *schedulerMockPollRepo) FinishRun(
	ctx context.Context,
	id, status string,
	reposChecked, ticketsCreated, ticketsSuperseded int64,
	errorsJSON string,
) (*store.PollRun, error) {
	if m.finishRunFn != nil {
		return m.finishRunFn(ctx, id, status, reposChecked, ticketsCreated, ticketsSuperseded, errorsJSON)
	}
	return &store.PollRun{
		ID:                id,
		Status:            status,
		ReposChecked:      reposChecked,
		TicketsCreated:    ticketsCreated,
		TicketsSuperseded: ticketsSuperseded,
		ErrorsJSON:        errorsJSON,
	}, nil
}

func (m *schedulerMockPollRepo) GetRun(context.Context, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockPollRepo) ListRuns(context.Context, int64, int64) ([]store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockPollRepo) InsertEvent(ctx context.Context, pollRunID string, monitoredRepoID *string, action string, detail *string) (*store.PollRunEvent, error) {
	if m.insertEventFn != nil {
		return m.insertEventFn(ctx, pollRunID, monitoredRepoID, action, detail)
	}
	return &store.PollRunEvent{ID: "event-1"}, nil
}

func (m *schedulerMockPollRepo) ListEventsByRunID(context.Context, string) ([]store.PollRunEvent, error) {
	return nil, errors.New("not implemented")
}

type schedulerMockSettingsRepo struct {
	pollIntervalMinutes int64
}

func (m *schedulerMockSettingsRepo) Get(context.Context) (*store.AppSettings, error) {
	return &store.AppSettings{PollIntervalMinutes: m.pollIntervalMinutes}, nil
}

func (m *schedulerMockSettingsRepo) UpdatePollInterval(context.Context, int64) (*store.AppSettings, error) {
	return nil, errors.New("not implemented")
}

type schedulerMockReposRepo struct {
	repos []store.MonitoredRepo
}

func (m *schedulerMockReposRepo) Create(context.Context, store.CreateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockReposRepo) Get(context.Context, string) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockReposRepo) List(context.Context) ([]store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockReposRepo) ListEnabled(context.Context) ([]store.MonitoredRepo, error) {
	return m.repos, nil
}

func (m *schedulerMockReposRepo) Update(context.Context, string, store.UpdateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockReposRepo) SetEnabled(context.Context, string, bool) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *schedulerMockReposRepo) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func TestClampPollIntervalMinutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want int64
	}{
		{1, poll.MinPollIntervalMinutes},
		{4, poll.MinPollIntervalMinutes},
		{5, 5},
		{360, 360},
	}
	for _, tc := range cases {
		if got := poll.ClampPollIntervalMinutes(tc.in); got != tc.want {
			t.Errorf("ClampPollIntervalMinutes(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSchedulerRunAllIncrementsReposChecked(t *testing.T) {
	t.Parallel()

	repos := []store.MonitoredRepo{
		{ID: "repo-1", Enabled: true},
		{ID: "repo-2", Enabled: true},
	}

	var finishReposChecked int64
	pollRepo := &schedulerMockPollRepo{
		finishRunFn: func(_ context.Context, _ string, _ string, reposChecked int64, _, _ int64, _ string) (*store.PollRun, error) {
			finishReposChecked = reposChecked
			return &store.PollRun{ReposChecked: reposChecked}, nil
		},
	}

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(pollRepo),
		Repos:  &schedulerMockReposRepo{repos: repos},
		Poll:   pollRepo,
		PollRepo: func(_ context.Context, _ string, repo store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			return &poll.RepoEvaluation{Actions: []string{poll.ActionSkip}, Repo: &repo}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if err := scheduler.RunAll(context.Background(), "run-1"); err != nil {
		t.Fatalf("RunAll: %v", err)
	}
	if finishReposChecked != 2 {
		t.Fatalf("repos_checked = %d, want 2", finishReposChecked)
	}
}

func TestSchedulerTriggerReturnsAlreadyRunning(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(&schedulerMockPollRepo{}),
		Poll:   &schedulerMockPollRepo{},
		PollRepo: func(ctx context.Context, _ string, _ store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return &poll.RepoEvaluation{Actions: []string{poll.ActionSkip}}, nil
		},
		Repos: &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	firstID, err := scheduler.Trigger(context.Background())
	if err != nil {
		t.Fatalf("first Trigger: %v", err)
	}
	if firstID == "" {
		t.Fatal("first Trigger returned empty run id")
	}

	<-started

	_, err = scheduler.Trigger(context.Background())
	if !errors.Is(err, poll.ErrAlreadyRunning) {
		t.Fatalf("second Trigger error = %v, want ErrAlreadyRunning", err)
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for scheduler.IsPolling() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if scheduler.IsPolling() {
		t.Fatal("scheduler still polling after run completed")
	}
}

func TestSchedulerMutexAllowsSequentialRuns(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32
	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(&schedulerMockPollRepo{}),
		Poll:   &schedulerMockPollRepo{},
		Repos:  &schedulerMockReposRepo{repos: []store.MonitoredRepo{{ID: "repo-1"}}},
		PollRepo: func(context.Context, string, store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			runs.Add(1)
			time.Sleep(20 * time.Millisecond)
			return &poll.RepoEvaluation{Actions: []string{poll.ActionSkip}}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = scheduler.Trigger(context.Background())
		}()
	}
	wg.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for scheduler.IsPolling() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := runs.Load(); got != 1 {
		t.Fatalf("completed runs = %d, want 1 (mutex should serialize)", got)
	}
}

func TestSchedulerStartUsesMinInterval(t *testing.T) {
	t.Parallel()

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine:   poll.NewEngine(&schedulerMockPollRepo{}),
		Settings: &schedulerMockSettingsRepo{pollIntervalMinutes: 2},
		Poll:     &schedulerMockPollRepo{},
		Repos:    &schedulerMockReposRepo{},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Schedule reload clamps 2 → 5; no panic means cron accepted the expression.
	cancel()
	time.Sleep(50 * time.Millisecond)
}
