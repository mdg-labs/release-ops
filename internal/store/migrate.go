package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
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

// removeLocalAccountsSlug names the migration that narrows auth_tokens.kind to the setup kind.
// The rebuild it generates copies every row into a table that rejects the other kinds, so the
// rows of the removed kinds are deleted first, in purgeRemovedTokenKinds.
const removeLocalAccountsSlug = "remove_local_accounts"

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

	for _, m := range pending {
		if m.Slug == removeLocalAccountsSlug {
			if err := purgeRemovedTokenKinds(ctx, dbPath); err != nil {
				return nil, fmt.Errorf("remove one-time tokens of the removed local accounts: %w", err)
			}
		}
	}

	done, err := runner.Apply(ctx, all)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return done, nil
}

// purgeRemovedTokenKinds deletes the auth_tokens rows whose kind is not setup: the invitation,
// password-reset and email-change tokens of the local accounts, which nothing redeems any more.
// It is the one data step the remove_local_accounts migration needs, and it runs before it so
// that the rebuilt table, which allows the setup kind only, accepts every row it is given. The
// delete is idempotent: a migration that fails afterwards is retried with nothing left to delete.
func purgeRemovedTokenKinds(ctx context.Context, dbPath string) error {
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// The journal mode is left as it is: switching it is OpenPath's job, not a migration step's.
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillis))
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: escapePath(dbPath), RawQuery: q.Encode()}).String())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var exists int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'auth_tokens'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	_, err = db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE kind <> 'setup'`)
	return err
}
