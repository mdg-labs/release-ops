package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mdg-labs/release-ops/internal/store"
	_ "modernc.org/sqlite"
)

func TestMigrateUpCreatesSchema(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	migrationsURL := migrationSourceURL(t)

	m, err := newMigrator(t, dbPath, migrationsURL)
	if err != nil {
		t.Fatalf("newMigrator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = m.Close()
	})

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up: %v", err)
	}

	version, dirty, err := m.Version()
	if err != nil {
		t.Fatalf("migration version: %v", err)
	}
	if dirty {
		t.Fatal("migration version is dirty")
	}
	if version != 4 {
		t.Fatalf("migration version = %d, want 4", version)
	}

	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	wantTables := []string{
		"app_settings",
		"auth_tokens",
		"integrations",
		"monitored_repo_notifications",
		"monitored_repos",
		"notification_targets",
		"poll_run_events",
		"poll_runs",
		"sessions",
		"ticket_projects",
		"users",
	}
	for _, table := range wantTables {
		if !tableExists(t, db, table) {
			t.Fatalf("table %q missing after migrate up", table)
		}
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var triggerSourceExists int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('poll_runs') WHERE name = 'trigger_source'`,
	).Scan(&triggerSourceExists); err != nil {
		t.Fatalf("poll_runs.trigger_source column lookup: %v", err)
	}
	if triggerSourceExists != 1 {
		t.Fatal("poll_runs.trigger_source column missing after migrate up")
	}

	var releasePublishedAtExists int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('monitored_repos') WHERE name = 'last_release_published_at'`,
	).Scan(&releasePublishedAtExists); err != nil {
		t.Fatalf("monitored_repos.last_release_published_at column lookup: %v", err)
	}
	if releasePublishedAtExists != 1 {
		t.Fatal("monitored_repos.last_release_published_at column missing after migrate up")
	}
}

func TestMigrateUpPreservesSeedData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "app.db")
	migrationsURL := migrationSourceURL(t)

	m, err := newMigrator(t, dbPath, migrationsURL)
	if err != nil {
		t.Fatalf("newMigrator: %v", err)
	}
	t.Cleanup(func() {
		_, _ = m.Close()
	})

	if err := m.Migrate(1); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate to version 1: %v", err)
	}

	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open db at version 1: %v", err)
	}

	const (
		settingsUpdatedAt = "2026-01-01T00:00:00Z"
		pollRunID         = "run-seed-1"
		pollEventID       = "evt-seed-1"
	)
	if _, err := db.Exec(
		`INSERT INTO app_settings (id, poll_interval_minutes, updated_at) VALUES (1, 120, ?)`,
		settingsUpdatedAt,
	); err != nil {
		t.Fatalf("seed app_settings: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO poll_runs (
			id, started_at, status, repos_checked, tickets_created, tickets_superseded, errors_json
		) VALUES (?, ?, 'success', 2, 1, 0, '[]')`,
		pollRunID,
		settingsUpdatedAt,
	); err != nil {
		t.Fatalf("seed poll_runs: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO poll_run_events (id, poll_run_id, action, created_at) VALUES (?, ?, 'baseline', ?)`,
		pollEventID,
		pollRunID,
		settingsUpdatedAt,
	); err != nil {
		t.Fatalf("seed poll_run_events: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("migrate up from version 1: %v", err)
	}

	db, err = store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	assertRowCount(t, db, "app_settings", 1)
	assertRowCount(t, db, "poll_runs", 1)
	assertRowCount(t, db, "poll_run_events", 1)

	var pollInterval int
	if err := db.QueryRow(
		`SELECT poll_interval_minutes FROM app_settings WHERE id = 1`,
	).Scan(&pollInterval); err != nil {
		t.Fatalf("read app_settings: %v", err)
	}
	if pollInterval != 120 {
		t.Fatalf("poll_interval_minutes = %d, want 120", pollInterval)
	}

	var triggerSource string
	if err := db.QueryRow(
		`SELECT trigger_source FROM poll_runs WHERE id = ?`,
		pollRunID,
	).Scan(&triggerSource); err != nil {
		t.Fatalf("read poll_runs.trigger_source: %v", err)
	}
	if triggerSource != "scheduled" {
		t.Fatalf("trigger_source = %q, want scheduled", triggerSource)
	}
}

func TestOpenUsesAppDBPathEnv(t *testing.T) {
	t.Setenv("APP_DB_PATH", filepath.Join(t.TempDir(), "env.db"))

	db, err := store.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if store.DBPath() != os.Getenv("APP_DB_PATH") {
		t.Fatalf("DBPath() = %q, want %q", store.DBPath(), os.Getenv("APP_DB_PATH"))
	}
}

func migrationSourceURL(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return "file://" + filepath.Join(root, "migrations")
}

func newMigrator(t *testing.T, dbPath, migrationsURL string) (*migrate.Migrate, error) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	driver, err := sqlite.WithInstance(db, &sqlite.Config{})
	if err != nil {
		return nil, err
	}

	return migrate.NewWithDatabaseInstance(migrationsURL, "sqlite", driver)
}

func assertRowCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s row count = %d, want %d", table, count, want)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()

	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`,
		name,
	).Scan(&count)
	if err != nil {
		t.Fatalf("lookup table %q: %v", name, err)
	}
	return count == 1
}
