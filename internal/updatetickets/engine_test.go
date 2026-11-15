package updatetickets_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

func process(t *testing.T, e *env, prID string) (updatetickets.Outcome, error) {
	t.Helper()
	return e.engine.Process(context.Background(), prID)
}

func TestAPassingUpdatePRGetsExactlyOneTicketWithItsKeyAndLink(t *testing.T) {
	e := newEnv(t)
	id := e.addPR(t, pr{Deps: []dep{{"foo", "1.4.0", "2.0.0", "major"}, {"bar", "abc", "def", ""}}, Notes: "Changelog body"})

	out, err := process(t, e, id)
	if err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process = %s, %v, want created", out, err)
	}
	calls := e.tracker.calls()
	if len(calls) != 1 || calls[0].Path != "/api/task/DEV" {
		t.Fatalf("tracker calls = %+v, want one create in DEV", calls)
	}
	row, ok := e.row(t, id)
	if !ok {
		t.Fatal("no update_tickets row")
	}
	if calls[0].Title != "Update dependency foo to v2 (acme/repo-1)" {
		t.Errorf("title = %q", calls[0].Title)
	}
	for _, want := range []string{"- foo: 1.4.0 → 2.0.0 (major)", "- bar: abc → def\n", "Changelog body", "Release Ops key: " + row.ID} {
		if !strings.Contains(calls[0].Description, want) {
			t.Errorf("description lacks %q:\n%s", want, calls[0].Description)
		}
	}
	if !strings.HasSuffix(calls[0].Description, "\n\nRelease Ops key: "+row.ID) {
		t.Errorf("the key line is not appended after the template:\n%s", calls[0].Description)
	}
	if row.State != "open" || row.ExternalID.String != "task-1" || row.PendingSince.Valid || row.ProjectID != e.project {
		t.Errorf("row = %+v, want open, task-1, no marker, project %s", row, e.project)
	}
	if !strings.Contains(row.ExternalURL.String, "/dashboard/workspace/ws-1/project/DEV/task/task-1") || !row.SyncedHash.Valid || row.LastError.Valid {
		t.Errorf("row link = %+v", row)
	}

	out, err = process(t, e, id)
	if err != nil || out != updatetickets.OutcomeExists || len(e.tracker.calls()) != 1 || e.rowCount(t) != 1 {
		t.Fatalf("a second Process = %s, %v with %d calls and %d rows, want exists and no second ticket", out, err, len(e.tracker.calls()), e.rowCount(t))
	}
}

func TestAGroupedPRIsOneTicketThatListsEveryDependency(t *testing.T) {
	e := newEnv(t)
	id := e.addPR(t, pr{Deps: []dep{{"lodash", "4.17.20", "4.17.21", "patch"}, {"react", "17.0.2", "18.2.0", "major"}, {"vue", "2", "3", "major"}}})
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process = %s, %v", out, err)
	}
	calls := e.tracker.calls()
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want one ticket for the group", len(calls))
	}
	for _, want := range []string{"- lodash: 4.17.20 → 4.17.21 (patch)", "- react: 17.0.2 → 18.2.0 (major)", "- vue: 2 → 3 (major)"} {
		if !strings.Contains(calls[0].Description, want) {
			t.Errorf("description lacks %q:\n%s", want, calls[0].Description)
		}
	}
}

func TestATrackerFailureAfterTheRequestWasSentLeavesTheMarkerAndANewAttemptCreatesNoDuplicate(t *testing.T) {
	e := newEnv(t)
	e.tracker.setStatus(func(int) int { return http.StatusInternalServerError })
	id := e.addPR(t, pr{Deps: foo})

	out, err := process(t, e, id)
	if err == nil || out != updatetickets.OutcomeFailed {
		t.Fatalf("Process = %s, %v, want a failure", out, err)
	}
	row, _ := e.row(t, id)
	if !row.PendingSince.Valid || row.ExternalID.Valid || !strings.Contains(row.LastError.String, "500") || row.Attempts != 1 {
		t.Fatalf("row = %+v, want the marker kept, no ticket, the 500 recorded and one attempt", row)
	}

	// The tracker may have stored the ticket; even a healthy tracker is not asked again.
	e.tracker.setStatus(func(int) int { return http.StatusOK })
	e.advance(time.Hour)
	for range 2 {
		out, err = process(t, e, id)
		if !errors.Is(err, updatetickets.ErrUnconfirmed) || out != updatetickets.OutcomeUnconfirmed {
			t.Fatalf("retry = %s, %v, want a refusal", out, err)
		}
	}
	if n := len(e.tracker.calls()); n != 1 {
		t.Fatalf("tracker calls = %d, want the one failed create and no duplicate", n)
	}
	row, _ = e.row(t, id)
	if !row.PendingSince.Valid || !strings.Contains(row.LastError.String, "may already exist") || row.Attempts != 1 {
		t.Fatalf("row = %+v, want the marker kept and the refusal recorded without a new attempt", row)
	}
	if e.rowCount(t) != 1 {
		t.Fatalf("rows = %d, want 1", e.rowCount(t))
	}
}

func TestAFailureThatProvesNothingWasStoredClearsTheMarkerAndTheRetryReusesTheRow(t *testing.T) {
	e := newEnv(t)
	e.tracker.setStatus(func(n int) int {
		if n == 1 {
			return http.StatusBadRequest
		}
		return http.StatusOK
	})
	id := e.addPR(t, pr{Deps: foo})

	if out, err := process(t, e, id); err == nil || out != updatetickets.OutcomeFailed {
		t.Fatalf("Process = %s, %v, want a failure", out, err)
	}
	first, _ := e.row(t, id)
	if first.PendingSince.Valid || !first.NextAttemptAt.Valid || first.Attempts != 1 || !strings.Contains(first.LastError.String, "400") {
		t.Fatalf("row = %+v, want the marker cleared, a retry scheduled and the 400 recorded", first)
	}

	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeWaiting || len(e.tracker.calls()) != 1 {
		t.Fatalf("an early retry = %s, %v with %d calls, want it to wait", out, err, len(e.tracker.calls()))
	}
	e.advance(time.Hour)
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("the due retry = %s, %v, want created", out, err)
	}
	row, _ := e.row(t, id)
	if row.ID != first.ID || e.rowCount(t) != 1 || row.ExternalID.String != "task-2" || row.PendingSince.Valid || row.LastError.Valid || row.Attempts != 0 || row.NextAttemptAt.Valid {
		t.Fatalf("row = %+v (first %+v), want the same row linked and its failure state cleared", row, first)
	}
}

// failingTickets makes selected writes of the repository fail.
type failingTickets struct {
	store.UpdateTicketRepository
	begin        error
	link         error
	linkAttempts int
}

func (f *failingTickets) BeginCreate(ctx context.Context, in store.BeginCreateInput) (string, error) {
	if f.begin != nil {
		return "", f.begin
	}
	return f.UpdateTicketRepository.BeginCreate(ctx, in)
}

func (f *failingTickets) LinkCreated(ctx context.Context, in store.LinkCreatedInput) error {
	f.linkAttempts++
	if f.link != nil {
		return f.link
	}
	return f.UpdateTicketRepository.LinkCreated(ctx, in)
}

func TestAFailedMarkerWriteMeansTheTrackerIsNotCalled(t *testing.T) {
	e := newEnv(t)
	e.engine.Tickets = &failingTickets{UpdateTicketRepository: e.st.UpdateTickets(), begin: errors.New("disk full")}
	id := e.addPR(t, pr{Deps: foo})

	out, err := process(t, e, id)
	if err == nil || out != updatetickets.OutcomeFailed || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Process = %s, %v, want the marker write's error", out, err)
	}
	if n := len(e.tracker.calls()); n != 0 {
		t.Fatalf("tracker calls = %d, want none without a marker", n)
	}
	if e.rowCount(t) != 0 {
		t.Fatalf("rows = %d, want none", e.rowCount(t))
	}
}

func TestAFailedLinkWriteIsRetriedAndTheMarkerKeepsGuardingThePR(t *testing.T) {
	e := newEnv(t)
	flaky := &failingTickets{UpdateTicketRepository: e.st.UpdateTickets(), link: errors.New("database is locked")}
	e.engine.Tickets = flaky
	id := e.addPR(t, pr{Deps: foo})

	out, err := process(t, e, id)
	if err == nil || out != updatetickets.OutcomeFailed || !strings.Contains(err.Error(), "task-1 was created but its link was not stored") {
		t.Fatalf("Process = %s, %v, want the created ticket named", out, err)
	}
	if flaky.linkAttempts != 3 {
		t.Fatalf("link writes = %d, want the v1 bound of 3", flaky.linkAttempts)
	}
	row, _ := e.row(t, id)
	if !row.PendingSince.Valid || row.ExternalID.Valid {
		t.Fatalf("row = %+v, want the marker kept and no link", row)
	}

	flaky.link = nil
	for range 2 {
		if out, err := process(t, e, id); !errors.Is(err, updatetickets.ErrUnconfirmed) || out != updatetickets.OutcomeUnconfirmed {
			t.Fatalf("a later Process = %s, %v, want a refusal", out, err)
		}
	}
	if n := len(e.tracker.calls()); n != 1 {
		t.Fatalf("tracker calls = %d, want the one create", n)
	}
}

func TestALinkWriteThatSucceedsOnARetryStoresTheTicket(t *testing.T) {
	e := newEnv(t)
	e.engine.Tickets = &flakyLink{UpdateTicketRepository: e.st.UpdateTickets(), failFirst: 2}
	id := e.addPR(t, pr{Deps: foo})
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process = %s, %v, want created after two failed link writes", out, err)
	}
	if row, _ := e.row(t, id); row.ExternalID.String != "task-1" || row.PendingSince.Valid {
		t.Fatalf("row = %+v", row)
	}
}

type flakyLink struct {
	store.UpdateTicketRepository
	failFirst, calls int
}

func (f *flakyLink) LinkCreated(ctx context.Context, in store.LinkCreatedInput) error {
	f.calls++
	if f.calls <= f.failFirst {
		return errors.New("database is locked")
	}
	return f.UpdateTicketRepository.LinkCreated(ctx, in)
}

func TestAConcurrentPairOfProcessesCreatesOneTicket(t *testing.T) {
	e := newEnv(t)
	id := e.addPR(t, pr{Deps: foo})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { _, _ = e.engine.Process(context.Background(), id) })
	}
	wg.Wait()
	if n := len(e.tracker.calls()); n != 1 {
		t.Fatalf("tracker calls = %d, want one", n)
	}
}

func TestTheTicketProjectIsTheRepositoryOverrideElseTheInstallationDefault(t *testing.T) {
	e := newEnv(t)
	other := e.addProject(t, "OPS", store.CreateTicketProjectInput{})
	a := e.addPR(t, pr{Number: 1, Deps: foo})
	b := e.addPR(t, pr{Number: 2, Repo: "repo-2", Deps: foo})
	if _, err := e.st.Repositories().Update(context.Background(), "repo-2", store.RepositoryChange{SetTicketProject: true, TicketProjectID: other}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
			t.Fatalf("Process(%s) = %s, %v", id, out, err)
		}
	}
	paths := map[string]bool{}
	for _, c := range e.tracker.calls() {
		paths[c.Path] = true
	}
	if !paths["/api/task/DEV"] || !paths["/api/task/OPS"] {
		t.Fatalf("created in %v, want the default DEV for repo-1 and the override OPS for repo-2", paths)
	}
	if row, _ := e.row(t, b); row.ProjectID != other {
		t.Fatalf("repo-2's ticket is in project %s, want the override %s", row.ProjectID, other)
	}
}

func TestARepositoryWithNoTicketProjectGetsNoTicketAndNoError(t *testing.T) {
	e := newEnv(t)
	e.exec(t, `DELETE FROM installation_settings`)
	id := e.addPR(t, pr{Deps: foo})

	for range 2 {
		if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeNoTicketProject {
			t.Fatalf("Process = %s, %v, want no_ticket_project without an error", out, err)
		}
	}
	if len(e.tracker.calls()) != 0 || e.rowCount(t) != 0 {
		t.Fatalf("calls = %d, rows = %d, want neither", len(e.tracker.calls()), e.rowCount(t))
	}
	if ids, err := e.st.UpdateTickets().PendingPRs(context.Background(), e.clock()); err != nil || len(ids) != 1 {
		t.Fatalf("PendingPRs = %v, %v, want the PR looked at again once a project is set", ids, err)
	}

	if err := e.st.InstallationSettings().SetDefaultTicketProject(context.Background(), "inst-1", e.project); err != nil {
		t.Fatal(err)
	}
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process after setting a default = %s, %v", out, err)
	}
}

func TestAFilteredOutPRCreatesNoTicket(t *testing.T) {
	e := newEnv(t)
	majorOnly := e.addProject(t, "MAJ", store.CreateTicketProjectInput{Filter: `{"updateTypes":["major"],"labels":[],"skipAutomerged":true}`})
	e.exec(t, `UPDATE repositories SET ticket_project_id = ? WHERE id = 'repo-1'`, majorOnly)

	patch := e.addPR(t, pr{Number: 1, Deps: []dep{{"a", "1", "2", "patch"}}})
	automerged := e.addPR(t, pr{Number: 2, Automerge: boolp(true), Deps: foo})
	major := e.addPR(t, pr{Number: 3, Deps: foo})
	for _, id := range []string{patch, automerged} {
		if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeFiltered {
			t.Fatalf("Process(%s) = %s, %v, want filtered", id, out, err)
		}
	}
	if len(e.tracker.calls()) != 0 || e.rowCount(t) != 0 {
		t.Fatalf("calls = %d, rows = %d, want neither for filtered PRs", len(e.tracker.calls()), e.rowCount(t))
	}
	if out, err := process(t, e, major); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("Process(major) = %s, %v", out, err)
	}
}

func TestTheDefaultFilterTicketsEveryNonAutomergedPR(t *testing.T) {
	e := newEnv(t)
	for i, d := range []dep{{"a", "1", "2", "patch"}, {"b", "1", "2", "digest"}, {"c", "1", "2", "other"}, {"d", "1", "2", ""}} {
		id := e.addPR(t, pr{Number: int64(i + 1), Deps: []dep{d}, Automerge: boolp(false)})
		if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
			t.Fatalf("Process(%s) = %s, %v, want created", d.Name, out, err)
		}
	}
	unknown := e.addPR(t, pr{Number: 9, Deps: foo})
	if out, err := process(t, e, unknown); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("a PR with an unknown automerge = %s, %v, want created", out, err)
	}
	auto := e.addPR(t, pr{Number: 10, Deps: foo, Automerge: boolp(true)})
	if out, err := process(t, e, auto); err != nil || out != updatetickets.OutcomeFiltered {
		t.Fatalf("an automerged PR = %s, %v, want filtered by default", out, err)
	}
}

func TestOnlyOpenPRsWithDependenciesGetATicket(t *testing.T) {
	e := newEnv(t)
	merged := e.addPR(t, pr{Number: 1, State: "merged", Deps: foo})
	closed := e.addPR(t, pr{Number: 2, State: "closed", Deps: foo})
	none := e.addPR(t, pr{Number: 3})
	for id, want := range map[string]updatetickets.Outcome{merged: updatetickets.OutcomeNotOpen, closed: updatetickets.OutcomeNotOpen, none: updatetickets.OutcomeNoDependencies} {
		if out, err := process(t, e, id); err != nil || out != want {
			t.Fatalf("Process(%s) = %s, %v, want %s", id, out, err, want)
		}
	}
	if out, err := process(t, e, "no-such-pr"); err != nil || out != updatetickets.OutcomeNotFound {
		t.Fatalf("Process(unknown) = %s, %v", out, err)
	}
	if len(e.tracker.calls()) != 0 || e.rowCount(t) != 0 {
		t.Fatalf("calls = %d, rows = %d, want neither", len(e.tracker.calls()), e.rowCount(t))
	}
}

func TestAPRWhoseRepositoryLeftTheInstallationGetsNoTicket(t *testing.T) {
	e := newEnv(t)
	id := e.addPR(t, pr{Deps: foo})
	e.exec(t, `UPDATE repositories SET removed_at = '2026-11-15T10:00:00.000Z' WHERE id = 'repo-1'`)
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeNotFound || len(e.tracker.calls()) != 0 {
		t.Fatalf("Process = %s, %v with %d calls", out, err, len(e.tracker.calls()))
	}
}

func TestATemplateOfTheReleaseContractFailsTheTicketAndIsRecordedWithoutAMarker(t *testing.T) {
	e := newEnv(t)
	e.exec(t, `UPDATE ticket_projects SET content_templates = '{"title":"Release {{ .Release.Tag }}","description":"","updateComment":"","supersedeComment":""}'`)
	id := e.addPR(t, pr{Deps: foo})

	out, err := process(t, e, id)
	if err == nil || out != updatetickets.OutcomeFailed || !strings.Contains(err.Error(), ".Release.Tag") {
		t.Fatalf("Process = %s, %v, want the variable named", out, err)
	}
	row, ok := e.row(t, id)
	if !ok || row.PendingSince.Valid || !strings.Contains(row.LastError.String, ".Release.Tag") || len(e.tracker.calls()) != 0 {
		t.Fatalf("row = %+v (%v) with %d calls, want the error recorded, no marker and no call", row, ok, len(e.tracker.calls()))
	}

	e.exec(t, `UPDATE ticket_projects SET content_templates = '{"title":"","description":"","updateComment":"","supersedeComment":""}'`)
	e.advance(time.Hour)
	if out, err := process(t, e, id); err != nil || out != updatetickets.OutcomeCreated {
		t.Fatalf("after the admin fixed the template = %s, %v", out, err)
	}
}

func TestAnErrorMessageIsOneBoundedLine(t *testing.T) {
	e := newEnv(t)
	e.tracker.setStatus(func(int) int { return http.StatusBadGateway })
	id := e.addPR(t, pr{Deps: foo})
	_, _ = process(t, e, id)
	row, _ := e.row(t, id)
	if strings.ContainsAny(row.LastError.String, "\r\n") || len([]rune(row.LastError.String)) > 512 {
		t.Fatalf("last_error = %q, want one line of at most 512 characters", row.LastError.String)
	}
	if strings.Contains(row.LastError.String, "test-key") {
		t.Fatalf("last_error carries the credential: %q", row.LastError.String)
	}
}
