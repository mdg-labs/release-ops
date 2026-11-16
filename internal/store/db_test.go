package store_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

func openTestDB(t *testing.T) (*store.DB, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dbPath
}

func pragmaString(t *testing.T, db *sql.DB, name string) string {
	t.Helper()

	var v string
	if err := db.QueryRow(`PRAGMA ` + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestOpenPathConfiguresBothPools(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)

	for name, pool := range map[string]*sql.DB{"writer": db.Writer, "reader": db.Reader} {
		if got := pragmaString(t, pool, "journal_mode"); got != "wal" {
			t.Errorf("%s journal_mode = %q, want wal", name, got)
		}
		if got := pragmaString(t, pool, "busy_timeout"); got != "5000" {
			t.Errorf("%s busy_timeout = %q, want 5000", name, got)
		}
		if got := pragmaString(t, pool, "foreign_keys"); got != "1" {
			t.Errorf("%s foreign_keys = %q, want 1", name, got)
		}
	}
	// synchronous: 1 is NORMAL. The read pool never commits, so only the writer needs it.
	if got := pragmaString(t, db.Writer, "synchronous"); got != "1" {
		t.Errorf("writer synchronous = %q, want 1 (NORMAL)", got)
	}
	if got := db.Writer.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("writer MaxOpenConnections = %d, want 1", got)
	}
	if got := db.Reader.Stats().MaxOpenConnections; got < 2 {
		t.Errorf("reader MaxOpenConnections = %d, want a pool of several", got)
	}
}

// A read on the pool while a write transaction is open used to deadlock: the database had
// one connection, the transaction held it, and the read waited for it forever. The read
// pool has its own connections, so the read completes while the transaction stays open.
func TestReadCompletesWhileWriteTransactionIsOpen(t *testing.T) {
	t.Parallel()

	db, dbPath := openTestDB(t)
	ctx := context.Background()

	tx, err := db.Writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO instance_settings (id, updated_at) VALUES (1, 't')`); err != nil {
		t.Fatalf("insert in tx: %v", err)
	}

	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var n int
		err := db.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_settings`).Scan(&n)
		done <- result{n, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("read during write transaction: %v", got.err)
		}
		if got.count != 0 {
			t.Fatalf("read saw %d rows, want 0: the transaction is uncommitted", got.count)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read on the pool blocked while a write transaction was open")
	}

	// The pre-WAL setup (cache=shared, one connection, no read pool) blocks on exactly
	// this read; the bounded context turns that hang into an error.
	legacy, err := sql.Open("sqlite", "file:"+dbPath+"?cache=shared&_foreign_keys=on&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open legacy pool: %v", err)
	}
	t.Cleanup(func() { _ = legacy.Close() })
	legacy.SetMaxOpenConns(1)
	legacyTx, err := legacy.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("legacy BeginTx: %v", err)
	}
	defer func() { _ = legacyTx.Rollback() }()

	readCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	var n int
	err = legacy.QueryRowContext(readCtx, `SELECT COUNT(*) FROM instance_settings`).Scan(&n)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("legacy read while its transaction is open: err = %v, want the deadline to expire", err)
	}
}

func TestWriteIsVisibleToReadPoolAfterCommit(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := context.Background()

	count := func() int {
		var n int
		if err := db.Reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_settings`).Scan(&n); err != nil {
			t.Fatalf("read: %v", err)
		}
		return n
	}
	if got := count(); got != 0 {
		t.Fatalf("rows before write = %d, want 0", got)
	}

	if _, err := db.Writer.ExecContext(ctx, `INSERT INTO instance_settings (id, updated_at) VALUES (1, 't')`); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := count(); got != 1 {
		t.Fatalf("rows after commit = %d, want 1 (stale read pool)", got)
	}
}

func TestReadPoolRejectsWrites(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t)
	ctx := context.Background()

	_, err := db.Reader.ExecContext(ctx, `INSERT INTO instance_settings (id, updated_at) VALUES (1, 't')`)
	if err == nil {
		t.Fatal("write on the read pool succeeded, want an error")
	}

	var n int
	if err := db.Writer.QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_settings`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rows after rejected write = %d, want 0", n)
	}
}

func TestOpenPathConvertsRollbackJournalDatabaseToWAL(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)

	old, err := sql.Open("sqlite", "file:"+dbPath+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("open old: %v", err)
	}
	if got := pragmaString(t, old, "journal_mode"); got == "wal" {
		t.Fatalf("migrated database already in %q mode, want a rollback journal", got)
	}
	if _, err := old.Exec(`INSERT INTO instance_settings (id, concurrency_cap, updated_at) VALUES (1, 7, 't')`); err != nil {
		t.Fatalf("seed old db: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close old: %v", err)
	}

	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("OpenPath on rollback-journal database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if got := pragmaString(t, db.Writer, "journal_mode"); got != "wal" {
		t.Fatalf("journal_mode after open = %q, want wal", got)
	}
	var cap int
	if err := db.Reader.QueryRow(`SELECT concurrency_cap FROM instance_settings WHERE id = 1`).Scan(&cap); err != nil {
		t.Fatalf("read existing row: %v", err)
	}
	if cap != 7 {
		t.Fatalf("concurrency_cap = %d, want 7", cap)
	}
}

// The store's repositories send reads to the pool and writes to the writer connection.
// With the writer's only connection held by a transaction, a read completes and a
// write waits for it.
func TestStoreRoutesReadsToPoolAndWritesToWriter(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	tx, err := s.DB().Writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	readCtx, cancelRead := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRead()
	if _, err := s.InstanceSettings().ConcurrencyCap(readCtx); err != nil {
		t.Fatalf("ConcurrencyCap while the writer is held: %v", err)
	}
	if _, err := s.Integrations().List(readCtx); err != nil {
		t.Fatalf("Integrations().List while the writer is held: %v", err)
	}

	writeCtx, cancelWrite := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancelWrite()
	err = s.InstanceSettings().SetConcurrencyCap(writeCtx, 3)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SetConcurrencyCap while the writer is held: err = %v, want it to wait for the writer connection", err)
	}
}

func TestStoreWriteIsVisibleToStoreReads(t *testing.T) {
	t.Parallel()

	s, _ := testStore(t)
	ctx := context.Background()

	created, err := s.Integrations().Create(ctx, store.CreateIntegrationInput{Kind: "linear", Name: "gh"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Integrations().Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != "gh" {
		t.Fatalf("Name = %q, want gh", got.Name)
	}
}
