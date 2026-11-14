package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"
)

// An in-memory database cannot use WAL; SQLite reports the mode it kept instead of failing.
func TestRequireWALFailsWhenJournalModeIsNotWAL(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		t.Fatalf("PRAGMA journal_mode=WAL: %v", err)
	}
	err = requireWAL(db)
	if err == nil || !strings.Contains(err.Error(), "journal_mode") {
		t.Fatalf("requireWAL = %v, want an error naming journal_mode", err)
	}
}

// Migrations run on their own connection against a database the server has already
// switched to WAL, and the pre-migration snapshot (VACUUM INTO) must still work.
func TestMigrateAppliesPendingMigrationToWALDatabase(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	baseline := loadEmbedded(t, ctx)
	if _, err := applyMigrations(ctx, dbPath, baseline); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	snapshotsBefore := snapshotCount(t, dbPath)

	db, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	next := testMigration(t, ctx, "20990101000000_add_probe.sql", "CREATE TABLE wal_probe (id TEXT PRIMARY KEY) STRICT;\n")
	applied, err := applyMigrations(ctx, dbPath, append(append([]sqlitemigrate.Migration{}, baseline...), next))
	if err != nil {
		t.Fatalf("apply migration on WAL database: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d migrations, want 1", len(applied))
	}
	if got := snapshotCount(t, dbPath); got != snapshotsBefore+1 {
		t.Fatalf("snapshots = %d, want %d: the pending migration takes one", got, snapshotsBefore+1)
	}
}
