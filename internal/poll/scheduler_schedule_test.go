package poll

import (
	"context"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

func TestReloadScheduleSkipsWhenScheduleUnchanged(t *testing.T) {
	t.Parallel()

	scheduler, err := NewScheduler(SchedulerConfig{
		Engine:   NewEngine(&scheduleTestPollRepo{}),
		Settings: &scheduleTestSettingsRepo{pollIntervalMinutes: 30},
		Poll:     &scheduleTestPollRepo{},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	ctx := context.Background()
	if err := scheduler.reloadSchedule(ctx); err != nil {
		t.Fatalf("first reloadSchedule: %v", err)
	}

	firstEntryID := scheduler.entryID
	firstSpec := scheduler.currentScheduleSpec
	if firstEntryID == 0 {
		t.Fatal("expected cron entry after first reloadSchedule")
	}
	if firstSpec != "@every 30m" {
		t.Fatalf("schedule spec = %q, want @every 30m", firstSpec)
	}

	if err := scheduler.reloadSchedule(ctx); err != nil {
		t.Fatalf("second reloadSchedule: %v", err)
	}
	if scheduler.entryID != firstEntryID {
		t.Fatal("cron entry replaced when schedule unchanged")
	}
	if scheduler.currentScheduleSpec != firstSpec {
		t.Fatalf("schedule spec changed to %q", scheduler.currentScheduleSpec)
	}
}

type scheduleTestSettingsRepo struct {
	pollIntervalMinutes int64
}

func (m *scheduleTestSettingsRepo) EnsureDefault(context.Context) error {
	return nil
}

func (m *scheduleTestSettingsRepo) Get(context.Context) (*store.AppSettings, error) {
	return &store.AppSettings{PollIntervalMinutes: m.pollIntervalMinutes}, nil
}

func (m *scheduleTestSettingsRepo) UpdatePollInterval(context.Context, int64) (*store.AppSettings, error) {
	return nil, errors.New("not implemented")
}

func (m *scheduleTestSettingsRepo) UpdateTokenExpiry(context.Context, int64, int64) (*store.AppSettings, error) {
	return nil, errors.New("not implemented")
}

type scheduleTestPollRepo struct{}

func (m *scheduleTestPollRepo) UpdatePollState(context.Context, string, store.PollStateUpdate) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *scheduleTestPollRepo) InsertRun(context.Context) (*store.PollRun, error) {
	return &store.PollRun{ID: "run-1", Status: "running"}, nil
}

func (m *scheduleTestPollRepo) FinishRun(
	context.Context, string, string, int64, int64, int64, string,
) (*store.PollRun, error) {
	return &store.PollRun{ID: "run-1", Status: "success"}, nil
}

func (m *scheduleTestPollRepo) GetRun(context.Context, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *scheduleTestPollRepo) ListRuns(context.Context, int64, int64) ([]store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *scheduleTestPollRepo) InsertEvent(context.Context, string, *string, string, *string) (*store.PollRunEvent, error) {
	return &store.PollRunEvent{ID: "event-1"}, nil
}

func (m *scheduleTestPollRepo) ListEventsByRunID(context.Context, string) ([]store.PollRunEvent, error) {
	return nil, errors.New("not implemented")
}
