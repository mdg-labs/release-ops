package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

func (e *jobEnv) inTx(t *testing.T, commit bool, fn func(tx *sql.Tx)) {
	t.Helper()
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fn(tx)
	if commit {
		err = tx.Commit()
	} else {
		err = tx.Rollback()
	}
	if err != nil {
		t.Fatal(err)
	}
}

func (e *jobEnv) enqueueTx(t *testing.T, tx *sql.Tx, repo string, prio queue.Priority) (queue.EnqueueResult, func()) {
	t.Helper()
	res, wake, err := e.q.EnqueueTx(context.Background(), tx, queue.EnqueueParams{
		RepositoryID: repo, Priority: prio, Trigger: queue.TriggerDashboardCheckbox, MatchedMarkers: []string{"rebase-check"},
	})
	if err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	if wake == nil {
		t.Fatal("EnqueueTx returned a nil wake-up function with no error")
	}
	return res, wake
}

func TestEnqueueTxRecordsTheJobOnlyWhenTheCallerCommits(t *testing.T) {
	e := newJobEnv(t, 1)

	e.inTx(t, false, func(tx *sql.Tx) { e.enqueueTx(t, tx, e.repos[0], queue.PriorityHigh) })
	if n := e.count(t, `1 = 1`); n != 0 {
		t.Fatalf("%d jobs after a rollback, want none", n)
	}
	e.wantNoClaim(t)

	var res queue.EnqueueResult
	e.inTx(t, true, func(tx *sql.Tx) { res, _ = e.enqueueTx(t, tx, e.repos[0], queue.PriorityHigh) })
	if res.Outcome != queue.EnqueueQueued {
		t.Fatalf("outcome = %q, want queued", res.Outcome)
	}
	job := e.claim(t)
	if job.ID != res.JobID || job.Priority != queue.PriorityHigh || job.Trigger != queue.TriggerDashboardCheckbox {
		t.Errorf("claimed %+v, want the committed high-priority dashboard_checkbox job", job)
	}
}

func TestEnqueueTxWakesWaitersOnlyWhenTheCallerRunsTheWakeUp(t *testing.T) {
	e := newJobEnv(t, 1)

	changed := e.q.Changed()
	var wake func()
	e.inTx(t, true, func(tx *sql.Tx) { _, wake = e.enqueueTx(t, tx, e.repos[0], queue.PriorityHigh) })
	select {
	case <-changed:
		t.Fatal("a waiter was woken by the commit alone: the wake-up is the caller's to run after it")
	default:
	}

	wake()
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("the wake-up did not wake a waiter")
	}
	if job := e.claim(t); job == nil {
		t.Fatal("a woken claim found no job")
	}
}

func TestEnqueueTxWakeUpIsANoOpForADroppedOrFlaggedTrigger(t *testing.T) {
	e := newJobEnv(t, 1)
	e.enqueue(t, e.repos[0], queue.PriorityHigh)
	e.claim(t)

	changed := e.q.Changed()
	e.inTx(t, true, func(tx *sql.Tx) {
		res, wake := e.enqueueTx(t, tx, e.repos[0], queue.PriorityHigh)
		if res.Outcome != queue.EnqueueRerunRequested {
			t.Fatalf("outcome = %q, want rerun_requested", res.Outcome)
		}
		wake()
	})
	select {
	case <-changed:
		t.Fatal("a rerun flag woke the waiters, though no job became claimable")
	default:
	}
	if n := e.count(t, `rerun_requested = 1`); n != 1 {
		t.Errorf("%d jobs flagged, want the running one", n)
	}
}

func TestEnqueueTxKeepsEveryGuardOfEnqueue(t *testing.T) {
	e := newJobEnv(t, 2)
	if _, err := e.db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE id = ?`, e.repos[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Writer.Exec(`UPDATE repositories SET removed_at = '2026-11-14T12:00:00.000Z' WHERE id = ?`, e.repos[1]); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		p    queue.EnqueueParams
		want error
	}{
		"paused":      {queue.EnqueueParams{RepositoryID: e.repos[0], Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}, queue.ErrRepositoryPaused},
		"removed":     {queue.EnqueueParams{RepositoryID: e.repos[1], Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}, queue.ErrRepositoryPaused},
		"unknown":     {queue.EnqueueParams{RepositoryID: "missing", Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}, queue.ErrRepositoryNotFound},
		"no id":       {queue.EnqueueParams{Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}, nil},
		"bad prio":    {queue.EnqueueParams{RepositoryID: e.repos[0], Priority: "urgent", Trigger: queue.TriggerPush}, nil},
		"bad trigger": {queue.EnqueueParams{RepositoryID: e.repos[0], Priority: queue.PriorityHigh, Trigger: "whim"}, nil},
	} {
		e.inTx(t, false, func(tx *sql.Tx) {
			_, wake, err := e.q.EnqueueTx(context.Background(), tx, tc.p)
			if err == nil || wake != nil || (tc.want != nil && !errors.Is(err, tc.want)) {
				t.Errorf("%s: EnqueueTx = wake %v, %v; want the error %v and no wake-up", name, wake != nil, err, tc.want)
			}
		})
	}
	if n := e.count(t, `1 = 1`); n != 0 {
		t.Fatalf("%d jobs after refused enqueues, want 0", n)
	}
}

func TestEnqueueTxCoalescesIntoTheQueuedJobInTheSameTransaction(t *testing.T) {
	e := newJobEnv(t, 1)

	e.inTx(t, true, func(tx *sql.Tx) {
		first, _ := e.enqueueTx(t, tx, e.repos[0], queue.PriorityNormal)
		second, _ := e.enqueueTx(t, tx, e.repos[0], queue.PriorityHigh)
		if first.Outcome != queue.EnqueueQueued || second.Outcome != queue.EnqueueCoalesced || first.JobID != second.JobID {
			t.Errorf("results = %+v, %+v, want a second trigger folded into the first job", first, second)
		}
	})
	if n := e.count(t, `priority = 'high'`); n != 1 {
		t.Errorf("%d high jobs, want the coalesced one raised to high", n)
	}
}

func TestRepositoryByGitHubIDReadsThroughTheTransaction(t *testing.T) {
	e := newJobEnv(t, 2)
	st := store.New(e.db, nil)
	ctx := context.Background()

	if _, err := e.db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE id = ?`, e.repos[1]); err != nil {
		t.Fatal(err)
	}
	e.inTx(t, false, func(tx *sql.Tx) {
		got, err := st.RepositoryByGitHubID(ctx, tx, 1)
		if err != nil || got.ID != e.repos[0] || got.FullName != "acme/repo-1" || got.DefaultBranch != "main" || !got.Enabled {
			t.Errorf("repository 1 = %+v, %v", got, err)
		}
		if got, err := st.RepositoryByGitHubID(ctx, tx, 2); err != nil || got.Enabled {
			t.Errorf("a paused repository = %+v, %v, want it returned with Enabled false", got, err)
		}
		if _, err := st.RepositoryByGitHubID(ctx, tx, 99); !errors.Is(err, store.ErrRepositoryNotFound) {
			t.Errorf("an unknown repository: %v, want ErrRepositoryNotFound", err)
		}
		if _, err := tx.Exec(`UPDATE repositories SET removed_at = '2026-11-14T12:00:00.000Z' WHERE github_repository_id = 1`); err != nil {
			t.Fatal(err)
		}
		if _, err := st.RepositoryByGitHubID(ctx, tx, 1); !errors.Is(err, store.ErrRepositoryNotFound) {
			t.Errorf("a repository removed in this transaction: %v, want ErrRepositoryNotFound", err)
		}
	})
}
