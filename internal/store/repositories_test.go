package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

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
