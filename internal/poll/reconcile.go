package poll

import (
	"context"
	"fmt"

	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	interruptedRunMessage = "poll run was interrupted before it finished"
	reconcilePageSize     = 200
)

// ReconcileInterruptedRuns marks every poll_runs row still running as failed, with an
// error entry saying the run was interrupted, and returns how many it changed. It is
// meant to run once at startup, before the scheduler can start a run: a row still
// running at that point belongs to a process that no longer exists.
func ReconcileInterruptedRuns(ctx context.Context, polls store.PollRepository) (int, error) {
	errorsJSON, err := EncodeRunErrors([]RunErrorEntry{{Message: interruptedRunMessage}})
	if err != nil {
		return 0, fmt.Errorf("encode interrupted run error: %w", err)
	}

	var stale []store.PollRun
	for offset := int64(0); ; offset += reconcilePageSize {
		runs, err := polls.ListRuns(ctx, reconcilePageSize, offset)
		if err != nil {
			return 0, fmt.Errorf("list poll runs: %w", err)
		}
		for _, run := range runs {
			if run.Status == RunStatusRunning {
				stale = append(stale, run)
			}
		}
		if len(runs) < reconcilePageSize {
			break
		}
	}

	for i, run := range stale {
		if _, err := polls.FinishRun(ctx, run.ID, RunStatusFailed, run.ReposChecked, run.TicketsCreated, run.TicketsSuperseded, errorsJSON); err != nil {
			return i, fmt.Errorf("finish interrupted poll run %s: %w", run.ID, err)
		}
	}
	return len(stale), nil
}
