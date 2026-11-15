package updateprs_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

var reportSeq int

// report stores a Renovate report as the result of a finished job of the repository.
func (r *rig) report(t *testing.T, json string) {
	t.Helper()
	reportSeq++
	job := fmt.Sprintf("job-%d", reportSeq)
	started := fmt.Sprintf("2026-11-15T11:%02d:00.000Z", reportSeq)
	if _, err := r.db.Writer.Exec(`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, "trigger", created_at)
		VALUES (?, 'renovate_run', ?, 'normal', 'succeeded', ?, 'schedule', ?)`, job, r.repo, started, started); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Writer.Exec(`INSERT INTO job_runs (id, job_id, attempt, started_at, outcome, report_status, report)
		VALUES (?, ?, 1, ?, 'succeeded', 'ok', ?)`, "run-"+job, job, started, json); err != nil {
		t.Fatal(err)
	}
}

type depRow struct{ Name, Manager, Datasource, From, To, Type, Source string }

func (r *rig) deps(t *testing.T, number int64) []depRow {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT d.name, COALESCE(d.manager, ''), COALESCE(d.datasource, ''), COALESCE(d.from_version, ''),
		COALESCE(d.to_version, ''), COALESCE(d.update_type, ''), d.source
		FROM update_pr_dependencies d JOIN update_prs p ON p.id = d.update_pr_id
		WHERE p.repository_id = ? AND p.number = ? ORDER BY d.rowid`, r.repo, number)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []depRow
	for rows.Next() {
		var d depRow
		if err := rows.Scan(&d.Name, &d.Manager, &d.Datasource, &d.From, &d.To, &d.Type, &d.Source); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func (r *rig) depIDs(t *testing.T, number int64) string {
	t.Helper()
	var ids string
	err := r.db.Reader.QueryRow(`SELECT COALESCE(group_concat(d.id), '') FROM update_pr_dependencies d JOIN update_prs p ON p.id = d.update_pr_id
		WHERE p.number = ?`, number).Scan(&ids)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func (r *rig) notes(t *testing.T, number int64) (notes, reportSHA string, null bool) {
	t.Helper()
	var n, s *string
	if err := r.db.Reader.QueryRow(`SELECT release_notes, report_sha FROM update_prs WHERE number = ?`, number).Scan(&n, &s); err != nil {
		t.Fatal(err)
	}
	if s != nil {
		reportSHA = *s
	}
	if n == nil {
		return "", reportSHA, true
	}
	return *n, reportSHA, false
}

func kinds(events []updateprs.Event) []string {
	var out []string
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

const groupReport = `{"repositories":{"acme/repo-1":{"branches":[{"branchName":"renovate/group","prNo":21,"sha":"sha","upgrades":[
	{"depName":"lodash","manager":"npm","datasource":"npm","currentVersion":"4.17.20","newVersion":"4.17.21","updateType":"patch"},
	{"depName":"react","manager":"npm","datasource":"npm","currentVersion":"17.0.2","newVersion":"18.2.0","updateType":"major"}]}]}}}`

func tableBody(rows ...string) string {
	return "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n" + strings.Join(rows, "\n") +
		"\n\n---\n\n - [ ] <!-- rebase-check -->rebase\n\n<!--renovate-debug:eyJ4IjoxfQ==-->\n"
}

func TestAGroupedPRGetsOneRowPerDependencyFromTheReportAndAReplayReplacesNothing(t *testing.T) {
	r := newRig(t, true)
	r.report(t, groupReport)
	pr := prOf(21, botID, renovateBody)
	out := r.applyOutcome(t, pr, botID)
	want := []depRow{
		{"lodash", "npm", "npm", "4.17.20", "4.17.21", "patch", "report"},
		{"react", "npm", "npm", "17.0.2", "18.2.0", "major", "report"},
	}
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %+v, want %+v", got, want)
	}
	if _, sha, _ := r.notes(t, 21); sha != "sha" {
		t.Fatalf("report_sha = %q", sha)
	}
	if !out.Changed || !reflect.DeepEqual(kinds(out.Events), []string{updateprs.EventDependenciesRecorded}) || out.Events[0].Dependencies != 2 {
		t.Fatalf("outcome = %+v", out)
	}

	ids := r.depIDs(t, 21)
	replay := r.applyOutcome(t, pr, botID)
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) || r.depIDs(t, 21) != ids {
		t.Fatalf("a replay changed the rows: %+v", got)
	}
	if replay.Changed || len(replay.Events) != 1 {
		t.Fatalf("replay outcome = %+v, want unchanged with the event", replay)
	}
}

func TestANewReportReplacesTheRowsAsASet(t *testing.T) {
	r := newRig(t, true)
	r.report(t, groupReport)
	r.applyOutcome(t, prOf(21, botID, renovateBody), botID)
	r.apply(t, prOf(22, botID, tableBody("| [vue](https://vue.dev) | major | `2` -> `3` |")), botID)
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[{"prNo":21,"sha":"sha","upgrades":[
		{"depName":"react","manager":"npm","datasource":"npm","currentVersion":"17.0.2","newVersion":"18.3.0","updateType":"major"}]}]}}}`)
	out := r.applyOutcome(t, prOf(21, botID, renovateBody), botID)
	want := []depRow{{"react", "npm", "npm", "17.0.2", "18.3.0", "major", "report"}}
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) || !out.Changed {
		t.Fatalf("dependencies = %+v, outcome %+v", got, out)
	}
	if got := r.deps(t, 22); len(got) != 1 || got[0].Name != "vue" {
		t.Fatalf("another PR's dependencies were touched: %+v", got)
	}
}

func TestANewerReportWithoutThePRKeepsTheRowsReadAtTheCurrentHead(t *testing.T) {
	r := newRig(t, true)
	r.report(t, groupReport)
	r.apply(t, prOf(21, botID, renovateBody), botID)
	want := r.deps(t, 21)
	if len(want) != 2 || want[0].Source != "report" {
		t.Fatalf("dependencies = %+v", want)
	}
	// A run that failed part-way still stores a readable report, without the PR's branch.
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[]}}}`)
	// The reconcile after it applies the PR again at the same head, with a body no table of
	// which is recognised.
	u := at(5)
	out := r.applyOutcome(t, prOf(21, botID, renovateBody, func(p *github.PullRequest) { p.UpdatedAt = &u }), botID)
	if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %+v, want %+v", got, want)
	}
	if _, sha, _ := r.notes(t, 21); sha != "sha" {
		t.Fatalf("report_sha = %q", sha)
	}
	if !reflect.DeepEqual(kinds(out.Events), []string{updateprs.EventDependenciesRecorded}) || out.Events[0].Dependencies != 2 {
		t.Fatalf("events = %+v", out.Events)
	}
	// A new head commit is no longer covered by the stored report: the body takes over.
	h := at(6)
	r.apply(t, prOf(21, botID, tableBody("| [vue](https://vue.dev) | major | `2` -> `3` |"), func(p *github.PullRequest) {
		p.UpdatedAt, p.Head.SHA = &h, "newer"
	}), botID)
	if got := r.deps(t, 21); len(got) != 1 || got[0].Name != "vue" || got[0].Source != "body" {
		t.Fatalf("dependencies = %+v", got)
	}
}

func TestTheBodyTableFillsTheRowsWhenTheReportHasNoEntryOrIsForAnotherCommit(t *testing.T) {
	body := tableBody("| [lodash](https://lodash.com/) | patch | `4.17.20` -> `4.17.21` |", "| [react](https://react.dev) | major | `17.0.2` -> `18.2.0` |")
	want := []depRow{
		{"lodash", "", "", "4.17.20", "4.17.21", "patch", "body"},
		{"react", "", "", "17.0.2", "18.2.0", "major", "body"},
	}
	for name, setup := range map[string]func(*rig){
		"no report":              func(*rig) {},
		"a report of no PR":      func(r *rig) { r.report(t, `{"repositories":{"acme/repo-1":{"branches":[]}}}`) },
		"an unreadable report":   func(r *rig) { r.report(t, `{"repositories":"changed"}`) },
		"a report of an old sha": func(r *rig) { r.report(t, strings.Replace(groupReport, `"sha":"sha"`, `"sha":"older"`, 1)) },
	} {
		r := newRig(t, true)
		setup(r)
		r.apply(t, prOf(21, botID, body), botID)
		if got := r.deps(t, 21); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: dependencies = %+v, want %+v", name, got, want)
		}
	}
}

func TestAPRWithNeitherReportNorTableKeepsNoRowsAndStillEmitsTheEvent(t *testing.T) {
	r := newRig(t, true)
	out := r.applyOutcome(t, prOf(21, botID, renovateBody), botID)
	if got := r.deps(t, 21); len(got) != 0 {
		t.Fatalf("dependencies = %+v", got)
	}
	if !out.Changed || len(out.Events) != 1 || out.Events[0].Kind != updateprs.EventDependenciesRecorded || out.Events[0].Dependencies != 0 {
		t.Fatalf("outcome = %+v", out)
	}
}

func TestConfigMigrationAndOnboardingPRsGetNoRowsEvenWithATableLikeSection(t *testing.T) {
	r := newRig(t, true)
	// A report that names neither PR, as Renovate's own report does for them.
	r.report(t, groupReport)
	tableLike := "Configure Renovate\n\n| Setting | Value |\n|---|---|\n| [schedule](https://x) | `at any time` -> `before 5am` |\n\n" +
		"| Package | Detected |\n|---|---|\n| [npm](https://npmjs.com) | `package.json` |\n\n" +
		" - [ ] <!-- create-config-migration-pr -->Select.\n<!--renovate-debug:eyJ4IjoxfQ==-->\n"
	for _, tc := range []struct {
		number int64
		ref    string
	}{{30, "renovate/migrate-config"}, {31, "renovate/configure"}} {
		out := r.applyOutcome(t, prOf(tc.number, botID, tableLike, func(p *github.PullRequest) { p.Head.Ref = tc.ref }), botID)
		if got := r.deps(t, tc.number); len(got) != 0 {
			t.Errorf("PR %d: dependencies = %+v", tc.number, got)
		}
		if len(out.Events) != 1 || out.Events[0].Kind != updateprs.EventDependenciesRecorded || out.Events[0].Dependencies != 0 {
			t.Errorf("PR %d: events = %+v", tc.number, out.Events)
		}
	}
}

func TestAPRThatIsNoLongerOpenKeepsItsRowsWhenNothingDerivesAnyMore(t *testing.T) {
	r := newRig(t, true)
	r.report(t, groupReport)
	r.apply(t, prOf(21, botID, renovateBody), botID)
	// The next run's report no longer lists the merged PR's branch.
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[]}}}`)
	merged := at(30)
	r.apply(t, prOf(21, botID, renovateBody, func(p *github.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = "closed", true, &merged, &merged, &merged
	}), botID)
	if got := r.deps(t, 21); len(got) != 2 || got[0].Source != "report" {
		t.Fatalf("a merged PR lost its dependencies: %+v", got)
	}
	// An open PR whose sources are gone keeps none.
	r.apply(t, prOf(22, botID, tableBody("| [a](https://a) | patch | `1` -> `2` |")), botID)
	if got := r.deps(t, 22); len(got) != 1 {
		t.Fatalf("dependencies = %+v", got)
	}
	r.apply(t, prOf(22, botID, renovateBody, func(p *github.PullRequest) { u := at(5); p.UpdatedAt = &u }), botID)
	if got := r.deps(t, 22); len(got) != 0 {
		t.Fatalf("an open PR kept rows no source supports: %+v", got)
	}
}

func TestAStaleEventChangesNeitherRowsNorNotes(t *testing.T) {
	r := newRig(t, true)
	body := "### Release Notes\n\nfresh\n\n---\n"
	newer := at(10)
	r.apply(t, prOf(21, botID, renovateBody+tableBody("| [lodash](https://l) | patch | `1` -> `2` |")+body, func(p *github.PullRequest) { p.UpdatedAt = &newer }), botID)
	out := r.applyOutcome(t, prOf(21, botID, renovateBody+tableBody("| [stale](https://l) | patch | `1` -> `2` |")+"### Release Notes\n\nstale\n---\n"), botID)
	if got := r.deps(t, 21); len(got) != 1 || got[0].Name != "lodash" {
		t.Fatalf("dependencies = %+v", got)
	}
	if notes, _, _ := r.notes(t, 21); notes != "fresh" {
		t.Fatalf("release notes = %q", notes)
	}
	if out.Changed || len(out.Events) != 0 {
		t.Fatalf("a stale event produced %+v", out)
	}
}

func TestReleaseNotesAreStoredTruncatedAndRefreshedWhenThePRChanges(t *testing.T) {
	r := newRig(t, true)
	withNotes := func(notes string) string {
		return renovateBody + "\n---\n\n### Release Notes\n\n" + notes + "\n\n---\n\n### Configuration\n"
	}
	r.apply(t, prOf(21, botID, withNotes("first")), botID)
	if notes, _, null := r.notes(t, 21); notes != "first" || null {
		t.Fatalf("release notes = %q", notes)
	}
	// An edit on a later event refreshes the excerpt.
	u := at(5)
	out := r.applyOutcome(t, prOf(21, botID, withNotes("second"), func(p *github.PullRequest) { p.UpdatedAt = &u }), botID)
	if notes, _, _ := r.notes(t, 21); notes != "second" || !out.Changed {
		t.Fatalf("release notes = %q, outcome %+v", notes, out)
	}
	long := strings.Repeat("0123456789\n", 1000)
	u = at(6)
	r.apply(t, prOf(21, botID, withNotes(long), func(p *github.PullRequest) { p.UpdatedAt = &u }), botID)
	if notes, _, _ := r.notes(t, 21); len([]rune(notes)) > updateprs.MaxReleaseNotesChars || !strings.HasSuffix(notes, "…") {
		t.Fatalf("release notes have %d characters", len([]rune(notes)))
	}
	// A body that lost the section clears the excerpt.
	u = at(7)
	r.apply(t, prOf(21, botID, renovateBody, func(p *github.PullRequest) { p.UpdatedAt = &u }), botID)
	if _, _, null := r.notes(t, 21); !null {
		t.Fatal("release notes were kept after the section went")
	}
}

// supersedeRig stores three closed PRs and an open one for lodash, as the reconcile would have
// recorded them: 1 closed with lodash, 2 merged with lodash, 3 closed with an unrelated
// dependency, 4 closed with lodash of another manager.
func supersedeRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, true)
	body := func(name string) string { return tableBody("| [" + name + "](https://x) | patch | `1` -> `2` |") }
	closedAt := at(3)
	closed := func(number int64, name string) {
		r.apply(t, prOf(number, botID, body(name), func(p *github.PullRequest) {
			p.State, p.ClosedAt, p.UpdatedAt = "closed", &closedAt, &closedAt
		}), botID)
	}
	closed(1, "lodash")
	closed(3, "chalk")
	r.apply(t, prOf(2, botID, body("lodash"), func(p *github.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = "closed", true, &closedAt, &closedAt, &closedAt
	}), botID)
	return r
}

func TestAnOpenPRThatReplacesAClosedOneSupersedesItOnceAndNeverAMergedOrUnrelatedOne(t *testing.T) {
	r := supersedeRig(t)
	if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "closed", 2: "merged", 3: "closed"}) {
		t.Fatalf("states = %v", got)
	}
	open := prOf(5, botID, tableBody("| [lodash](https://x) | minor | `1` -> `3` |"))
	out := r.applyOutcome(t, open, botID)
	if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "superseded", 2: "merged", 3: "closed", 5: "open"}) {
		t.Fatalf("states = %v", got)
	}
	var by int64
	if err := r.db.Reader.QueryRow(`SELECT n.number FROM update_prs o JOIN update_prs n ON n.id = o.superseded_by_id WHERE o.number = 1`).Scan(&by); err != nil || by != 5 {
		t.Fatalf("superseded_by = %d, %v", by, err)
	}
	if !out.Changed || !reflect.DeepEqual(kinds(out.Events), []string{updateprs.EventDependenciesRecorded, updateprs.EventSuperseded}) ||
		out.Events[1].Number != 1 || out.Events[1].ByNumber != 5 {
		t.Fatalf("outcome = %+v", out)
	}

	replay := r.applyOutcome(t, open, botID)
	if replay.Changed || !reflect.DeepEqual(kinds(replay.Events), []string{updateprs.EventDependenciesRecorded}) {
		t.Fatalf("replay outcome = %+v", replay)
	}
	if got := r.states(t); got[1] != "superseded" || got[2] != "merged" || got[3] != "closed" {
		t.Fatalf("states after the replay = %v", got)
	}
}

func TestASupersedeNeedsTheSameManagerWhereBothAreKnownAndAHigherNumber(t *testing.T) {
	r := newRig(t, true)
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[
		{"prNo":1,"upgrades":[{"depName":"lodash","manager":"npm","datasource":"npm"}]},
		{"prNo":2,"upgrades":[{"depName":"lodash","manager":"pip_requirements","datasource":"pypi"}]},
		{"prNo":9,"upgrades":[{"depName":"lodash","manager":"npm","datasource":"npm"}]}]}}}`)
	closedAt := at(3)
	for _, n := range []int64{1, 2, 9} {
		r.apply(t, prOf(n, botID, renovateBody, func(p *github.PullRequest) { p.State, p.ClosedAt, p.UpdatedAt = "closed", &closedAt, &closedAt }), botID)
	}
	// 9 is closed too, so it replaces nothing; 5 is open with lodash from npm.
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[{"prNo":5,"upgrades":[{"depName":"lodash","manager":"npm","datasource":"npm"}]}]}}}`)
	r.apply(t, prOf(5, botID, renovateBody), botID)
	if got := r.states(t); got[1] != "superseded" || got[2] != "closed" || got[9] != "closed" {
		t.Fatalf("states = %v", got)
	}
}

func TestACloseAfterTheReplacementWasRecordedSupersedesTheClosedPR(t *testing.T) {
	r := newRig(t, true)
	r.apply(t, prOf(1, botID, tableBody("| [lodash](https://x) | patch | `1` -> `2` |")), botID)
	r.apply(t, prOf(5, botID, tableBody("| [lodash](https://x) | minor | `1` -> `3` |")), botID)
	closedAt := at(3)
	out := r.applyOutcome(t, prOf(1, botID, tableBody("| [lodash](https://x) | patch | `1` -> `2` |"), func(p *github.PullRequest) {
		p.State, p.ClosedAt, p.UpdatedAt = "closed", &closedAt, &closedAt
	}), botID)
	if got := r.states(t); got[1] != "superseded" || got[5] != "open" {
		t.Fatalf("states = %v", got)
	}
	if len(out.Events) != 2 || out.Events[1].Kind != updateprs.EventSuperseded || out.Events[1].Number != 1 || out.Events[1].ByNumber != 5 {
		t.Fatalf("events = %+v", out.Events)
	}
	// A closed report of the superseded PR keeps it superseded and emits nothing more.
	again := r.applyOutcome(t, prOf(1, botID, tableBody("| [lodash](https://x) | patch | `1` -> `2` |"), func(p *github.PullRequest) {
		p.State, p.ClosedAt, p.UpdatedAt = "closed", &closedAt, &closedAt
	}), botID)
	if got := r.states(t); got[1] != "superseded" || again.Changed || len(again.Events) != 1 {
		t.Fatalf("states = %v, outcome %+v", got, again)
	}
}

func TestTheReconcileAfterAJobResultRecordsTheDependenciesFromTheStoredReport(t *testing.T) {
	r := newRig(t, true)
	r.bot(21, func(p *githubtest.PullRequest) { p.HeadRef, p.HeadSHA = "renovate/group", "sha" })
	r.report(t, groupReport)
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := r.deps(t, 21); len(got) != 2 || got[1].Name != "react" || got[1].Source != "report" {
		t.Fatalf("dependencies = %+v", got)
	}
	if !r.woken() {
		t.Fatal("recording dependencies did not wake the engine")
	}
	// The next run reports one more dependency; the reconcile after it replaces the set, and an
	// unchanged reconcile wakes nothing.
	r.report(t, strings.Replace(groupReport, `"updateType":"major"}]`, `"updateType":"major"},{"depName":"vue","currentVersion":"2","newVersion":"3","updateType":"major"}]`, 1))
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	if got := r.deps(t, 21); len(got) != 3 || !r.woken() {
		t.Fatalf("dependencies = %+v", got)
	}
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	if r.woken() {
		t.Fatal("an unchanged reconcile woke the engine")
	}
}

func (r *rig) automerge(t *testing.T, number int64) string {
	t.Helper()
	var v *int
	if err := r.db.Reader.QueryRow(`SELECT automerge FROM update_prs WHERE number = ?`, number).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return "unknown"
	}
	return fmt.Sprint(*v)
}

// Whether a PR is automerged is GitHub's auto_merge field of the payload, and the report's flag
// for the PR's branch where the report has it; neither says: unknown.
func TestAutomergeComesFromThePayloadAndTheReport(t *testing.T) {
	r := newRig(t, true)
	body := tableBody("| [vue](https://vue.dev) | major | `2` -> `3` |")

	r.apply(t, prOf(21, botID, body), botID)
	if got := r.automerge(t, 21); got != "unknown" {
		t.Fatalf("a payload without auto_merge: automerge = %s, want unknown", got)
	}
	r.apply(t, prOf(22, botID, body, func(p *github.PullRequest) { p.AutoMerge = github.AutoMerge{Present: true} }), botID)
	if got := r.automerge(t, 22); got != "0" {
		t.Fatalf("auto_merge null: automerge = %s, want 0", got)
	}
	updated := at(5)
	enabled := func(p *github.PullRequest) {
		p.AutoMerge = github.AutoMerge{Present: true, Enabled: true}
		p.UpdatedAt = &updated
	}
	r.apply(t, prOf(22, botID, body, enabled), botID)
	if got := r.automerge(t, 22); got != "1" {
		t.Fatalf("auto_merge set: automerge = %s, want 1", got)
	}

	// Renovate's own automerge of the branch, from the report, refines the payload.
	r.report(t, `{"repositories":{"acme/repo-1":{"branches":[
		{"branchName":"renovate/a","prNo":23,"sha":"sha","automerge":true,"upgrades":[{"depName":"a","currentVersion":"1","newVersion":"2","updateType":"minor"}]},
		{"branchName":"renovate/b","prNo":24,"sha":"sha","automerge":false,"upgrades":[{"depName":"b","currentVersion":"1","newVersion":"2","updateType":"minor"}]}]}}}`)
	r.apply(t, prOf(23, botID, renovateBody, func(p *github.PullRequest) { p.AutoMerge = github.AutoMerge{Present: true} }), botID)
	if got := r.automerge(t, 23); got != "1" {
		t.Fatalf("the report's automerge over an empty auto_merge: automerge = %s, want 1", got)
	}
	r.apply(t, prOf(24, botID, renovateBody, func(p *github.PullRequest) { p.AutoMerge = github.AutoMerge{Present: true, Enabled: true} }), botID)
	if got := r.automerge(t, 24); got != "0" {
		t.Fatalf("the report's automerge over an enabled auto_merge: automerge = %s, want 0", got)
	}
}
