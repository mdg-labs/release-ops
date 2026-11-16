package store_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/mdg-labs/release-ops/internal/store"
)

func (e *prEnv) run(t *testing.T, repo, job, started, status, report string) {
	t.Helper()
	if _, err := e.db.Writer.Exec(`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		VALUES (?, 'renovate_run', ?, 'normal', 'succeeded', ?, 'schedule', ?)`, job, repo, started, started); err != nil {
		t.Fatal(err)
	}
	var rep any
	if report != "" {
		rep = report
	}
	if _, err := e.db.Writer.Exec(`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome, report_status, report)
		VALUES (?, ?, 1, ?, 'succeeded', ?, ?)`, "run-"+job, job, started, status, rep); err != nil {
		t.Fatal(err)
	}
}

func (e *prEnv) latest(t *testing.T, repo string) (store.RenovateReport, bool) {
	t.Helper()
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	rep, ok, err := e.repos.LatestReport(context.Background(), tx, repo)
	if err != nil {
		t.Fatal(err)
	}
	return rep, ok
}

func TestLatestReportIsTheNewestReadableReportOfTheRepository(t *testing.T) {
	e := newPREnv(t)
	other := "repo-2"
	if _, ok := e.latest(t, e.repo); ok {
		t.Fatal("a repository without a run has a report")
	}
	e.run(t, e.repo, "j1", "2026-11-15T09:00:00.000Z", "ok", `{"n":1}`)
	e.run(t, e.repo, "j2", "2026-11-15T09:30:00.000Z", "ok", `{"n":2}`)
	e.run(t, e.repo, "j3", "2026-11-15T09:45:00.000Z", "unreadable", "")
	e.run(t, e.repo, "j4", "2026-11-15T09:50:00.000Z", "missing", "")
	e.run(t, other, "j5", "2026-11-15T09:55:00.000Z", "ok", `{"n":5}`)
	rep, ok := e.latest(t, e.repo)
	if !ok || rep.Report != `{"n":2}` || rep.FullName != "acme/repo-1" {
		t.Fatalf("report = %+v, %v", rep, ok)
	}
}

func TestRecordDependenciesReplacesTheSetAndLeavesAMovedOnRowAlone(t *testing.T) {
	e := newPREnv(t)
	e.upsert(t, store.UpdatePRInput{EventAt: at(5), OpenedAt: at(0), HeadSHA: "aaa"})
	record := func(eventAt int, keepEmpty bool, deps ...store.UpdatePRDependency) store.DependenciesResult {
		tx, err := e.db.Writer.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := e.repos.RecordDependencies(context.Background(), tx, store.DependenciesInput{
			RepositoryID: e.repo, Number: 21, EventAt: at(eventAt), Dependencies: deps, KeepEmpty: keepEmpty, Now: at(60),
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return res
	}
	names := func() []string {
		rows, err := e.db.Reader.Query(`SELECT name FROM update_pr_dependencies ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			out = append(out, n)
		}
		return out
	}
	dep := func(n string) store.UpdatePRDependency {
		return store.UpdatePRDependency{Name: n, Source: store.DependencySourceBody}
	}

	if res := record(5, false, dep("a"), dep("b")); !res.Applied || !res.Changed || !reflect.DeepEqual(names(), []string{"a", "b"}) {
		t.Fatalf("result = %+v, names = %v", res, names())
	}
	if res := record(5, false, dep("b"), dep("a")); !res.Applied || res.Changed {
		t.Fatalf("the same set in another order changed: %+v", res)
	}
	if res := record(5, false, dep("c")); !res.Changed || !reflect.DeepEqual(names(), []string{"c"}) {
		t.Fatalf("result = %+v, names = %v", res, names())
	}
	if res := record(5, true); res.Changed || !reflect.DeepEqual(names(), []string{"c"}) {
		t.Fatalf("an empty set with KeepEmpty changed the list: %+v, %v", res, names())
	}
	// GitHub changed the row at minute 5; a set derived from minute 4 is older.
	if res := record(4, false, dep("old")); res.Applied || !reflect.DeepEqual(names(), []string{"c"}) {
		t.Fatalf("an older derivation was applied: %+v, %v", res, names())
	}
	if res := record(5, false); !res.Changed || len(names()) != 0 {
		t.Fatalf("an empty set did not clear the list: %+v, %v", res, names())
	}
}
