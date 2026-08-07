package poll_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/poll"
	"github.com/mdg-labs/release-ops/internal/store"
)

type runTestPollRepo struct {
	events []store.PollRunEvent
}

func (m *runTestPollRepo) UpdatePollState(context.Context, string, store.PollStateUpdate) (*store.MonitoredRepo, error) {
	return nil, errors.New("not implemented")
}

func (m *runTestPollRepo) InsertRun(context.Context) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *runTestPollRepo) FinishRun(context.Context, string, string, int64, int64, int64, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *runTestPollRepo) GetRun(context.Context, string) (*store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *runTestPollRepo) ListRuns(context.Context, int64, int64) ([]store.PollRun, error) {
	return nil, errors.New("not implemented")
}

func (m *runTestPollRepo) InsertEvent(
	_ context.Context,
	pollRunID string,
	monitoredRepoID *string,
	action string,
	detail *string,
) (*store.PollRunEvent, error) {
	event := store.PollRunEvent{
		ID:              "evt-" + action,
		PollRunID:       pollRunID,
		MonitoredRepoID: monitoredRepoID,
		Action:          action,
		Detail:          detail,
		CreatedAt:       "2026-08-07T10:00:00.000Z",
	}
	m.events = append(m.events, event)
	return &event, nil
}

func (m *runTestPollRepo) ListEventsByRunID(context.Context, string) ([]store.PollRunEvent, error) {
	return m.events, nil
}

func TestCounterDeltasForAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		action             string
		wantCreated        int64
		wantSuperseded     int64
	}{
		{poll.ActionCreate, 1, 0},
		{poll.ActionSupersede, 0, 1},
		{poll.ActionBaseline, 0, 0},
		{poll.ActionSkip, 0, 0},
		{poll.ActionMerge, 0, 0},
		{poll.ActionSkipOpen, 0, 0},
		{poll.ActionError, 0, 0},
	}
	for _, tc := range cases {
		created, superseded := poll.CounterDeltasForAction(tc.action)
		if created != tc.wantCreated || superseded != tc.wantSuperseded {
			t.Errorf("CounterDeltasForAction(%q) = (%d, %d), want (%d, %d)",
				tc.action, created, superseded, tc.wantCreated, tc.wantSuperseded)
		}
	}
}

func TestEncodeRunErrors(t *testing.T) {
	t.Parallel()

	raw, err := poll.EncodeRunErrors([]poll.RunErrorEntry{
		{RepoID: "repo-1", Message: "fetch failed"},
	})
	if err != nil {
		t.Fatalf("EncodeRunErrors: %v", err)
	}

	var decoded []poll.RunErrorEntry
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0].RepoID != "repo-1" || decoded[0].Message != "fetch failed" {
		t.Fatalf("decoded = %v, want one repo error", decoded)
	}

	empty, err := poll.EncodeRunErrors(nil)
	if err != nil {
		t.Fatalf("EncodeRunErrors(nil): %v", err)
	}
	if empty != "[]" {
		t.Fatalf("EncodeRunErrors(nil) = %q, want []", empty)
	}
}

func TestRunFinishStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repos, errors int64
		want          string
	}{
		{0, 0, poll.RunStatusSuccess},
		{3, 0, poll.RunStatusSuccess},
		{3, 1, poll.RunStatusPartial},
		{3, 2, poll.RunStatusPartial},
		{3, 3, poll.RunStatusFailed},
	}
	for _, tc := range cases {
		if got := poll.RunFinishStatus(tc.repos, tc.errors); got != tc.want {
			t.Errorf("RunFinishStatus(%d, %d) = %q, want %q", tc.repos, tc.errors, got, tc.want)
		}
	}
}

func TestRunRecorderIncrementsCountersOnSupersede(t *testing.T) {
	t.Parallel()

	repo := store.MonitoredRepo{ID: "repo-1"}
	pollRepo := &runTestPollRepo{}
	recorder := poll.NewRunRecorder(pollRepo, nil)

	var created, superseded int64
	recorder.RecordEvaluation(context.Background(), "run-1", "repo-1", &poll.RepoEvaluation{
		Actions: []string{poll.ActionSupersede, poll.ActionCreate},
		Repo:    &repo,
	}, &created, &superseded)

	if created != 1 {
		t.Fatalf("tickets_created = %d, want 1", created)
	}
	if superseded != 1 {
		t.Fatalf("tickets_superseded = %d, want 1", superseded)
	}
	if len(pollRepo.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(pollRepo.events))
	}
	if pollRepo.events[0].Action != poll.ActionSupersede {
		t.Fatalf("first event action = %q, want supersede", pollRepo.events[0].Action)
	}
	if pollRepo.events[1].Action != poll.ActionCreate {
		t.Fatalf("second event action = %q, want create", pollRepo.events[1].Action)
	}
}

func TestSchedulerRunAllRecordsCountersAndErrorsJSON(t *testing.T) {
	t.Parallel()

	repos := []store.MonitoredRepo{
		{ID: "repo-ok", Enabled: true},
		{ID: "repo-err", Enabled: true},
	}

	var finishStatus, finishErrorsJSON string
	var finishCreated, finishSuperseded int64
	pollRepo := &schedulerMockPollRepo{
		finishRunFn: func(_ context.Context, _ string, status string, _, ticketsCreated, ticketsSuperseded int64, errorsJSON string) (*store.PollRun, error) {
			finishStatus = status
			finishCreated = ticketsCreated
			finishSuperseded = ticketsSuperseded
			finishErrorsJSON = errorsJSON
			return &store.PollRun{Status: status, ErrorsJSON: errorsJSON}, nil
		},
	}

	scheduler, err := poll.NewScheduler(poll.SchedulerConfig{
		Engine: poll.NewEngine(pollRepo),
		Repos:  &schedulerMockReposRepo{repos: repos},
		Poll:   pollRepo,
		PollRepo: func(_ context.Context, _ string, repo store.MonitoredRepo) (*poll.RepoEvaluation, error) {
			switch repo.ID {
			case "repo-ok":
				return &poll.RepoEvaluation{
					Actions: []string{poll.ActionSupersede, poll.ActionCreate},
					Repo:    &repo,
				}, nil
			case "repo-err":
				return &poll.RepoEvaluation{
					Actions: []string{poll.ActionError},
					Repo:    &repo,
					Detail:  "source timeout",
				}, errors.New("source timeout")
			default:
				return nil, errors.New("unexpected repo")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}

	if err := scheduler.RunAll(context.Background(), "run-1"); err != nil {
		t.Fatalf("RunAll: %v", err)
	}

	if finishStatus != poll.RunStatusPartial {
		t.Fatalf("status = %q, want partial", finishStatus)
	}
	if finishCreated != 1 {
		t.Fatalf("tickets_created = %d, want 1", finishCreated)
	}
	if finishSuperseded != 1 {
		t.Fatalf("tickets_superseded = %d, want 1", finishSuperseded)
	}

	var runErrors []poll.RunErrorEntry
	if err := json.Unmarshal([]byte(finishErrorsJSON), &runErrors); err != nil {
		t.Fatalf("unmarshal errors_json: %v", err)
	}
	if len(runErrors) != 1 || runErrors[0].RepoID != "repo-err" {
		t.Fatalf("errors_json = %v, want one error for repo-err", runErrors)
	}
}
