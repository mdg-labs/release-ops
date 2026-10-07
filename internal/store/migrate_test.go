package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/migrations"
	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"
	_ "modernc.org/sqlite"
)

var wantTables = []string{
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

func TestMigrateCreatesStrictSchemaOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")

	applied, err := Migrate(ctx, dbPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied on empty db = %d migrations, want 1", len(applied))
	}
	snapshotsAfterFirst := snapshotCount(t, dbPath)

	db, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range wantTables {
		var strict, count int
		if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(strict), 0) FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&count, &strict); err != nil {
			t.Fatalf("table_list %q: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %q missing after migrate", table)
		}
		if strict != 1 {
			t.Fatalf("table %q is not STRICT", table)
		}
	}

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("pragma foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	again, err := Migrate(ctx, dbPath)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Migrate applied %d migrations, want 0", len(again))
	}
	if got := snapshotCount(t, dbPath); got != snapshotsAfterFirst {
		t.Fatalf("snapshots after no-op Migrate = %d, want %d", got, snapshotsAfterFirst)
	}
}

func TestSessionsUserIDForeignKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	if _, err := Migrate(ctx, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	db, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	insertSession := func(token string, userID any) error {
		_, err := db.Exec(
			`INSERT INTO sessions (token, data, expiry, user_id) VALUES (?, x'00', 2460000.5, ?)`,
			token, userID,
		)
		return err
	}

	if err := insertSession("t-unknown", "no-such-user"); err == nil {
		t.Fatal("session with unknown user_id accepted, want foreign key failure")
	}
	if err := insertSession("t-anon", nil); err != nil {
		t.Fatalf("session without user_id: %v", err)
	}

	if _, err := db.Exec(
		`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u1', 'u1@example.com', 'x', 't', 't')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := insertSession("t-known", "u1"); err != nil {
		t.Fatalf("session with known user_id: %v", err)
	}

	if _, err := db.Exec(`DELETE FROM sessions WHERE user_id = 'u1'`); err != nil {
		t.Fatalf("delete user sessions: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
		t.Fatalf("delete user after its sessions: %v", err)
	}
}

func TestApplyMigrationsFailureLeavesDatabaseUnchanged(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	baseline := loadEmbedded(t, ctx)

	if _, err := applyMigrations(ctx, dbPath, baseline); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}

	broken := testMigration(t, ctx, "20990101000000_broken.sql",
		"CREATE TABLE half_applied (id TEXT PRIMARY KEY) STRICT;\nINSERT INTO missing_table (id) VALUES ('x');\n")
	_, err := applyMigrations(ctx, dbPath, append(append([]sqlitemigrate.Migration{}, baseline...), broken))
	if err == nil {
		t.Fatal("broken migration applied without error")
	}
	if !strings.Contains(err.Error(), "missing_table") {
		t.Fatalf("error = %q, want it to name the failing statement's cause", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var halfApplied, recorded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'half_applied'`).Scan(&halfApplied); err != nil {
		t.Fatalf("lookup half_applied: %v", err)
	}
	if halfApplied != 0 {
		t.Fatal("table from the failed migration exists, want the whole batch rolled back")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&recorded); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	if recorded != len(baseline) {
		t.Fatalf("recorded migrations = %d, want %d", recorded, len(baseline))
	}

	if _, err := applyMigrations(ctx, dbPath, append(append([]sqlitemigrate.Migration{}, baseline...), broken)); err == nil {
		t.Fatal("next start with the broken migration succeeded, want the same error")
	}
}

func TestApplyMigrationsRejectsEditedAppliedMigration(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	baseline := loadEmbedded(t, ctx)
	if _, err := applyMigrations(ctx, dbPath, baseline); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}

	edited := testMigration(t, ctx, baseline[0].Filename, baseline[0].SQL+"\n-- edited\n")
	_, err := applyMigrations(ctx, dbPath, []sqlitemigrate.Migration{edited})
	var mismatch *sqlitemigrate.ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want ChecksumMismatchError", err)
	}

	_, err = applyMigrations(ctx, dbPath, nil)
	var missing *sqlitemigrate.MissingMigrationError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %v, want MissingMigrationError", err)
	}
}

func TestOpenUsesAppDBPathEnv(t *testing.T) {
	t.Setenv("APP_DB_PATH", filepath.Join(t.TempDir(), "env.db"))

	db, err := Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if DBPath() != os.Getenv("APP_DB_PATH") {
		t.Fatalf("DBPath() = %q, want %q", DBPath(), os.Getenv("APP_DB_PATH"))
	}
}

func loadEmbedded(t *testing.T, ctx context.Context) []sqlitemigrate.Migration {
	t.Helper()

	all, err := sqlitemigrate.LoadDir(ctx, migrations.FS, ".")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no embedded migrations")
	}
	return all
}

func testMigration(t *testing.T, ctx context.Context, filename, body string) sqlitemigrate.Migration {
	t.Helper()

	m, err := sqlitemigrate.Load(ctx, filename, strings.NewReader(body))
	if err != nil {
		t.Fatalf("Load %s: %v", filename, err)
	}
	return m
}

func snapshotCount(t *testing.T, dbPath string) int {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(filepath.Dir(dbPath), "snapshots"))
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	return len(entries)
}
