package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/mdguggenbichler/release-ops/internal/store"
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
	if version != 1 {
		t.Fatalf("migration version = %d, want 1", version)
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
