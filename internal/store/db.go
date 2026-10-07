package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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

// Open opens app.db using modernc.org/sqlite with foreign keys enabled.
func Open() (*sql.DB, error) {
	return OpenPath(DBPath())
}

// OpenPath opens the SQLite database at path with foreign keys enabled.
func OpenPath(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db parent dir: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?cache=shared&_foreign_keys=on", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return db, nil
}
