package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

var retentionNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func agoStamp(d time.Duration) string {
	return retentionNow.Add(-d).Format("2006-01-02T15:04:05.000Z")
}

// insertJob inserts a job in the given state with one attempt row and one event row, so the
// cascade of the retention is visible. finished is NULL for a job that has not finished.
func insertJob(t *testing.T, tx *sql.Tx, id, repo, state string, created string, finished any) {
	t.Helper()

	var lease any
	if state == "running" {
		lease = "0.lease"
	}
	if _, err := tx.Exec(
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, lease_owner, lease_expires_at, "trigger", created_at, finished_at)
		 VALUES (?, 'renovate_run', ?, 'normal', ?, ?, 1, ?, ?, 'schedule', ?, ?)`,
		id, repo, state, created, lease, lease, created, finished,
	); err != nil {
		t.Fatalf("insert job %s: %v", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome) VALUES (?, ?, 1, ?, 'failed')`, "run-"+id, id, created); err != nil {
		t.Fatalf("insert job_run of %s: %v", id, err)
	}
	if _, err := tx.Exec(`INSERT INTO job_events (id, job_id, job_run_id, at, kind) VALUES (?, ?, ?, ?, 'retry_scheduled')`, "evt-"+id, id, "run-"+id, created); err != nil {
		t.Fatalf("insert job_event of %s: %v", id, err)
	}
}

func retentionCount(t *testing.T, db *store.DB, query string, args ...any) int {
	t.Helper()

	var n int
	if err := db.Writer.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// The scenario the retention must never produce: a job that is queued for its retry, running or
// leased, or a record still inside its window, deleted because it is old or its siblings are.
func TestPruneNeverDeletesLiveJobsOrRecordsInsideTheirWindow(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	repos := storetest.SeedRepositories(t, db, 3)
	old := 90 * 24 * time.Hour
	tx, err := db.Writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	insertJob(t, tx, "old-succeeded", repos[0], "succeeded", agoStamp(old), agoStamp(old))
	insertJob(t, tx, "old-failed", repos[0], "failed", agoStamp(old), agoStamp(old))
	insertJob(t, tx, "just-expired", repos[0], "succeeded", agoStamp(31*24*time.Hour), agoStamp(31*24*time.Hour))
	insertJob(t, tx, "inside-window", repos[0], "succeeded", agoStamp(29*24*time.Hour), agoStamp(29*24*time.Hour))
	insertJob(t, tx, "old-running", repos[1], "running", agoStamp(old), nil)
	insertJob(t, tx, "old-retry-waiting", repos[2], "queued", agoStamp(old), nil)
	for _, d := range []struct {
		guid string
		age  time.Duration
	}{{"stale", 8 * 24 * time.Hour}, {"fresh", 6 * 24 * time.Hour}} {
		if _, err := tx.Exec(`INSERT INTO webhook_deliveries (guid, event, received_at) VALUES (?, 'push', ?)`, d.guid, agoStamp(d.age)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	res, err := store.NewRetention(db, store.RetentionOptions{Now: func() time.Time { return retentionNow }}).Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Jobs != 3 || res.Deliveries != 1 || res.More {
		t.Fatalf("Prune = %+v, want 3 finished jobs and 1 delivery record deleted and nothing left over", res)
	}

	for _, id := range []string{"old-succeeded", "old-failed", "just-expired"} {
		if n := retentionCount(t, db, `SELECT (SELECT COUNT(*) FROM jobs WHERE id = ?) + (SELECT COUNT(*) FROM job_runs WHERE job_id = ?) + (SELECT COUNT(*) FROM job_events WHERE job_id = ?)`, id, id, id); n != 0 {
			t.Errorf("%s left %d rows behind, want the job and its runs and events deleted", id, n)
		}
	}
	for _, id := range []string{"inside-window", "old-running", "old-retry-waiting"} {
		if n := retentionCount(t, db, `SELECT (SELECT COUNT(*) FROM jobs WHERE id = ?) + (SELECT COUNT(*) FROM job_runs WHERE job_id = ?) + (SELECT COUNT(*) FROM job_events WHERE job_id = ?)`, id, id, id); n != 3 {
			t.Errorf("%s has %d of its 3 rows, want the job, its run and its event kept", id, n)
		}
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM webhook_deliveries WHERE guid = 'fresh'`); n != 1 {
		t.Errorf("the delivery record inside its window was deleted")
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM webhook_deliveries WHERE guid = 'stale'`); n != 0 {
		t.Errorf("the delivery record past its window was kept")
	}
}

// A long history is not scanned at once: a pass deletes at most Batch x MaxBatches rows of each
// kind, oldest first, says there is more, and later passes finish the work.
func TestPruneIsBoundedPerPassOnALargeHistory(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	repos := storetest.SeedRepositories(t, db, 2)
	const jobs, deliveries = 3000, 1500
	tx, err := db.Writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range jobs {
		at := agoStamp(time.Duration(40*24*time.Hour) + time.Duration(jobs-i)*time.Minute)
		insertJob(t, tx, fmt.Sprintf("job-%04d", i), repos[0], "succeeded", at, at)
	}
	insertJob(t, tx, "running-old", repos[1], "running", agoStamp(400*24*time.Hour), nil)
	for i := range deliveries {
		if _, err := tx.Exec(`INSERT INTO webhook_deliveries (guid, event, received_at) VALUES (?, 'push', ?)`, fmt.Sprintf("guid-%04d", i), agoStamp(10*24*time.Hour+time.Duration(deliveries-i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	r := store.NewRetention(db, store.RetentionOptions{Now: func() time.Time { return retentionNow }, Batch: 100, MaxBatches: 5})
	res, err := r.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if res.Jobs != 500 || res.Deliveries != 500 || !res.More {
		t.Fatalf("first pass = %+v, want exactly the cap (500 of each kind) and More", res)
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM jobs`); n != jobs-500+1 {
		t.Errorf("jobs left = %d, want %d", n, jobs-500+1)
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM job_runs`) + retentionCount(t, db, `SELECT COUNT(*) FROM job_events`); n != 2*(jobs-500+1) {
		t.Errorf("runs and events left = %d, want them to go with their jobs", n)
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM jobs WHERE id IN ('job-0000', 'job-0499')`); n != 0 {
		t.Errorf("the oldest jobs were not deleted first")
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM jobs WHERE id = 'job-0500'`); n != 1 {
		t.Errorf("a job beyond the cap was deleted in the first pass")
	}

	passes := 1
	for res.More {
		if res, err = r.Prune(context.Background()); err != nil {
			t.Fatal(err)
		}
		passes++
		if passes > 10 {
			t.Fatal("Prune keeps reporting More")
		}
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM jobs`); n != 1 {
		t.Errorf("jobs after the last pass = %d, want only the running one", n)
	}
	if n := retentionCount(t, db, `SELECT COUNT(*) FROM webhook_deliveries`); n != 0 {
		t.Errorf("delivery records after the last pass = %d, want none", n)
	}
}

// Run prunes once at start, not after a day.
func TestRetentionRunPrunesAtStart(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	repos := storetest.SeedRepositories(t, db, 1)
	tx, err := db.Writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	insertJob(t, tx, "old", repos[0], "succeeded", agoStamp(60*24*time.Hour), agoStamp(60*24*time.Hour))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		store.NewRetention(db, store.RetentionOptions{Now: func() time.Time { return retentionNow }}).Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for retentionCount(t, db, `SELECT COUNT(*) FROM jobs`) != 0 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the first pass did not run at start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}
