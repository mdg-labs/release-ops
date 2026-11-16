package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/migrations"
	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"
	_ "modernc.org/sqlite"
)

var wantTables = []string{
	"auth_identities",
	"auth_tokens",
	"github_app",
	"installation_settings",
	"installations",
	"instance_settings",
	"integrations",
	"job_events",
	"job_runs",
	"jobs",
	"repositories",
	"sessions",
	"ticket_projects",
	"update_pr_dependencies",
	"update_prs",
	"update_tickets",
	"users",
}

var droppedV1Tables = []string{
	"monitored_repo_notifications",
	"monitored_repos",
	"notification_targets",
	"poll_run_events",
	"poll_runs",
}

func TestMigrateCreatesStrictSchemaOnce(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")

	applied, err := Migrate(ctx, dbPath)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if want := len(loadEmbedded(t, ctx)); len(applied) != want {
		t.Fatalf("applied on empty db = %d migrations, want %d", len(applied), want)
	}
	snapshotsAfterFirst := snapshotCount(t, dbPath)

	db, err := OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, table := range wantTables {
		var strict, count int
		if err := db.Writer.QueryRow(`SELECT COUNT(*), COALESCE(SUM(strict), 0) FROM pragma_table_list WHERE schema = 'main' AND name = ?`, table).Scan(&count, &strict); err != nil {
			t.Fatalf("table_list %q: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("table %q missing after migrate", table)
		}
		if strict != 1 {
			t.Fatalf("table %q is not STRICT", table)
		}
	}

	for _, table := range droppedV1Tables {
		var count int
		if err := db.Writer.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatalf("lookup %q: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("v1 table %q still exists after migrate", table)
		}
	}

	var foreignKeys int
	if err := db.Writer.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
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

func TestPollRunEventTicketRefMigrationKeepsExistingEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := v1Migrations(t, loadEmbedded(t, ctx))
	if len(all) < 2 {
		t.Fatalf("v1 migrations = %d, want the baseline plus the ticket ref migration", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:1]); err != nil {
		t.Fatalf("apply baseline: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, stmt := range []string{
		`INSERT INTO poll_runs (id, started_at, status) VALUES ('run-1', 't', 'success')`,
		`INSERT INTO poll_run_events (id, poll_run_id, action, detail, created_at) VALUES ('evt-1', 'run-1', 'create', 'legacy', 't')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	var detail string
	var ticketID, ticketURL, tag sql.NullString
	if err := raw.QueryRow(
		`SELECT detail, ticket_external_id, ticket_url, release_tag FROM poll_run_events WHERE id = 'evt-1'`,
	).Scan(&detail, &ticketID, &ticketURL, &tag); err != nil {
		t.Fatalf("read legacy event: %v", err)
	}
	if detail != "legacy" || ticketID.Valid || ticketURL.Valid || tag.Valid {
		t.Fatalf("legacy event = %q %v %v %v, want kept with NULL ticket ref and tag", detail, ticketID, ticketURL, tag)
	}
}

func TestIntegrationDefaultMigrationKeepsExistingIntegrations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")
	all := v1Migrations(t, loadEmbedded(t, ctx))
	if len(all) < 3 {
		t.Fatalf("v1 migrations = %d, want at least three", len(all))
	}

	if _, err := applyMigrations(ctx, dbPath, all[:2]); err != nil {
		t.Fatalf("apply earlier migrations: %v", err)
	}
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, stmt := range []string{
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at) VALUES ('int-1', 'github', 'legacy', NULL, 'enc', 't', 't')`,
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at) VALUES ('int-2', 'github', 'legacy two', NULL, 'enc', 't', 't')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	if _, err := applyMigrations(ctx, dbPath, all); err != nil {
		t.Fatalf("apply remaining migrations: %v", err)
	}

	var count, defaults int
	if err := raw.QueryRow(`SELECT COUNT(*), COALESCE(SUM(is_default), 0) FROM integrations`).Scan(&count, &defaults); err != nil {
		t.Fatalf("read integrations: %v", err)
	}
	if count != 2 || defaults != 0 {
		t.Fatalf("integrations = %d rows, %d default, want 2 rows and none default", count, defaults)
	}
}

func TestMigrateRefusesV1Database(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	all := loadEmbedded(t, ctx)
	v1 := v1Migrations(t, all)
	if len(v1) != 4 || len(v1) == len(all) {
		t.Fatalf("v1 migrations = %d of %d embedded, want the four v1 files and a newer one", len(v1), len(all))
	}

	seedV1Repo := func(t *testing.T, dbPath string) {
		t.Helper()
		raw, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		defer func() { _ = raw.Close() }()
		for _, stmt := range []string{
			`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at) VALUES ('int-1', 'kaneo', 'Kaneo', 'https://kaneo.example', 'enc', 't', 't')`,
			`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at) VALUES ('tp-1', 'int-1', 'X', 'Project', '{}', '{}', 't', 't')`,
			`INSERT INTO monitored_repos (id, source_kind, project_path, ticket_project_id, created_at, updated_at) VALUES ('repo-1', 'github', 'org/repo', 'tp-1', 't', 't')`,
		} {
			if _, err := raw.Exec(stmt); err != nil {
				t.Fatalf("seed %q: %v", stmt, err)
			}
		}
	}

	cases := []struct {
		name  string
		build func(t *testing.T, dbPath string)
	}{
		{"all four v1 migrations applied, repo present", func(t *testing.T, dbPath string) {
			if _, err := applyMigrations(ctx, dbPath, v1); err != nil {
				t.Fatalf("apply v1: %v", err)
			}
			seedV1Repo(t, dbPath)
		}},
		{"only the first v1 migration applied", func(t *testing.T, dbPath string) {
			if _, err := applyMigrations(ctx, dbPath, v1[:1]); err != nil {
				t.Fatalf("apply v1 baseline: %v", err)
			}
		}},
		{"v1 tables without migration bookkeeping", func(t *testing.T, dbPath string) {
			raw, err := sql.Open("sqlite", dbPath)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer func() { _ = raw.Close() }()
			if _, err := raw.Exec(v1[0].SQL); err != nil {
				t.Fatalf("create v1 tables: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dbPath := filepath.Join(t.TempDir(), "app.db")
			tc.build(t, dbPath)
			before, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatalf("read db: %v", err)
			}
			entriesBefore := dirListing(t, filepath.Dir(dbPath))

			applied, err := Migrate(ctx, dbPath)
			if !errors.Is(err, ErrV1Database) {
				t.Fatalf("Migrate error = %v, want ErrV1Database", err)
			}
			if len(applied) != 0 {
				t.Fatalf("Migrate applied %d migrations on a v1 database, want 0", len(applied))
			}
			if !strings.Contains(err.Error(), "fresh data volume") {
				t.Fatalf("error = %q, want it to tell the operator to use a fresh data volume", err)
			}

			after, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatalf("re-read db: %v", err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("v1 database file changed, want it byte-identical")
			}
			if got := dirListing(t, filepath.Dir(dbPath)); got != entriesBefore {
				t.Fatalf("data dir after the refusal = %q, want %q (no snapshot, no sidecar file)", got, entriesBefore)
			}
		})
	}
}

func TestMigrateTreatsMissingAndEmptyDatabaseAsFresh(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := len(loadEmbedded(t, ctx))

	missing := filepath.Join(t.TempDir(), "app.db")
	if applied, err := Migrate(ctx, missing); err != nil || len(applied) != want {
		t.Fatalf("Migrate on a missing file = %d migrations, %v; want %d, nil", len(applied), err, want)
	}

	empty := filepath.Join(t.TempDir(), "app.db")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write empty db: %v", err)
	}
	if applied, err := Migrate(ctx, empty); err != nil || len(applied) != want {
		t.Fatalf("Migrate on an empty file = %d migrations, %v; want %d, nil", len(applied), err, want)
	}
}

func TestV2SchemaConstraints(t *testing.T) {
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

	exec := func(stmt string, args ...any) error {
		_, err := db.Writer.Exec(stmt, args...)
		return err
	}
	mustExec := func(stmt string, args ...any) {
		t.Helper()
		if err := exec(stmt, args...); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	mustFail := func(what, stmt string, args ...any) {
		t.Helper()
		if err := exec(stmt, args...); err == nil {
			t.Fatalf("%s: accepted, want a constraint failure", what)
		}
	}

	const installation = `INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		VALUES (?, ?, 1, 'acme', 'Organization', ?, 'all', 't', 't')`
	mustExec(installation, "inst-1", 11, "active")
	mustFail("a second installation that is not removed", installation, "inst-2", 12, "suspended")
	mustExec(`UPDATE installations SET status = 'removed' WHERE id = 'inst-1'`)
	mustExec(installation, "inst-2", 12, "active")
	mustFail("a duplicate github installation id", installation, "inst-3", 12, "removed")

	const repo = `INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'main', 't', 't')`
	mustExec(repo, "repo-1", "inst-2", 101, "acme/api")
	mustFail("the same full name in one installation", repo, "repo-2", "inst-2", 102, "acme/api")
	mustFail("the same github repository id twice", repo, "repo-3", "inst-2", 101, "acme/web")
	mustFail("a repository of an unknown installation", repo, "repo-4", "no-such", 104, "acme/x")

	const job = `INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		VALUES (?, 'renovate_run', 'repo-1', 'normal', ?, 't', 'schedule', 't')`
	mustExec(job, "job-1", "queued")
	mustFail("a second queued job for one repository", job, "job-2", "queued")
	mustExec(job, "job-3", "running")
	mustFail("a second running job for one repository", job, "job-4", "running")
	mustExec(job, "job-5", "succeeded")
	mustExec(job, "job-6", "succeeded")

	mustExec(`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome) VALUES ('run-1', 'job-1', 1, 't', 'running')`)
	mustFail("a second run with the same attempt", `INSERT INTO job_runs (id, job_id, attempt, started_at, outcome) VALUES ('run-2', 'job-1', 1, 't', 'running')`)
	mustExec(`INSERT INTO job_events (id, job_id, job_run_id, at, kind) VALUES ('evt-1', 'job-1', 'run-1', 't', 'enqueued')`)
	mustExec(`DELETE FROM jobs WHERE id = 'job-1'`)
	var left int
	if err := db.Writer.QueryRow(`SELECT (SELECT COUNT(*) FROM job_runs) + (SELECT COUNT(*) FROM job_events)`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("job_runs and job_events after deleting their job = %d, %v; want them cascaded away", left, err)
	}

	mustExec(`INSERT INTO users (id, created_at, updated_at) VALUES ('u1', 't', 't')`)
	const identity = `INSERT INTO auth_identities (id, user_id, provider, subject, login, created_at) VALUES (?, 'u1', ?, ?, 'octo', 't')`
	mustExec(identity, "ai-1", "github", "4242")
	mustFail("the same provider and subject twice", identity, "ai-2", "github", "4242")
	mustFail("an unknown provider", identity, "ai-3", "gitlab", "7")

	mustFail("a forge integration kind", `INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at) VALUES ('i-gh', 'github', 'GH', NULL, 'enc', 't', 't')`)
	mustExec(`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at) VALUES ('i-lin', 'linear', 'Linear', NULL, 'enc', 't', 't')`)

	mustExec(`INSERT INTO instance_settings (id, updated_at) VALUES (1, 't')`)
	var cap int
	if err := db.Writer.QueryRow(`SELECT concurrency_cap FROM instance_settings`).Scan(&cap); err != nil || cap != 2 {
		t.Fatalf("concurrency_cap default = %d, %v; want 2", cap, err)
	}
	mustFail("a concurrency cap of 0", `UPDATE instance_settings SET concurrency_cap = 0`)
}

func TestGitHubAppSecretsAreEncryptedColumnsOnly(t *testing.T) {
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

	rows, err := db.Writer.Query(`SELECT name, "notnull" FROM pragma_table_info('github_app')`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	defer func() { _ = rows.Close() }()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		var notNull int
		if err := rows.Scan(&name, &notNull); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols[name] = notNull == 1
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for _, name := range []string{"encrypted_private_key", "encrypted_client_secret", "encrypted_webhook_secret", "key_id"} {
		if notNull, ok := cols[name]; !ok || !notNull {
			t.Fatalf("github_app.%s present and NOT NULL = %v, %v; want both", name, ok, notNull)
		}
	}
	for name := range cols {
		for _, plain := range []string{"private_key", "client_secret", "webhook_secret", "pem"} {
			if name == plain {
				t.Fatalf("github_app has a plaintext secret column %q", name)
			}
		}
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
		_, err := db.Writer.Exec(
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

	if _, err := db.Writer.Exec(
		`INSERT INTO users (id, created_at, updated_at) VALUES ('u1', 't', 't')`,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := insertSession("t-known", "u1"); err != nil {
		t.Fatalf("session with known user_id: %v", err)
	}

	if _, err := db.Writer.Exec(`DELETE FROM sessions WHERE user_id = 'u1'`); err != nil {
		t.Fatalf("delete user sessions: %v", err)
	}
	if _, err := db.Writer.Exec(`DELETE FROM users WHERE id = 'u1'`); err != nil {
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

// v1Migrations returns the migrations a v1 install has, in version order.
func v1Migrations(t *testing.T, all []sqlitemigrate.Migration) []sqlitemigrate.Migration {
	t.Helper()

	var v1 []sqlitemigrate.Migration
	for _, m := range all {
		if m.Version <= lastV1MigrationVersion {
			v1 = append(v1, m)
		}
	}
	return v1
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

// dirListing lists the files under dir, with their sizes, as one comparable string.
func dirListing(t *testing.T, dir string) string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s:%d", path, info.Size()))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return strings.Join(out, "\n")
}

func snapshotCount(t *testing.T, dbPath string) int {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join(filepath.Dir(dbPath), "snapshots"))
	if err != nil {
		t.Fatalf("read snapshots dir: %v", err)
	}
	return len(entries)
}
