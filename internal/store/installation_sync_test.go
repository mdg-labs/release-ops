package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

func newSync(t *testing.T) (store.InstallationSync, *store.DB) {
	t.Helper()
	db := storetest.OpenMigrated(t)
	return store.New(db, nil).InstallationSync(), db
}

func apply(t *testing.T, s store.InstallationSync, in store.NewInstallation) store.InstallationOutcome {
	t.Helper()
	var out store.InstallationOutcome
	err := s.Transaction(context.Background(), func(tx *sql.Tx) error {
		var err error
		out, err = s.ApplyInstallation(context.Background(), tx, in)
		return err
	})
	if err != nil {
		t.Fatalf("ApplyInstallation: %v", err)
	}
	return out
}

func count(t *testing.T, db *store.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Reader.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestInstallationSyncTransactionRollsBackEverythingWhenTheCallbackFails(t *testing.T) {
	t.Parallel()

	s, db := newSync(t)
	boom := errors.New("boom")
	err := s.Transaction(context.Background(), func(tx *sql.Tx) error {
		if _, err := s.ApplyInstallation(context.Background(), tx, sampleInstallation(1000)); err != nil {
			return err
		}
		if err := s.UpsertRepositories(context.Background(), tx, 1000, []store.SyncedRepository{{GitHubID: 1, FullName: "acme/a", DefaultBranch: "main"}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Transaction = %v, want the callback's error", err)
	}
	if n := count(t, db, `SELECT (SELECT COUNT(*) FROM installations) + (SELECT COUNT(*) FROM repositories)`); n != 0 {
		t.Errorf("rows after a failed transaction = %d, want none: a half-applied sync must not be left behind", n)
	}
}

func TestInstallationSyncApplyInstallationRefreshesTheRecordedOneOnlyWhenSomethingChanged(t *testing.T) {
	t.Parallel()

	s, db := newSync(t)
	in := sampleInstallation(1000)
	if out := apply(t, s, in); out != store.InstallationRecorded {
		t.Fatalf("first apply = %v, want recorded", out)
	}
	var created string
	if err := db.Reader.QueryRow(`SELECT updated_at FROM installations`).Scan(&created); err != nil {
		t.Fatal(err)
	}

	if out := apply(t, s, in); out != store.InstallationKnown {
		t.Fatalf("same apply = %v, want known", out)
	}
	var same string
	_ = db.Reader.QueryRow(`SELECT updated_at FROM installations`).Scan(&same)
	if same != created {
		t.Errorf("an identical apply rewrote the row: %s -> %s", created, same)
	}

	in.Status = "suspended"
	in.Permissions = `{"contents":"read"}`
	apply(t, s, in)
	var status, permissions string
	if err := db.Reader.QueryRow(`SELECT status, permissions FROM installations`).Scan(&status, &permissions); err != nil {
		t.Fatal(err)
	}
	if status != "suspended" || permissions != `{"contents":"read"}` {
		t.Errorf("row = %s %s, want the refreshed values", status, permissions)
	}

	if out := apply(t, s, sampleInstallation(2000)); out != store.InstallationRefused {
		t.Errorf("a second installation = %v, want refused", out)
	}
}

func TestInstallationSyncRefusesRepositoriesOfAnInstallationThatIsNotLive(t *testing.T) {
	t.Parallel()

	s, db := newSync(t)
	ctx := context.Background()
	repos := []store.SyncedRepository{{GitHubID: 1, FullName: "acme/a", DefaultBranch: "main"}}

	err := s.Transaction(ctx, func(tx *sql.Tx) error { return s.UpsertRepositories(ctx, tx, 1000, repos) })
	if !errors.Is(err, store.ErrInstallationNotLive) {
		t.Fatalf("upsert for an unknown installation = %v, want ErrInstallationNotLive", err)
	}

	apply(t, s, sampleInstallation(1000))
	if err := s.Transaction(ctx, func(tx *sql.Tx) error { return s.RemoveInstallation(ctx, tx, 1000) }); err != nil {
		t.Fatal(err)
	}
	err = s.Transaction(ctx, func(tx *sql.Tx) error { return s.UpsertRepositories(ctx, tx, 1000, repos) })
	if !errors.Is(err, store.ErrInstallationNotLive) {
		t.Fatalf("upsert for a removed installation = %v, want ErrInstallationNotLive", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repositories`); n != 0 {
		t.Errorf("repositories = %d, want none", n)
	}

	if err := s.Transaction(ctx, func(tx *sql.Tx) error { return s.RemoveInstallation(ctx, tx, 4242) }); err != nil {
		t.Errorf("removing an unknown installation = %v, want nothing to do", err)
	}
}

func TestInstallationSyncReplaceRepositoriesMarksTheRestRemovedAndKeepsAPause(t *testing.T) {
	t.Parallel()

	s, db := newSync(t)
	ctx := context.Background()
	apply(t, s, sampleInstallation(1000))
	all := []store.SyncedRepository{
		{GitHubID: 1, FullName: "acme/a", DefaultBranch: "main", ConfigState: store.ConfigPresent},
		{GitHubID: 2, FullName: "acme/b", DefaultBranch: "main", ConfigState: store.ConfigMissing},
	}
	if err := s.Transaction(ctx, func(tx *sql.Tx) error { return s.ReplaceRepositories(ctx, tx, 1000, all) }); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE github_repository_id = 1`); err != nil {
		t.Fatal(err)
	}

	// An empty ConfigState keeps what is recorded; a missing repository leaves.
	if err := s.Transaction(ctx, func(tx *sql.Tx) error {
		return s.ReplaceRepositories(ctx, tx, 1000, []store.SyncedRepository{{GitHubID: 1, FullName: "acme/a", DefaultBranch: "main"}})
	}); err != nil {
		t.Fatal(err)
	}
	states, err := s.RepositoryConfigStates(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[1] != store.ConfigPresent {
		t.Errorf("active config states = %v, want only repository 1, still present", states)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repositories WHERE github_repository_id = 1 AND enabled = 0`); n != 1 {
		t.Errorf("a sync re-enabled a paused repository")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM repositories WHERE github_repository_id = 2 AND removed_at IS NOT NULL`); n != 1 {
		t.Errorf("repository 2 was not marked removed")
	}
}
