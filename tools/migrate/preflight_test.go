package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPhasicalIntegrations(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := checkPhasicalIntegrations(db); err != nil {
		t.Fatalf("empty database: %v", err)
	}

	if _, err := db.Exec(`CREATE TABLE integrations (id TEXT PRIMARY KEY, kind TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO integrations (id, kind) VALUES ('a', 'jira')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := checkPhasicalIntegrations(db); err != nil {
		t.Fatalf("no phasical rows: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO integrations (id, kind) VALUES ('b', 'phasical')`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	err = checkPhasicalIntegrations(db)
	if err == nil || !strings.Contains(err.Error(), "Phasical") {
		t.Fatalf("err = %v, want Phasical upgrade error", err)
	}
}

func TestSQLitePath(t *testing.T) {
	t.Parallel()

	if got := sqlitePath("sqlite:///data/app.db?_pragma=foreign_keys(1)"); got != "/data/app.db" {
		t.Fatalf("sqlitePath = %q, want /data/app.db", got)
	}
}
