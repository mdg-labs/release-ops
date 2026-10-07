package poll

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/mdg-labs/release-ops/internal/store"
)

// Run status values persisted on poll_runs (specs §4.6).
const (
	RunStatusRunning = "running"
	RunStatusSuccess = "success"
	RunStatusPartial = "partial"
	RunStatusFailed  = "failed"
)

// RunErrorEntry is one element of poll_runs.errors_json (specs §4.6).
type RunErrorEntry struct {
	RepoID  string `json:"repoId"`
	Message string `json:"message"`
}

// RunRecorder persists poll_run_events and aggregates counters for a single run.
type RunRecorder struct {
	poll     store.PollRepository
	notifier *Notifier
}

// NewRunRecorder returns a recorder that writes events via pollRepo.
func NewRunRecorder(pollRepo store.PollRepository, notifier *Notifier) *RunRecorder {
	return &RunRecorder{poll: pollRepo, notifier: notifier}
}

// RecordEvaluation inserts poll_run_events for each action and updates counter totals.
func (r *RunRecorder) RecordEvaluation(
	ctx context.Context,
	runID, repoID string,
	eval *RepoEvaluation,
	ticketsCreated, ticketsSuperseded *int64,
) {
	if eval == nil {
		return
	}

	repoIDPtr := repoID
	for _, action := range eval.Actions {
		var detail *string
		if eval.Detail != "" {
			d := eval.Detail
			detail = &d
		}
		if _, err := r.poll.InsertEvent(ctx, runID, &repoIDPtr, action, detail); err != nil {
			slog.Error("insert poll run event", "runId", runID, "repoId", repoID, "action", action, "error", err)
		}
		created, superseded := CounterDeltasForAction(action)
		*ticketsCreated += created
		*ticketsSuperseded += superseded
	}

	if r.notifier != nil && eval.Repo != nil {
		for _, action := range eval.Actions {
			r.notifier.NotifyRepoAction(ctx, *eval.Repo, action, eval.Detail)
		}
	}
}

// CounterDeltasForAction returns tickets_created and tickets_superseded increments for one action.
func CounterDeltasForAction(action string) (ticketsCreated, ticketsSuperseded int64) {
	switch action {
	case ActionCreate:
		return 1, 0
	case ActionSupersede:
		return 0, 1
	default:
		return 0, 0
	}
}

// EncodeRunErrors marshals run errors for poll_runs.errors_json.
func EncodeRunErrors(entries []RunErrorEntry) (string, error) {
	if entries == nil {
		entries = []RunErrorEntry{}
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// RunFinishStatus maps repo/error counts to poll_runs.status (specs §5.6).
func RunFinishStatus(reposChecked, errorCount int64) string {
	if reposChecked == 0 || errorCount == 0 {
		return RunStatusSuccess
	}
	if errorCount >= reposChecked {
		return RunStatusFailed
	}
	return RunStatusPartial
}
