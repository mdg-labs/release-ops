package store_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

var prEpoch = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type prEnv struct {
	db    *store.DB
	repos store.UpdatePRRepository
	repo  string
}

func newPREnv(t *testing.T) *prEnv {
	t.Helper()
	s, _ := testStore(t)
	ids := storetest.SeedRepositories(t, s.DB(), 2)
	return &prEnv{db: s.DB(), repos: s.UpdatePRs(), repo: ids[0]}
}

// upsert applies one report in its own transaction and reports whether the row changed.
func (e *prEnv) upsert(t *testing.T, in store.UpdatePRInput) bool {
	t.Helper()
	return e.upsertResult(t, in).Changed
}

func (e *prEnv) upsertResult(t *testing.T, in store.UpdatePRInput) store.UpsertResult {
	t.Helper()
	if in.RepositoryID == "" {
		in.RepositoryID = e.repo
	}
	if in.Number == 0 {
		in.Number = 21
	}
	if in.NodeID == "" {
		in.NodeID, in.URL, in.HeadRef = "PR_node", "https://github.com/acme/repo-1/pull/21", "renovate/lodash-4.x"
	}
	if in.Title == "" {
		in.Title = "chore(deps): update lodash"
	}
	if in.State == "" {
		in.State = store.UpdatePROpen
	}
	in.Now = prEpoch.Add(time.Hour)
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.repos.Upsert(context.Background(), tx, in)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("Upsert: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return res
}

type prRow struct {
	State, Title, HeadSHA, Labels, SeenAt string
	SupersededBy                          sql.NullString
	ClosedAt, MergedAt                    sql.NullString
}

func (e *prEnv) row(t *testing.T) prRow {
	t.Helper()
	var r prRow
	var sha sql.NullString
	if err := e.db.Reader.QueryRow(`SELECT state, title, head_sha, labels, last_seen_at, superseded_by_id, closed_at, merged_at
		FROM update_prs WHERE repository_id = ? AND number = 21`, e.repo).
		Scan(&r.State, &r.Title, &sha, &r.Labels, &r.SeenAt, &r.SupersededBy, &r.ClosedAt, &r.MergedAt); err != nil {
		t.Fatalf("read row: %v", err)
	}
	r.HeadSHA = sha.String
	return r
}

func (e *prEnv) count(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.db.Reader.QueryRow(`SELECT COUNT(*) FROM update_prs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func at(minutes int) time.Time { return prEpoch.Add(time.Duration(minutes) * time.Minute) }

func TestUpsertCreatesTheRowAndReportsAChange(t *testing.T) {
	e := newPREnv(t)
	if !e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0), HeadSHA: "aaa", Labels: []string{"dependencies"}}) {
		t.Fatal("creating a row reported no change")
	}
	got := e.row(t)
	if got.State != "open" || got.HeadSHA != "aaa" || got.Labels != `["dependencies"]` || got.SeenAt != "2026-11-15T10:00:00.000Z" {
		t.Fatalf("row = %+v", got)
	}
}

func TestUpsertReplayChangesNothing(t *testing.T) {
	e := newPREnv(t)
	in := store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0), HeadSHA: "aaa"}
	e.upsert(t, in)
	if e.upsert(t, in) {
		t.Fatal("applying the same report again reported a change")
	}
	if e.count(t) != 1 {
		t.Fatalf("%d rows, want one", e.count(t))
	}
}

// A report older than the stored one never overwrites it: an edit that was generated before the
// close but arrives after it must not reopen the row or rewrite its title.
func TestUpsertStaleEditAfterCloseDoesNotReopen(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0), State: store.UpdatePRMerged, MergedAt: at(30), ClosedAt: at(30)})

	if e.upsert(t, store.UpdatePRInput{EventAt: at(25), OpenedAt: at(0), Title: "stale title"}) {
		t.Fatal("a stale edit reported a change")
	}
	got := e.row(t)
	if got.State != "merged" || got.Title != "chore(deps): update lodash" || got.SeenAt != "2026-11-15T10:30:00.000Z" {
		t.Fatalf("row after the stale edit = %+v, want merged with the original title", got)
	}
}

// Upsert says whether it accepted a report apart from whether the row changed: an applied replay
// is accepted and changes nothing, a stale report or an open one at a close's EventAt is not
// accepted.
func TestUpsertReportsWhetherTheReportWasAccepted(t *testing.T) {
	e := newPREnv(t)
	in := store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0), HeadSHA: "aaa"}
	if got := e.upsertResult(t, in); got != (store.UpsertResult{Accepted: true, Changed: true}) {
		t.Fatalf("creating = %+v", got)
	}
	if got := e.upsertResult(t, in); got != (store.UpsertResult{Accepted: true}) {
		t.Fatalf("replay = %+v", got)
	}
	e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0), State: store.UpdatePRClosed, ClosedAt: at(30)})
	if got := e.upsertResult(t, store.UpdatePRInput{EventAt: at(25), OpenedAt: at(0)}); got.Accepted || got.Changed {
		t.Fatalf("stale report = %+v", got)
	}
	if got := e.upsertResult(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0)}); got.Accepted || got.Changed {
		t.Fatalf("open report at the close's EventAt = %+v", got)
	}
	if got := e.upsertResult(t, store.UpdatePRInput{EventAt: at(31), OpenedAt: at(0)}); got != (store.UpsertResult{Accepted: true, Changed: true}) {
		t.Fatalf("later open report = %+v", got)
	}
}

// GitHub stamps a close and an edit made in the same second alike; the close stays.
func TestUpsertEditStampedAtTheCloseDoesNotReopen(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0), State: store.UpdatePRClosed, ClosedAt: at(30)})
	if e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0)}) {
		t.Fatal("an open report at the close's time reported a change")
	}
	if got := e.row(t); got.State != "closed" {
		t.Fatalf("state = %q, want closed", got.State)
	}
}

func TestUpsertReopenAfterCloseOpensTheRowAndClearsTheCloseTimes(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0), State: store.UpdatePRClosed, ClosedAt: at(30)})
	if !e.upsert(t, store.UpdatePRInput{EventAt: at(40), OpenedAt: at(0)}) {
		t.Fatal("reopening reported no change")
	}
	got := e.row(t)
	if got.State != "open" || got.ClosedAt.Valid || got.MergedAt.Valid {
		t.Fatalf("row = %+v, want open without close times", got)
	}
}

func TestUpsertOutOfOrderEditsLeaveTheNewestState(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(10), OpenedAt: at(0), Title: "second", HeadSHA: "bbb"})
	e.upsert(t, store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0), Title: "first", HeadSHA: "aaa"})
	if got := e.row(t); got.Title != "second" || got.HeadSHA != "bbb" {
		t.Fatalf("row = %+v, want the newer report", got)
	}
}

func TestUpsertKeepsSupersededOnAClosedReportAndClearsItOnReopen(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	if _, err := e.db.Writer.Exec(`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		VALUES ('replacement', ?, 99, 'n', 'u', 't', 'h', 'open', '2026-11-15T10:00:00.000Z', '2026-11-15T10:00:00.000Z', '2026-11-15T10:00:00.000Z')`, e.repo); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Writer.Exec(`UPDATE update_prs SET state = 'superseded', superseded_by_id = 'replacement' WHERE number = 21`); err != nil {
		t.Fatal(err)
	}

	e.upsert(t, store.UpdatePRInput{EventAt: at(20), OpenedAt: at(0), State: store.UpdatePRClosed, ClosedAt: at(20), Title: "renamed"})
	if got := e.row(t); got.State != "superseded" || got.SupersededBy.String != "replacement" || got.Title != "renamed" {
		t.Fatalf("row after a closed report = %+v, want still superseded by the replacement", got)
	}
	e.upsert(t, store.UpdatePRInput{EventAt: at(30), OpenedAt: at(0)})
	if got := e.row(t); got.State != "open" || got.SupersededBy.Valid {
		t.Fatalf("row after a reopen = %+v, want open without a replacement", got)
	}
}

func TestUpsertAReportThatOnlyAdvancesTheClockReportsNoChange(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	if e.upsert(t, store.UpdatePRInput{EventAt: at(7), OpenedAt: at(0)}) {
		t.Fatal("a report with nothing new reported a change")
	}
	if got := e.row(t); got.SeenAt != "2026-11-15T10:07:00.000Z" {
		t.Fatalf("last_seen_at = %q, want the newer event time", got.SeenAt)
	}
}

func TestUpsertRowsAreScopedToTheRepository(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(0), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{RepositoryID: "repo-2", EventAt: at(0), OpenedAt: at(0)})
	if e.count(t) != 2 {
		t.Fatalf("%d rows, want one per repository for the same number", e.count(t))
	}
}

// The upsert is the transaction's: a rollback leaves no row.
func TestUpsertRollsBackWithItsTransaction(t *testing.T) {
	e := newPREnv(t)
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.repos.Upsert(context.Background(), tx, store.UpdatePRInput{
		RepositoryID: e.repo, Number: 21, NodeID: "n", URL: "u", Title: "t", HeadRef: "h", State: "open", EventAt: at(0), Now: at(0),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if e.count(t) != 0 {
		t.Fatal("a rolled-back upsert left a row")
	}
}

func TestOpenPRsCountsAndReconcileRepositories(t *testing.T) {
	e := newPREnv(t)
	for number, state := range map[int64]string{21: "open", 22: "open", 23: "merged", 24: "closed"} {
		e.upsert(t, store.UpdatePRInput{Number: number, State: state, EventAt: at(0), OpenedAt: at(0)})
	}
	ctx := context.Background()
	open, err := e.repos.OpenPRs(ctx, e.repo)
	want := []store.OpenUpdatePR{{Number: 21, Version: "2026-11-15T10:00:00.000Z"}, {Number: 22, Version: "2026-11-15T10:00:00.000Z"}}
	if err != nil || !reflect.DeepEqual(open, want) {
		t.Fatalf("OpenPRs = %v, %v, want %v", open, err, want)
	}
	counts, err := e.repos.OpenCounts(ctx)
	if err != nil || !reflect.DeepEqual(counts, map[string]int{"acme/repo-1": 2, "acme/repo-2": 0}) {
		t.Fatalf("OpenCounts = %v, %v", counts, err)
	}

	if _, err := e.db.Writer.Exec(`UPDATE repositories SET enabled = 0 WHERE id = 'repo-2'`); err != nil {
		t.Fatal(err)
	}
	ids, err := e.repos.ReconcileRepositoryIDs(ctx)
	if err != nil || !reflect.DeepEqual(ids, []string{"repo-1"}) {
		t.Fatalf("ReconcileRepositoryIDs = %v, %v, want only the enabled repository", ids, err)
	}
	if _, err := e.repos.ReconcileRepository(ctx, "repo-2"); !errors.Is(err, store.ErrRepositoryNotFound) {
		t.Fatalf("ReconcileRepository of a paused repository = %v, want ErrRepositoryNotFound", err)
	}
	got, err := e.repos.ReconcileRepository(ctx, "repo-1")
	if err != nil || got.FullName != "acme/repo-1" || got.GitHubInstallationID != 1 {
		t.Fatalf("ReconcileRepository = %+v, %v", got, err)
	}
	if _, err := e.db.Writer.Exec(`UPDATE repositories SET removed_at = '2026-11-15T10:00:00.000Z' WHERE id = 'repo-1'`); err != nil {
		t.Fatal(err)
	}
	counts, _ = e.repos.OpenCounts(ctx)
	if _, listed := counts["acme/repo-1"]; listed {
		t.Fatalf("OpenCounts = %v, want a removed repository left out", counts)
	}
}

// seen is the version (last_seen_at) of a row last reported at the given minute.
func seen(minutes int) string { return at(minutes).Format("2006-01-02T15:04:05.000Z") }

func (e *prEnv) closeGone(t *testing.T, repositoryID string, number int64, version string) bool {
	t.Helper()
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := e.repos.CloseGone(context.Background(), tx, repositoryID, number, version, at(90))
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("CloseGone: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return changed
}

// CloseGone closes an open row and nothing else: a merged row, another repository's row with
// the same number and an absent row stay as they are, and last_seen_at keeps ordering reports.
func TestCloseGoneClosesOnlyTheOpenRowOfTheRepository(t *testing.T) {
	e := newPREnv(t)
	var other string
	if err := e.db.Reader.QueryRow(`SELECT id FROM repositories WHERE id <> ?`, e.repo).Scan(&other); err != nil {
		t.Fatal(err)
	}
	e.upsert(t, store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{RepositoryID: other, EventAt: at(5), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{Number: 22, EventAt: at(6), OpenedAt: at(0), State: store.UpdatePRMerged, MergedAt: at(6)})

	if e.closeGone(t, e.repo, 99, seen(5)) {
		t.Fatal("closing an absent row reported a change")
	}
	if e.closeGone(t, e.repo, 22, seen(6)) {
		t.Fatal("closing a merged row reported a change")
	}
	if e.closeGone(t, e.repo, 21, seen(4)) {
		t.Fatal("closing a row that was reported after it was read reported a change")
	}
	if got := e.row(t); got.State != "open" || got.ClosedAt.Valid {
		t.Fatalf("row = %+v, want it left open", got)
	}
	if !e.closeGone(t, e.repo, 21, seen(5)) {
		t.Fatal("closing an open row reported no change")
	}
	got := e.row(t)
	if got.State != "closed" || got.ClosedAt.String != "2026-11-15T11:30:00.000Z" || got.SeenAt != "2026-11-15T10:05:00.000Z" || got.MergedAt.Valid {
		t.Fatalf("row = %+v, want closed at 11:30 with last_seen_at 10:05", got)
	}
	var otherState, mergedState string
	if err := e.db.Reader.QueryRow(`SELECT state FROM update_prs WHERE repository_id = ? AND number = 21`, other).Scan(&otherState); err != nil {
		t.Fatal(err)
	}
	if err := e.db.Reader.QueryRow(`SELECT state FROM update_prs WHERE repository_id = ? AND number = 22`, e.repo).Scan(&mergedState); err != nil {
		t.Fatal(err)
	}
	if otherState != "open" || mergedState != "merged" {
		t.Fatalf("another repository's PR is %q, the merged PR is %q, want open and merged", otherState, mergedState)
	}
	if e.closeGone(t, e.repo, 21, seen(5)) {
		t.Fatal("closing a closed row again reported a change")
	}
}

// An open report at the EventAt a row was closed by CloseGone at reopens it, because that close
// carries no stamp from GitHub; a close from GitHub keeps refusing an open report at its
// EventAt.
func TestUpsertReopensARowCloseGoneClosedAtTheSameEventAt(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{Number: 22, EventAt: at(5), OpenedAt: at(0)})
	e.upsert(t, store.UpdatePRInput{Number: 22, EventAt: at(6), OpenedAt: at(0), State: store.UpdatePRClosed, ClosedAt: at(6)})
	if !e.closeGone(t, e.repo, 21, seen(5)) {
		t.Fatal("closing an open row reported no change")
	}

	if !e.upsert(t, store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0)}) {
		t.Fatal("an open report at the EventAt of a gone-closed row changed nothing")
	}
	if got := e.row(t); got.State != "open" || got.ClosedAt.Valid {
		t.Fatalf("row = %+v, want open without closed_at", got)
	}
	if e.upsert(t, store.UpdatePRInput{Number: 22, EventAt: at(6), OpenedAt: at(0)}) {
		t.Fatal("an open report at the EventAt of a close from GitHub changed the row")
	}
}
