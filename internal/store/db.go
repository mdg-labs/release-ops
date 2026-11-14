package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	_ "modernc.org/sqlite"
)

const defaultAppDBPath = "/data/app.db"

// DBPath returns the SQLite file path from APP_DB_PATH or the deployment default.
func DBPath() string {
	if path := os.Getenv("APP_DB_PATH"); path != "" {
		return path
	}
	return defaultAppDBPath
}

// busyTimeoutMillis is how long a statement waits for a lock held by another connection
// (a backup, the sqlite3 CLI) before it fails with SQLITE_BUSY.
const busyTimeoutMillis = 5000

// minReadConns is the smallest read pool, so a few concurrent reads never queue
// behind each other on a small host.
const minReadConns = 4

// DB is the app's SQLite handle: one writer connection and a pool of read-only
// connections to the same WAL database. Writes and transactions use Writer; plain
// reads use Reader, so a read never waits for the writer's connection.
type DB struct {
	// Writer has a single connection. A transaction on it holds that connection until
	// it ends, so code inside the transaction reads through the transaction, never
	// through Writer again.
	Writer *sql.DB
	// Reader is read-only; a write on it fails.
	Reader *sql.DB
}

// Close closes both pools, returning the first error.
func (d *DB) Close() error {
	return errors.Join(d.Reader.Close(), d.Writer.Close())
}

// Open opens app.db at DBPath.
func Open() (*DB, error) {
	return OpenPath(DBPath())
}

// OpenPath opens the SQLite database at path in WAL mode: a writer with a single
// connection, then a read-only pool. Migrations run beforehand on their own connection.
// The data directory must be a local filesystem: WAL keeps its index in shared memory.
func OpenPath(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db parent dir: %w", err)
	}

	writer, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	writer.SetMaxOpenConns(1)

	if err := writer.Ping(); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping sqlite writer: %w", err)
	}
	if err := requireWAL(writer); err != nil {
		_ = writer.Close()
		return nil, err
	}

	reader, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open sqlite reader: %w", err)
	}
	reader.SetMaxOpenConns(max(minReadConns, runtime.NumCPU()))

	if err := reader.Ping(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, fmt.Errorf("ping sqlite reader: %w", err)
	}

	return &DB{Writer: writer, Reader: reader}, nil
}

// dsn builds the connection string. Every pragma is applied to each connection the
// pool opens. journal_mode is stored in the file, so it converts a rollback-journal
// database on the writer's first connection; the read-only pool never changes it.
func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeoutMillis))
	q.Add("_pragma", "foreign_keys(1)")
	if readOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	} else {
		q.Add("_pragma", "journal_mode(WAL)")
		q.Add("_pragma", "synchronous(NORMAL)")
	}
	return (&url.URL{Scheme: "file", Opaque: escapePath(path), RawQuery: q.Encode()}).String()
}

// escapePath percent-encodes the characters that would end the path of a file: URI.
func escapePath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// requireWAL fails when the database did not switch to WAL, as on a filesystem
// without shared-memory support, where SQLite silently keeps its old journal mode.
func requireWAL(db *sql.DB) error {
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		return fmt.Errorf("read journal_mode: %w", err)
	}
	if !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("sqlite journal_mode is %q, want wal: the data directory must be a local filesystem", mode)
	}
	return nil
}
