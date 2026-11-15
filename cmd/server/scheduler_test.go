package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

// The server runs the safety-net scheduler: with the interval shortened, a tick queues one
// normal-priority renovate_run job for each repository that takes part, and none for a paused
// one or one recorded as having no config, however many ticks pass.
func TestServerSchedulesOneJobPerEligibleRepository(t *testing.T) {
	interval := schedulerInterval
	t.Cleanup(func() { schedulerInterval = interval })
	schedulerInterval = 50 * time.Millisecond

	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ids := storetest.SeedRepositories(t, db, 3)
	if _, err := db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE id = ?`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer.Exec(`UPDATE repositories SET config_state = 'missing' WHERE id = ?`, ids[2]); err != nil {
		t.Fatal(err)
	}

	startServer(t, &config.Config{AppDBPath: dbPath, AppURL: "https://release-ops.example.com"})

	jobs := func() (n int, repository, priority, trigger, kind string) {
		_ = db.Reader.QueryRowContext(context.Background(),
			`SELECT COUNT(*), COALESCE(MIN(repository_id), ''), COALESCE(MIN(priority), ''), COALESCE(MIN("trigger"), ''), COALESCE(MIN(kind), '') FROM jobs`).
			Scan(&n, &repository, &priority, &trigger, &kind)
		return
	}
	waitFor(t, "the scheduler to queue the eligible repository", func() bool {
		n, _, _, _, _ := jobs()
		return n > 0
	})
	time.Sleep(10 * schedulerInterval)
	n, repository, priority, trigger, kind := jobs()
	if n != 1 || repository != ids[0] || priority != "normal" || trigger != "schedule" || kind != "renovate_run" {
		t.Fatalf("jobs after several ticks = %d (%s %s %s %s), want one normal schedule renovate_run job for %s", n, repository, priority, trigger, kind, ids[0])
	}
}
