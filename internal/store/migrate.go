package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"

	"github.com/mdg-labs/release-ops/migrations"
)

// retainedSnapshots is how many pre-migration snapshots stay on disk.
const retainedSnapshots = 3

// Migrate applies the embedded migrations to the database at dbPath and returns
// the ones it applied. Snapshots go to a snapshots directory next to the database.
func Migrate(ctx context.Context, dbPath string) ([]sqlitemigrate.Migration, error) {
	all, err := sqlitemigrate.LoadDir(ctx, migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("load migrations: %w", err)
	}
	return applyMigrations(ctx, dbPath, all)
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
