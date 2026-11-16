package store_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

func newRepoStore(t *testing.T, e *jobEnv) store.RepositoryRepository {
	t.Helper()
	return store.New(e.db, nil).Repositories()
}

func (e *jobEnv) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := e.db.Writer.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func TestEnqueueRefusesAPausedOrRemovedOrUnknownRepository(t *testing.T) {
	e := newJobEnv(t, 3)
	e.pause(t, e.repos[0])
	e.remove(t, e.repos[1])

	for repo, want := range map[string]error{
		e.repos[0]: queue.ErrRepositoryPaused,
		e.repos[1]: queue.ErrRepositoryPaused,
		"unknown":  queue.ErrRepositoryNotFound,
	} {
		_, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerRunNow})
		if !errors.Is(err, want) {
			t.Errorf("Enqueue(%s) = %v, want %v", repo, err, want)
		}
	}
	var n int
	if err := e.db.Writer.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("jobs = %d, %v; want none", n, err)
	}
	e.enqueue(t, e.repos[2], queue.PriorityNormal)
}

func TestSetEnabledPausesAndDropsOnlyTheQueuedJobOfThatRepository(t *testing.T) {
	e := newJobEnv(t, 3)
	repos := newRepoStore(t, e)
	ctx := context.Background()
	running := e.enqueue(t, e.repos[0], queue.PriorityHigh)
	claimed := e.claim(t)
	if claimed.ID != running.JobID {
		t.Fatalf("claimed %s, want %s", claimed.ID, running.JobID)
	}
	queuedBehindIt := e.enqueue(t, e.repos[0], queue.PriorityHigh)
	other := e.enqueue(t, e.repos[1], queue.PriorityNormal)

	got, err := repos.SetEnabled(ctx, e.repos[0], false)
	if err != nil || got.Enabled || got.ID != e.repos[0] {
		t.Fatalf("SetEnabled(false) = %+v, %v; want the paused repository", got, err)
	}
	if state := e.row(t, other.JobID).State; state != queue.StateQueued {
		t.Errorf("the other repository's job is %q after the first pause, want it still queued", state)
	}
	if untouched, err := repos.Get(ctx, e.repos[1]); err != nil || !untouched.Enabled {
		t.Fatalf("Get(other) after the first pause = %+v, %v; want it still enabled", untouched, err)
	}
	if _, err := repos.SetEnabled(ctx, e.repos[1], false); err != nil {
		t.Fatalf("SetEnabled repo 2: %v", err)
	}

	var ids []string
	rows, err := e.db.Writer.Query(`SELECT id FROM jobs ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	_ = rows.Close()
	if !slices.Equal(ids, []string{running.JobID}) {
		t.Errorf("jobs after the pauses = %v, want only the running job %s (queued %s and %s dropped)", ids, running.JobID, queuedBehindIt.JobID, other.JobID)
	}

	got, err = repos.SetEnabled(ctx, e.repos[0], true)
	if err != nil || !got.Enabled {
		t.Fatalf("SetEnabled(true) = %+v, %v", got, err)
	}
}

func TestPausingKeepsTheAttemptHistoryOfAJobWaitingForItsRetry(t *testing.T) {
	e := newJobEnv(t, 2)
	repos := newRepoStore(t, e)
	ctx := context.Background()

	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	job := e.claim(t)
	run := queue.RunRecord{ExitCode: intPtr(1), ErrorCode: "renovate_failed", ErrorMessage: "boom", ReportStatus: queue.ReportOK, LogExcerpt: "log tail"}
	outcome, err := e.q.Fail(ctx, job.ID, job.LeaseOwner, queue.FailParams{Run: &run})
	if err != nil || outcome != queue.FailRequeued {
		t.Fatalf("Fail = %v, %v; want the job queued for its retry", outcome, err)
	}
	noRuns := e.enqueue(t, e.repos[1], queue.PriorityNormal)

	for _, id := range e.repos {
		if _, err := repos.SetEnabled(ctx, id, false); err != nil {
			t.Fatalf("SetEnabled(%s, false): %v", id, err)
		}
	}

	got := e.row(t, job.ID)
	if got.State != queue.StateFailed || got.Attempts != 1 {
		t.Errorf("job after the pause = %+v, want state failed with its one attempt", got)
	}
	var finishedAt sql.NullString
	if err := e.db.Writer.QueryRow(`SELECT finished_at FROM jobs WHERE id = ?`, job.ID).Scan(&finishedAt); err != nil || !finishedAt.Valid {
		t.Errorf("finished_at = %v, %v; want it set", finishedAt, err)
	}
	runs := e.runs(t, job.ID)
	if len(runs) != 1 || runs[0].Outcome != queue.OutcomeFailed || runs[0].ErrorMessage.String != "boom" || runs[0].LogExcerpt.String != "log tail" {
		t.Errorf("job_runs after the pause = %+v, want the failed attempt intact", runs)
	}
	if n := e.count(t, `state = 'queued'`); n != 0 {
		t.Errorf("queued jobs after the pauses = %d, want none", n)
	}
	var events int
	if err := e.db.Writer.QueryRow(`SELECT COUNT(*) FROM job_events WHERE job_id = ? AND kind = 'retry_scheduled'`, job.ID).Scan(&events); err != nil || events != 1 {
		t.Errorf("retry_scheduled events after the pause = %d, %v; want the one the failed attempt wrote kept", events, err)
	}
	if n := e.count(t, `id = ?`, noRuns.JobID); n != 0 {
		t.Errorf("the queued job without attempts still exists, want it deleted")
	}
}

func TestSetEnabledAndGetRefuseWhatIsNotManageable(t *testing.T) {
	e := newJobEnv(t, 3)
	repos := newRepoStore(t, e)
	ctx := context.Background()
	e.remove(t, e.repos[0])

	for _, id := range []string{e.repos[0], "unknown"} {
		if _, err := repos.Get(ctx, id); !errors.Is(err, store.ErrRepositoryNotFound) {
			t.Errorf("Get(%s) = %v, want ErrRepositoryNotFound", id, err)
		}
		if _, err := repos.SetEnabled(ctx, id, false); !errors.Is(err, store.ErrRepositoryNotFound) {
			t.Errorf("SetEnabled(%s) = %v, want ErrRepositoryNotFound", id, err)
		}
	}
	e.exec(t, `UPDATE installations SET status = 'removed'`)
	if _, err := repos.Get(ctx, e.repos[1]); !errors.Is(err, store.ErrRepositoryNotFound) {
		t.Errorf("Get in a removed installation = %v, want ErrRepositoryNotFound", err)
	}
	if _, err := repos.SetEnabled(ctx, e.repos[1], false); !errors.Is(err, store.ErrRepositoryNotFound) {
		t.Errorf("SetEnabled in a removed installation = %v, want ErrRepositoryNotFound", err)
	}
}

func TestListScheduledKeepsOnlyTheRepositoriesTheSafetyNetEnqueues(t *testing.T) {
	e := newJobEnv(t, 8)
	repos := newRepoStore(t, e)
	r := e.repos
	e.exec(t, `UPDATE repositories SET config_state = 'present' WHERE id = ?`, r[0])
	// r[1] is unknown: it takes part, the config is not yet known to be absent.
	e.pause(t, r[2])
	e.exec(t, `UPDATE repositories SET config_state = 'missing' WHERE id = ?`, r[3])
	e.remove(t, r[4])
	e.enqueue(t, r[5], queue.PriorityNormal)
	e.enqueue(t, r[6], queue.PriorityHigh)
	e.claim(t) // r[6] runs: the high job is claimed first

	got, err := repos.ListScheduled(context.Background())
	if err != nil {
		t.Fatalf("ListScheduled: %v", err)
	}
	if want := []string{r[0], r[1], r[7]}; !slices.Equal(got, want) {
		t.Fatalf("ListScheduled = %v, want %v", got, want)
	}

	e.exec(t, `UPDATE installations SET status = 'suspended'`)
	if got, err := repos.ListScheduled(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("ListScheduled in a suspended installation = %v, %v; want none", got, err)
	}
}

// A queued retry whose attempt left no worker record is kept on a pause all the same: the
// reaper and a result without a run record each leave the attempt's job_runs row.
func TestPausingKeepsAJobWaitingForARetryThatLeftNoWorkerRecord(t *testing.T) {
	for name, end := range map[string]struct {
		do      func(t *testing.T, e *jobEnv, job *queue.ClaimedJob)
		outcome string
	}{
		"a reaped attempt": {outcome: "lease_expired", do: func(t *testing.T, e *jobEnv, _ *queue.ClaimedJob) {
			e.clock.Advance(3 * time.Minute)
			if res, err := e.q.Reap(context.Background()); err != nil || res.Requeued != 1 {
				t.Fatalf("Reap = %+v, %v; want the job requeued", res, err)
			}
		}},
		"a failure reported without a run": {outcome: "failed", do: func(t *testing.T, e *jobEnv, job *queue.ClaimedJob) {
			if out, err := e.q.Fail(context.Background(), job.ID, job.LeaseOwner, queue.FailParams{}); err != nil || out != queue.FailRequeued {
				t.Fatalf("Fail = %v, %v; want the job requeued", out, err)
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			e := newJobEnv(t, 1)
			repos := newRepoStore(t, e)
			e.enqueue(t, e.repos[0], queue.PriorityHigh)
			job := e.claim(t)
			end.do(t, e, job)
			if got := e.row(t, job.ID); got.State != queue.StateQueued || got.Attempts != 1 {
				t.Fatalf("job = %+v, want queued for its retry after one attempt", got)
			}

			if _, err := repos.SetEnabled(context.Background(), e.repos[0], false); err != nil {
				t.Fatalf("SetEnabled(false): %v", err)
			}

			if got := e.row(t, job.ID); got.State != queue.StateFailed || got.Attempts != 1 {
				t.Errorf("job after the pause = %+v, want state failed with its one attempt", got)
			}
			runs := e.runs(t, job.ID)
			if len(runs) != 1 || runs[0].Outcome != end.outcome {
				t.Errorf("job_runs after the pause = %+v, want the %s attempt kept", runs, end.outcome)
			}
		})
	}
}
