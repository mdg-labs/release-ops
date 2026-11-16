package updatetickets_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updateprs"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

// distinctMapping names a superseded status that is none of the others, so a supersede is a real
// move in the ticket system.
const distinctMapping = `{"open":["ready","in-progress"],"done":["done"],"cancelled":["cancelled"],"superseded":"superseded"}`

// ticketed creates the ticket of a new open PR and returns the PR id.
func (e *env) ticketed(t *testing.T, p pr) string {
	t.Helper()
	if p.Deps == nil {
		p.Deps = foo
	}
	id := e.addPR(t, p)
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process = %s, %v, want created", out, err)
	}
	return id
}

func (e *env) useMapping(t *testing.T, mapping string) {
	t.Helper()
	e.exec(t, `UPDATE ticket_projects SET status_mapping = ? WHERE id = ?`, mapping, e.project)
}

func (e *env) setState(t *testing.T, prID, state string) {
	t.Helper()
	e.exec(t, `UPDATE update_prs SET state = ? WHERE id = ?`, state, prID)
}

func (e *env) replace(t *testing.T, oldID, newID string) {
	t.Helper()
	e.exec(t, `UPDATE update_prs SET state = 'superseded', superseded_by_id = ? WHERE id = ?`, newID, oldID)
}

func (e *env) setDeps(t *testing.T, prID string, deps []dep) {
	t.Helper()
	e.exec(t, `DELETE FROM update_pr_dependencies WHERE update_pr_id = ?`, prID)
	for i, d := range deps {
		e.exec(t, `INSERT INTO update_pr_dependencies (id, update_pr_id, name, from_version, to_version, update_type, source) VALUES (?, ?, ?, ?, ?, ?, 'report')`,
			fmt.Sprintf("%s-n%d", prID, i), prID, d.Name, nullable(d.From), nullable(d.To), nullable(d.Type))
	}
}

func syncPR(t *testing.T, e *env, id string) (updatetickets.SyncOutcome, error) {
	t.Helper()
	return e.engine.Sync(context.Background(), id)
}

func wantSync(t *testing.T, e *env, id string, want updatetickets.SyncOutcome) {
	t.Helper()
	if out, err := syncPR(t, e, id); err != nil || out != want {
		t.Fatalf("Sync = %s, %v, want %s", out, err, want)
	}
}

func paths(rs []trackerRequest) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func TestANewVersionOfThePRUpdatesTheTicketAndAddsANote(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	row, _ := e.row(t, id)
	created := len(e.tracker.calls())

	// Nothing changed: no call to the ticket system.
	wantSync(t, e, id, updatetickets.SyncNothing)
	if n := len(e.tracker.calls()); n != created {
		t.Fatalf("calls = %d, want %d: an unchanged PR calls nothing", n, created)
	}

	e.setDeps(t, id, []dep{{"foo", "1.4.0", "3.0.0", "major"}, {"bar", "1", "2", "minor"}})
	wantSync(t, e, id, updatetickets.SyncUpdated)

	want := []string{"POST /api/task/DEV", "PUT /api/task/title/task-1", "PUT /api/task/description/task-1", "POST /api/comment/task-1"}
	if got := paths(e.tracker.writes()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("writes = %v, want %v", got, want)
	}
	task := e.tracker.task("task-1")
	for _, w := range []string{"- foo: 1.4.0 → 3.0.0 (major)", "- bar: 1 → 2 (minor)", "Release Ops key: " + row.ID} {
		if !strings.Contains(task.Description, w) {
			t.Errorf("description lacks %q:\n%s", w, task.Description)
		}
	}
	if len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "Pull request updated:") || !strings.Contains(task.Comments[0], "- foo: 1.4.0 → 3.0.0 (major)") {
		t.Errorf("comments = %q, want the update note listing the new versions", task.Comments)
	}
	after, _ := e.row(t, id)
	if after.SyncedHash.String == row.SyncedHash.String || after.LastError.Valid || after.State != "open" {
		t.Errorf("row = %+v, want the new hash stored on an open ticket", after)
	}

	n := len(e.tracker.calls())
	wantSync(t, e, id, updatetickets.SyncNothing)
	if len(e.tracker.calls()) != n {
		t.Fatalf("a second Sync called the ticket system: %v", paths(e.tracker.calls()[n:]))
	}
}

func TestAnUpdateThatFailsKeepsTheOldHashAndIsRetriedWhenDue(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	before, _ := e.row(t, id)
	e.setDeps(t, id, []dep{{"foo", "1.4.0", "3.0.0", "major"}})
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/comment/", Code: http.StatusBadGateway, Times: 1})

	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want a failure", out, err)
	}
	row, _ := e.row(t, id)
	if row.SyncedHash.String != before.SyncedHash.String || !strings.Contains(row.LastError.String, "502") || row.Attempts != 1 || !row.NextAttemptAt.Valid {
		t.Fatalf("row = %+v, want the old hash, the 502 recorded, one attempt and a retry", row)
	}
	wantSync(t, e, id, updatetickets.SyncWaiting)

	e.advance(time.Hour)
	wantSync(t, e, id, updatetickets.SyncUpdated)
	row, _ = e.row(t, id)
	if row.SyncedHash.String == before.SyncedHash.String || row.LastError.Valid || row.Attempts != 0 || row.NextAttemptAt.Valid {
		t.Fatalf("row = %+v, want the new hash and the failure state cleared", row)
	}
}

// pendingContent is the dependencies hash recorded for an update that is not finished.
func (e *env) pendingContent(t *testing.T, prID string) sql.NullString {
	t.Helper()
	var h sql.NullString
	if err := e.db.Reader.QueryRow(`SELECT pending_content_hash FROM update_tickets WHERE update_pr_id = ?`, prID).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestAnUpdateWhoseNoteFailedIsDueAgainAfterThePRRevertsToTheSyncedVersion(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	before, _ := e.row(t, id)
	if h := e.pendingContent(t, id); h.Valid {
		t.Fatalf("pending content hash = %v on a new ticket, want NULL", h)
	}

	// The PR changes to B; the content reaches the ticket and its note fails.
	e.setDeps(t, id, []dep{{"foo", "1.4.0", "3.0.0", "major"}})
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/comment/", Code: http.StatusBadGateway, Times: 1})
	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want a failure", out, err)
	}
	if desc := e.tracker.task("task-1").Description; !strings.Contains(desc, "1.4.0 → 3.0.0") {
		t.Fatalf("description = %q, want the B content sent before the note failed", desc)
	}
	row, _ := e.row(t, id)
	if row.SyncedHash.String != before.SyncedHash.String {
		t.Fatalf("synced hash = %q, want the old one %q", row.SyncedHash.String, before.SyncedHash.String)
	}
	if h := e.pendingContent(t, id); !h.Valid || h.String == before.SyncedHash.String {
		t.Fatalf("pending content hash = %v, want the hash of the B content", h)
	}

	// The PR changes back to A before the retry: the ticket is brought back to A.
	e.setDeps(t, id, foo)
	e.advance(time.Hour)
	wantSync(t, e, id, updatetickets.SyncUpdated)
	task := e.tracker.task("task-1")
	if !strings.Contains(task.Description, "- foo: 1.4.0 → 2.0.0 (major)") || strings.Contains(task.Description, "3.0.0") {
		t.Errorf("description = %q, want the A content back", task.Description)
	}
	if len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "2.0.0") {
		t.Errorf("comments = %q, want the note of the A update", task.Comments)
	}
	row, _ = e.row(t, id)
	if row.SyncedHash.String != before.SyncedHash.String || row.LastError.Valid || row.Attempts != 0 || row.NextAttemptAt.Valid {
		t.Errorf("row = %+v, want the A hash and the failure state cleared", row)
	}
	if h := e.pendingContent(t, id); h.Valid {
		t.Errorf("pending content hash = %v, want it cleared by the successful update", h)
	}

	n := len(e.tracker.calls())
	wantSync(t, e, id, updatetickets.SyncNothing)
	if len(e.tracker.calls()) != n {
		t.Fatalf("a Sync after the update called the ticket system: %v", paths(e.tracker.calls()[n:]))
	}
}

func TestAnUpdateWhoseNoteKeepsFailingStaysDueUntilItsNoteIsAdded(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	e.setDeps(t, id, []dep{{"foo", "1.4.0", "3.0.0", "major"}})
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/comment/", Code: http.StatusBadGateway, Times: 2})

	for range 2 {
		if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
			t.Fatalf("Sync = %s, %v, want a failure", out, err)
		}
		if h := e.pendingContent(t, id); !h.Valid {
			t.Fatal("pending content hash cleared after a failed note, want it kept")
		}
		e.advance(time.Hour)
	}
	wantSync(t, e, id, updatetickets.SyncUpdated)
	if h := e.pendingContent(t, id); h.Valid {
		t.Errorf("pending content hash = %v, want it cleared once the note was added", h)
	}
	if got := e.tracker.task("task-1").Comments; len(got) != 1 {
		t.Errorf("comments = %q, want exactly the one note", got)
	}
}

func TestAMergedPRMovesItsTicketToDoneOnceAndAReopenedPRLeavesItAlone(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})

	e.setState(t, id, "merged")
	wantSync(t, e, id, updatetickets.SyncTransitioned)
	if got := e.tracker.task("task-1").Status; got != "done" {
		t.Fatalf("task status = %q, want done", got)
	}
	row, _ := e.row(t, id)
	if row.State != "done" || row.TargetState.Valid || row.LastError.Valid {
		t.Fatalf("row = %+v, want done with no recorded transition", row)
	}
	n := len(e.tracker.calls())
	wantSync(t, e, id, updatetickets.SyncNothing)
	e.setState(t, id, "open")
	wantSync(t, e, id, updatetickets.SyncNothing)
	if len(e.tracker.calls()) != n {
		t.Fatalf("a done ticket was looked at again: %v", paths(e.tracker.calls()[n:]))
	}
}

func TestAClosedUnmergedPRMovesItsTicketToWontDoAndAReopenLeavesItThere(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})

	e.setState(t, id, "closed")
	wantSync(t, e, id, updatetickets.SyncTransitioned)
	if got := e.tracker.task("task-1").Status; got != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", got)
	}
	if row, _ := e.row(t, id); row.State != "cancelled" {
		t.Fatalf("state = %q, want cancelled", row.State)
	}

	// The PR is reopened, and its dependencies change: the ticket is not moved back, edited or
	// replaced by a second one.
	n := len(e.tracker.calls())
	e.setState(t, id, "open")
	e.setDeps(t, id, []dep{{"foo", "1.4.0", "9.0.0", "major"}})
	wantSync(t, e, id, updatetickets.SyncNothing)
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeExists {
		t.Fatalf("Process = %s, %v, want exists: no second ticket", out, err)
	}
	if len(e.tracker.calls()) != n || e.rowCount(t) != 1 {
		t.Fatalf("calls = %v, rows = %d, want the reopened PR to leave its ticket as it is", paths(e.tracker.calls()[n:]), e.rowCount(t))
	}
}

func TestAnEmptyStatusListIsAClearErrorAndNoCallUntilTheMappingIsFixed(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, `{"open":["ready"],"done":[],"cancelled":["cancelled"],"superseded":"cancelled"}`)
	id := e.ticketed(t, pr{})
	n := len(e.tracker.calls())

	e.setState(t, id, "merged")
	out, err := syncPR(t, e, id)
	if err == nil || out != updatetickets.SyncFailed || !strings.Contains(err.Error(), "status_mapping.done has no entry") {
		t.Fatalf("Sync = %s, %v, want the empty list named", out, err)
	}
	row, _ := e.row(t, id)
	if !strings.Contains(row.LastError.String, "status_mapping.done") || row.State != "open" || row.TargetState.Valid || len(e.tracker.calls()) != n {
		t.Fatalf("row = %+v with %d new calls, want the error recorded, no recorded transition and no call", row, len(e.tracker.calls())-n)
	}

	e.useMapping(t, `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`)
	e.advance(time.Hour)
	wantSync(t, e, id, updatetickets.SyncTransitioned)
}

func TestATransitionThatFailsIsRecordedAndRetriedAndNeverLost(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	e.tracker.fail(trackerFailure{Method: http.MethodPut, PathPrefix: "/api/task/status/", Code: http.StatusServiceUnavailable, Times: 1})

	e.setState(t, id, "merged")
	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want a failure", out, err)
	}
	row, _ := e.row(t, id)
	if row.State != "open" || row.TargetState.String != "done" || !strings.Contains(row.LastError.String, "503") || row.Attempts != 1 {
		t.Fatalf("row = %+v, want open, the transition recorded, the 503 and one attempt", row)
	}
	ids, err := e.st.UpdateTickets().SyncablePRs(context.Background(), e.clock().Add(time.Hour))
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("SyncablePRs = %v, %v, want the PR once its retry is due", ids, err)
	}
	if ids, _ := e.st.UpdateTickets().SyncablePRs(context.Background(), e.clock()); len(ids) != 0 {
		t.Fatalf("SyncablePRs before the retry is due = %v, want none", ids)
	}

	e.advance(time.Hour)
	wantSync(t, e, id, updatetickets.SyncTransitioned)
	row, _ = e.row(t, id)
	if row.State != "done" || row.TargetState.Valid || row.LastError.Valid || row.Attempts != 0 {
		t.Fatalf("row = %+v, want done and the failure state cleared", row)
	}
}

// A transition the ticket system applied and then failed to answer is finished from the live
// status, not repeated.
func TestATransitionTheTrackerAppliedBeforeItsErrorIsFinishedWithoutASecondCall(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	e.tracker.fail(trackerFailure{Method: http.MethodPut, PathPrefix: "/api/task/status/", Code: http.StatusBadGateway, Times: 1, Applied: true})

	e.setState(t, id, "merged")
	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want a failure", out, err)
	}
	e.advance(time.Hour)
	wantSync(t, e, id, updatetickets.SyncTransitioned)

	statusPuts := 0
	for _, r := range e.tracker.writes() {
		if strings.HasPrefix(r.Path, "/api/task/status/") {
			statusPuts++
		}
	}
	row, _ := e.row(t, id)
	if statusPuts != 1 || row.State != "done" || row.HandsOffAt.Valid {
		t.Fatalf("status writes = %d, row = %+v, want one write and a done ticket that was not handed off", statusPuts, row)
	}
}

// failingState makes the write of a transition's new state fail, as a stop between the call to
// the ticket system and that write.
type failingState struct {
	store.UpdateTicketRepository
	fail error
}

func (f *failingState) CompleteTransition(ctx context.Context, id, state string, now time.Time) error {
	if f.fail != nil {
		return f.fail
	}
	return f.UpdateTicketRepository.CompleteTransition(ctx, id, state, now)
}

func TestAStopBetweenTheTransitionCallAndTheStateWriteIsNotTakenForAChangeByHand(t *testing.T) {
	e := newEnv(t)
	flaky := &failingState{UpdateTicketRepository: e.st.UpdateTickets(), fail: errors.New("database is locked")}
	e.engine.Tickets = flaky
	id := e.ticketed(t, pr{})

	e.setState(t, id, "merged")
	if out, err := syncPR(t, e, id); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want the failed state write", out, err)
	}
	if got := e.tracker.task("task-1").Status; got != "done" {
		t.Fatalf("task status = %q, want the ticket already moved", got)
	}
	row, _ := e.row(t, id)
	if row.State != "open" || row.TargetState.String != "done" {
		t.Fatalf("row = %+v, want the old state and the transition recorded", row)
	}

	flaky.fail = nil
	n := len(e.tracker.calls())
	wantSync(t, e, id, updatetickets.SyncTransitioned)
	row, _ = e.row(t, id)
	if row.State != "done" || row.TargetState.Valid || row.HandsOffAt.Valid {
		t.Fatalf("row = %+v, want done, finished from the recorded transition and not handed off", row)
	}
	if got := paths(e.tracker.calls()[n:]); fmt.Sprint(got) != "[GET /api/task/task-1]" {
		t.Fatalf("calls after the stop = %v, want only the live status read and no second move", got)
	}
}

func TestATicketChangedByHandIsLeftAloneForGood(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	e.tracker.setTaskStatus("task-1", "done")
	n := len(e.tracker.calls())

	e.setState(t, id, "closed")
	wantSync(t, e, id, updatetickets.SyncHandedOff)
	row, _ := e.row(t, id)
	if !row.HandsOffAt.Valid || row.State != "open" || row.LastError.Valid {
		t.Fatalf("row = %+v, want it handed off with its state kept", row)
	}
	if got := paths(e.tracker.calls()[n:]); fmt.Sprint(got) != "[GET /api/task/task-1]" {
		t.Fatalf("calls = %v, want only the live status read", got)
	}

	// A later merge, a new version or a replacement touch nothing, and nothing is read.
	n = len(e.tracker.calls())
	e.setState(t, id, "merged")
	wantSync(t, e, id, updatetickets.SyncNothing)
	e.replace(t, id, e.addPR(t, pr{Number: 40, Deps: foo}))
	wantSync(t, e, id, updatetickets.SyncNothing)
	if ids, _ := e.st.UpdateTickets().SyncablePRs(context.Background(), e.clock().Add(time.Hour)); len(ids) != 0 {
		t.Fatalf("SyncablePRs = %v, want a handed-off ticket left out", ids)
	}
	if len(e.tracker.calls()) != n {
		t.Fatalf("a handed-off ticket was called: %v", paths(e.tracker.calls()[n:]))
	}
}

func TestAStatusNotInTheMappingIsAnErrorNotAGuess(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	e.tracker.setTaskStatus("task-1", "triage")

	e.setState(t, id, "merged")
	out, err := syncPR(t, e, id)
	row, _ := e.row(t, id)
	if err == nil || out != updatetickets.SyncFailed || !strings.Contains(row.LastError.String, `"triage" is not in the status mapping`) || row.HandsOffAt.Valid {
		t.Fatalf("Sync = %s, %v with row %+v, want the status named, recorded and no hand-off", out, err, row)
	}
	if len(e.tracker.writes()) != 1 {
		t.Fatalf("writes = %v, want only the create", paths(e.tracker.writes()))
	}
}

func TestAReplacedPRSupersedesItsTicketAndLinksTheNewPRsTicket(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.addPR(t, pr{Number: 22, Deps: []dep{{"foo", "1.4.0", "2.1.0", "major"}}})
	e.replace(t, old, fresh)

	// The new PR is going to get a ticket: the old one waits for it and calls nothing.
	n := len(e.tracker.calls())
	wantSync(t, e, old, updatetickets.SyncWaiting)
	if len(e.tracker.calls()) != n {
		t.Fatalf("a waiting supersede called the ticket system: %v", paths(e.tracker.calls()[n:]))
	}

	if out, err := process(t, e, fresh); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process(new) = %s, %v", out, err)
	}
	newRow, _ := e.row(t, fresh)
	wantSync(t, e, old, updatetickets.SyncSuperseded)

	task := e.tracker.task("task-1")
	if task.Status != "superseded" {
		t.Fatalf("old task status = %q, want superseded", task.Status)
	}
	if len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "Superseded by https://github.com/acme/repo-1/pull/22") ||
		!strings.Contains(task.Comments[0], "New ticket: "+newRow.ExternalURL.String) {
		t.Fatalf("comments = %q, want the new PR and its ticket linked", task.Comments)
	}
	if row, _ := e.row(t, old); row.State != "superseded" || row.TargetState.Valid {
		t.Fatalf("row = %+v, want superseded", row)
	}
	if len(e.tracker.task("task-2").Comments) != 0 || e.tracker.task("task-2").Status != "ready" {
		t.Fatalf("the new ticket was touched: %+v", e.tracker.task("task-2"))
	}
	n = len(e.tracker.calls())
	wantSync(t, e, old, updatetickets.SyncNothing)
	if len(e.tracker.calls()) != n {
		t.Fatalf("a superseded ticket was looked at again")
	}
}

func TestACreatedTicketReleasesThePRsItReplacedThroughTheEngineRun(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.addPR(t, pr{Number: 22, Deps: foo})
	e.replace(t, old, fresh)
	e.engine.SweepInterval = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	eventually(t, "the old ticket to be superseded", func() bool {
		row, _ := e.row(t, old)
		return row.State == "superseded"
	})
	if task := e.tracker.task("task-1"); task.Status != "superseded" || len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "New ticket: ") {
		t.Fatalf("old task = %+v, want superseded and linked to the new ticket", task)
	}
	if n := len(e.tracker.task("task-2").Comments); n != 0 {
		t.Fatalf("the new ticket has %d comments, want none", n)
	}
}

func TestAReplacementThatGetsNoTicketIsNamedWithoutATicketLink(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	e.exec(t, `UPDATE ticket_projects SET filter = '{"updateTypes":[],"labels":["wanted"],"skipAutomerged":true}' WHERE id = ?`, e.project)
	old := e.ticketed(t, pr{Number: 21, Labels: []string{"wanted"}})
	fresh := e.addPR(t, pr{Number: 22, Deps: foo})
	e.replace(t, old, fresh)

	wantSync(t, e, old, updatetickets.SyncSuperseded)
	task := e.tracker.task("task-1")
	if task.Status != "superseded" || len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "pull/22") || strings.Contains(task.Comments[0], "New ticket") {
		t.Fatalf("task = %+v, want superseded and a comment naming only the new PR", task)
	}
}

// The PR was closed unmerged and its ticket moved to won't do; the supersede check then finds the
// replacement and reclassifies the closed PR as superseded.
func TestACloseThatIsLaterReclassifiedAsSupersededMovesTheWontDoTicketAndLinksTheNewTicket(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.ticketed(t, pr{Number: 22, Deps: []dep{{"foo", "1.4.0", "2.1.0", "major"}}})
	newRow, _ := e.row(t, fresh)

	e.setState(t, old, "closed")
	wantSync(t, e, old, updatetickets.SyncTransitioned)
	if got := e.tracker.task("task-1").Status; got != "cancelled" {
		t.Fatalf("task status after the close = %q, want cancelled", got)
	}

	e.replace(t, old, fresh)
	wantSync(t, e, old, updatetickets.SyncSuperseded)
	task := e.tracker.task("task-1")
	if task.Status != "superseded" || len(task.Comments) != 1 || !strings.Contains(task.Comments[0], "New ticket: "+newRow.ExternalURL.String) {
		t.Fatalf("task = %+v, want moved from won't do to superseded and linked to the new ticket", task)
	}
	if row, _ := e.row(t, old); row.State != "superseded" {
		t.Fatalf("state = %q, want superseded", row.State)
	}
}

func TestAWontDoTicketThatWasMovedByHandIsNotSuperseded(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.ticketed(t, pr{Number: 22, Deps: foo})

	e.setState(t, old, "closed")
	wantSync(t, e, old, updatetickets.SyncTransitioned)
	e.tracker.setTaskStatus("task-1", "ready")
	e.replace(t, old, fresh)
	wantSync(t, e, old, updatetickets.SyncHandedOff)
	if task := e.tracker.task("task-1"); task.Status != "ready" || len(task.Comments) != 0 {
		t.Fatalf("task = %+v, want it left as the person set it", task)
	}
}

func TestASupersedeWhoseCommentFailsIsFinishedWithoutMovingTheTicketAgain(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.ticketed(t, pr{Number: 22, Deps: foo})
	e.replace(t, old, fresh)
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/comment/task-1", Code: http.StatusBadGateway, Times: 1})

	if out, err := syncPR(t, e, old); err == nil || out != updatetickets.SyncFailed {
		t.Fatalf("Sync = %s, %v, want the comment to fail", out, err)
	}
	row, _ := e.row(t, old)
	if row.State != "open" || row.TargetState.String != "superseded" || !strings.Contains(row.LastError.String, "502") {
		t.Fatalf("row = %+v, want the supersede recorded and the 502 shown", row)
	}

	e.advance(time.Hour)
	n := len(e.tracker.calls())
	wantSync(t, e, old, updatetickets.SyncSuperseded)
	if got := paths(e.tracker.calls()[n:]); fmt.Sprint(got) != "[GET /api/task/task-1 POST /api/comment/task-1]" {
		t.Fatalf("calls = %v, want the live read and the comment, no second move", got)
	}
	if task := e.tracker.task("task-1"); task.Status != "superseded" || len(task.Comments) != 1 {
		t.Fatalf("task = %+v, want superseded with one comment", task)
	}
	if row, _ := e.row(t, old); row.State != "superseded" || row.TargetState.Valid || row.LastError.Valid {
		t.Fatalf("row = %+v, want superseded and clean", row)
	}
}

func TestASupersedeNeedsTheSupersededStatusOfTheMapping(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, `{"open":["ready"],"done":["done"],"cancelled":["cancelled"],"superseded":""}`)
	old := e.ticketed(t, pr{Number: 21})
	e.replace(t, old, e.ticketed(t, pr{Number: 22, Deps: foo}))
	n := len(e.tracker.calls())

	out, err := syncPR(t, e, old)
	if err == nil || out != updatetickets.SyncFailed || !strings.Contains(err.Error(), "status_mapping.superseded is empty") || len(e.tracker.calls()) != n {
		t.Fatalf("Sync = %s, %v with %d new calls, want the empty value named and no call", out, err, len(e.tracker.calls())-n)
	}
}

// A ticket moved to the mapped superseded status counts as cancelled, which is what the stored
// state says too.
func TestASupersededStatusThatEqualsWontDoMakesNoStatusCall(t *testing.T) {
	e := newEnv(t)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.ticketed(t, pr{Number: 22, Deps: foo})
	e.setState(t, old, "closed")
	wantSync(t, e, old, updatetickets.SyncTransitioned)
	e.replace(t, old, fresh)
	n := len(e.tracker.writes())

	wantSync(t, e, old, updatetickets.SyncSuperseded)
	added := paths(e.tracker.writes()[n:])
	if fmt.Sprint(added) != "[POST /api/comment/task-1]" {
		t.Fatalf("writes = %v, want the comment only: the ticket is in the status already", added)
	}
}

func TestAStuckMarkerIsClearedByTheOperatorAndTheNextEventCreatesExactlyOneTicket(t *testing.T) {
	e := newEnv(t)
	e.tracker.fail(trackerFailure{Method: http.MethodPost, PathPrefix: "/api/task/", Code: http.StatusServiceUnavailable, Times: 1})
	id := e.addPR(t, pr{Deps: foo})

	if out, err := process(t, e, id); err == nil || out != updatetickets.OutcomeFailed {
		t.Fatalf("Process = %s, %v, want the 503", out, err)
	}
	e.advance(time.Hour)
	for range 3 {
		if out, err := process(t, e, id); !errors.Is(err, updatetickets.ErrUnconfirmed) || out != updatetickets.OutcomeUnconfirmed {
			t.Fatalf("a repeated event = %s, %v, want the refusal", out, err)
		}
	}
	if n := len(e.tracker.calls()); n != 1 {
		t.Fatalf("calls = %d, want only the failed create: a kept marker creates nothing", n)
	}
	row, _ := e.row(t, id)
	if !row.PendingSince.Valid || row.State != "pending" {
		t.Fatalf("row = %+v, want the marker kept", row)
	}

	if err := e.st.UpdateTickets().ClearMarker(context.Background(), row.ID, e.clock()); err != nil {
		t.Fatalf("ClearMarker: %v", err)
	}
	cleared, _ := e.row(t, id)
	if cleared.PendingSince.Valid || cleared.LastError.Valid || cleared.Attempts != 0 || cleared.NextAttemptAt.Valid || cleared.ID != row.ID {
		t.Fatalf("row = %+v, want the same row without marker, error and retry", cleared)
	}
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("the next event = %s, %v, want created", out, err)
	}
	if n := len(e.tracker.writes()); n != 2 {
		t.Fatalf("creates = %d, want the failed one and exactly one that succeeded", n)
	}
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeExists || len(e.tracker.writes()) != 2 {
		t.Fatalf("a further event = %s, %v, want exists and no second ticket", out, err)
	}
	if final, _ := e.row(t, id); final.ID != row.ID || final.ExternalID.String != "task-1" || final.PendingSince.Valid {
		t.Fatalf("row = %+v, want it linked to the one ticket", final)
	}
}

func TestClearingAMarkerRefusesARowThatHasItsTicketOrNoMarker(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	row, _ := e.row(t, id)
	tickets := e.st.UpdateTickets()

	if err := tickets.ClearMarker(context.Background(), row.ID, e.clock()); !errors.Is(err, store.ErrNoPendingMarker) {
		t.Fatalf("ClearMarker of a linked ticket = %v, want ErrNoPendingMarker", err)
	}
	if err := tickets.ClearMarker(context.Background(), "missing", e.clock()); !errors.Is(err, store.ErrUpdateTicketNotFound) {
		t.Fatalf("ClearMarker of an unknown row = %v, want ErrUpdateTicketNotFound", err)
	}
	if after, _ := e.row(t, id); after.ExternalID.String != "task-1" || after.State != "open" {
		t.Fatalf("row = %+v, want the linked ticket untouched", after)
	}
}

func TestAFailedLinkWriteShowsTheCreatedTicketInTheProblemsView(t *testing.T) {
	e := newEnv(t)
	e.engine.Tickets = &failingTickets{UpdateTicketRepository: e.st.UpdateTickets(), link: errors.New("database is locked")}
	id := e.addPR(t, pr{Deps: foo})

	if out, err := process(t, e, id); err == nil || out != updatetickets.OutcomeFailed {
		t.Fatalf("Process = %s, %v", out, err)
	}
	row, _ := e.row(t, id)
	if !strings.Contains(row.LastError.String, "task-1 was created but its link was not stored") || !row.PendingSince.Valid || row.Attempts != 0 {
		t.Fatalf("row = %+v, want the created ticket named in last_error, the marker kept and no attempt counted", row)
	}
}

func TestTheErrorOfAFailedCreateIsDroppedOnceItsPRWillNotBeTicketed(t *testing.T) {
	cases := map[string]struct {
		change string
		want   updatetickets.Outcome
	}{
		"filtered out":      {`UPDATE ticket_projects SET filter = '{"updateTypes":[],"labels":["wanted"],"skipAutomerged":true}'`, updatetickets.OutcomeFiltered},
		"closed":            {`UPDATE update_prs SET state = 'closed'`, updatetickets.OutcomeNotOpen},
		"no ticket project": {`UPDATE installation_settings SET default_ticket_project_id = NULL`, updatetickets.OutcomeNoTicketProject},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.tracker.setStatus(func(int) int { return http.StatusBadRequest })
			id := e.addPR(t, pr{Deps: foo})
			if _, err := process(t, e, id); err == nil {
				t.Fatal("want the 400 recorded")
			}
			if row, _ := e.row(t, id); !row.LastError.Valid {
				t.Fatalf("row = %+v, want the error recorded", row)
			}

			e.exec(t, tc.change)
			e.advance(time.Hour)
			if out, err := process(t, e, id); err != nil || out != tc.want {
				t.Fatalf("Process = %s, %v, want %s", out, err, tc.want)
			}
			row, _ := e.row(t, id)
			if row.LastError.Valid || row.Attempts != 0 || row.NextAttemptAt.Valid || row.PendingSince.Valid {
				t.Fatalf("row = %+v, want the stale error and retry dropped", row)
			}
		})
	}
}

func TestAMarkerKeepsItsErrorWhenItsPRIsClosed(t *testing.T) {
	e := newEnv(t)
	e.tracker.setStatus(func(int) int { return http.StatusInternalServerError })
	id := e.addPR(t, pr{Deps: foo})
	_, _ = process(t, e, id)
	e.setState(t, id, "closed")

	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeNotOpen {
		t.Fatalf("Process = %s, %v", out, err)
	}
	if row, _ := e.row(t, id); !row.PendingSince.Valid || !row.LastError.Valid {
		t.Fatalf("row = %+v, want the marker and its error kept: a ticket may exist that nobody linked", row)
	}
}

// The superseded event the PR tracker publishes after its transaction commits makes the engine
// look at the replaced PR's ticket at once, not at the next pass.
func TestASupersededEventSupersedesTheReplacedPRsTicket(t *testing.T) {
	e := newEnv(t)
	e.useMapping(t, distinctMapping)
	old := e.ticketed(t, pr{Number: 21})
	fresh := e.ticketed(t, pr{Number: 22, Deps: foo})
	e.engine.SweepInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	time.Sleep(100 * time.Millisecond)

	e.replace(t, old, fresh)
	e.engine.Handle([]updateprs.Event{{Kind: updateprs.EventSuperseded, UpdatePRID: old, Number: 21, ByUpdatePRID: fresh, ByNumber: 22}})
	eventually(t, "the event to supersede the ticket", func() bool {
		row, _ := e.row(t, old)
		return row.State == "superseded"
	})
}
