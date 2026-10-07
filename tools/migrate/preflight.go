package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "modernc.org/sqlite"
)

// kaneoRenameVersion is migration 000007_rename_phasical_to_kaneo, which drops the
// 'phasical' integration kind from the CHECK constraint.
const kaneoRenameVersion = 7

// preflightUp refuses to run `up` when a pending migration would abort on existing data,
// with a message that says how to fix it, instead of a bare CHECK constraint error that
// also leaves schema_migrations dirty.
func preflightUp(m *migrate.Migrate, databaseURL string) error {
	version, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	if version > kaneoRenameVersion || (version == kaneoRenameVersion && !dirty) {
		return nil
	}

	db, err := sql.Open("sqlite", sqlitePath(databaseURL))
	if err != nil {
		return fmt.Errorf("open database for preflight: %w", err)
	}
	defer func() { _ = db.Close() }()

	return checkPhasicalIntegrations(db)
}

func checkPhasicalIntegrations(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'integrations'`).Scan(&tables); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	if tables == 0 {
		return nil
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM integrations WHERE kind = 'phasical'`).Scan(&count); err != nil {
		return fmt.Errorf("preflight: %w", err)
	}
	if count == 0 {
		return nil
	}
	return fmt.Errorf(
		"cannot upgrade: %d Phasical integration(s) found. Phasical was replaced by Kaneo and "+
			"migration 000007 cannot keep them. Delete the Phasical integrations (and the ticket "+
			"projects and monitored repos that use them) with the previous version, or follow "+
			"README \"Upgrading\" (Phasical → Kaneo), then start again", count)
}

func sqlitePath(databaseURL string) string {
	path := strings.TrimPrefix(databaseURL, "sqlite://")
	path, _, _ = strings.Cut(path, "?")
	return path
}
