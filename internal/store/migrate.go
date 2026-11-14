package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"

	"github.com/mdg-labs/release-ops/migrations"
)

// retainedSnapshots is how many pre-migration snapshots stay on disk.
const retainedSnapshots = 3

// lastV1MigrationVersion is the newest migration a v1 install has. A database whose
// recorded migrations all sit at or below it was created by v1.
const lastV1MigrationVersion = "20261008041322"

// ErrV1Database is returned by Migrate for a database created by Release Ops v1.
var ErrV1Database = errors.New("this database was created by Release Ops v1 and is not upgraded: start v2 on a fresh data volume (an empty APP_DB_PATH directory)")

// Migrate applies the embedded migrations to the database at dbPath and returns
// the ones it applied. Snapshots go to a snapshots directory next to the database.
// A v1 database is refused with ErrV1Database before anything is written.
func Migrate(ctx context.Context, dbPath string) ([]sqlitemigrate.Migration, error) {
	all, err := sqlitemigrate.LoadDir(ctx, migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	if err := refuseV1Database(ctx, dbPath); err != nil {
		return nil, err
	}
	return applyMigrations(ctx, dbPath, all)
}

// refuseV1Database reports ErrV1Database when the database at dbPath has applied
// migrations but none newer than the last v1 one, or still holds a v1 table. It
// only reads: a missing or empty database is not a v1 database.
func refuseV1Database(ctx context.Context, dbPath string) error {
	runner := &sqlitemigrate.Runner{DBPath: dbPath}
	applied, err := runner.Applied(ctx)
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	if len(applied) > 0 && applied[len(applied)-1].Version <= lastV1MigrationVersion {
		return ErrV1Database
	}

	hasV1Table, err := hasTable(ctx, dbPath, "monitored_repos")
	if err != nil {
		return fmt.Errorf("check for v1 tables: %w", err)
	}
	if hasV1Table {
		return ErrV1Database
	}
	return nil
}

// hasTable reports whether the existing database file at dbPath has the named table.
// It opens the file read-only and never creates it.
func hasTable(ctx context.Context, dbPath, name string) (bool, error) {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	db, err := sql.Open("sqlite", dsn(dbPath, true))
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()

	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

// applyMigrations applies the pending subset of all. A database with nothing
// pending is left untouched and takes no snapshot; a checksum mismatch or a
// missing migration file is reported before any write.
func applyMigrations(ctx context.Context, dbPath string, all []sqlitemigrate.Migration) ([]sqlitemigrate.Migration, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db parent dir: %w", err)
	}

	runner := &sqlitemigrate.Runner{
		DBPath:          dbPath,
		SnapshotDir:     filepath.Join(filepath.Dir(dbPath), "snapshots"),
		RetainSnapshots: retainedSnapshots,
	}

	applied, err := runner.Applied(ctx)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	pending, err := sqlitemigrate.PendingMigrations(all, applied)
	if err != nil {
		return nil, fmt.Errorf("check migrations: %w", err)
	}
	if len(pending) == 0 {
		return nil, nil
	}

	done, err := runner.Apply(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return done, nil
}
