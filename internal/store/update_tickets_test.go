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

var ticketEpoch = time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)

type ticketEnv struct {
	st      *store.Store
	db      *store.DB
	repos   []string
	project string
}

func newTicketEnv(t *testing.T) *ticketEnv {
	t.Helper()
	st, _ := testStore(t)
	e := &ticketEnv{st: st, db: st.DB(), repos: storetest.SeedRepositories(t, st.DB(), 2)}
	e.project = e.newProject(t, "DEV")
	return e
}

func (e *ticketEnv) newProject(t *testing.T, key string) string {
	t.Helper()
	base := "https://kaneo.example.com"
	integ, err := e.st.Integrations().Create(context.Background(), store.CreateIntegrationInput{Kind: "kaneo", Name: key, BaseURL: &base, Secret: []byte(`{"api_key":"k"}`)})
	if err != nil {
		t.Fatal(err)
	}
	tp, err := e.st.TicketProjects().Create(context.Background(), store.CreateTicketProjectInput{IntegrationID: integ.ID, ExternalProjectID: key, Name: key, CreateConfig: `{}`, StatusMapping: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	return tp.ID
}

func (e *ticketEnv) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Writer.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func (e *ticketEnv) addPR(t *testing.T, id, repo string, number int, state string, deps int) {
	t.Helper()
	const at = "2026-11-15T10:00:00.000Z"
	e.exec(t, `INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, labels, automerge, release_notes, last_seen_at, created_at, updated_at)
		VALUES (?, ?, ?, 'n', 'https://github.com/acme/x/pull/1', 'Update foo', 'renovate/foo', ?, '["dependencies","security"]', 1, 'notes', ?, ?, ?)`, id, repo, number, state, at, at, at)
	for i := range deps {
		e.exec(t, `INSERT INTO update_pr_dependencies (id, update_pr_id, name, from_version, to_version, update_type, source) VALUES (?, ?, ?, '1', '2', 'minor', 'report')`,
			id+string(rune('a'+i)), id, "dep"+string(rune('a'+i)))
	}
}

func TestCandidateReadsThePRItsDependenciesAndTheResolvedTicketProject(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	other := e.newProject(t, "OPS")
	e.addPR(t, "pr-1", e.repos[0], 1, "open", 2)
	e.addPR(t, "pr-2", e.repos[1], 2, "open", 0)

	c, err := e.st.UpdateTickets().Candidate(ctx, "pr-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.TicketProjectID != "" || c.Ticket != nil || c.FullName != "acme/repo-1" || c.Number != 1 || c.State != "open" || c.ReleaseNotes != "notes" ||
		!reflect.DeepEqual(c.Labels, []string{"dependencies", "security"}) || c.Automerge == nil || !*c.Automerge || len(c.Dependencies) != 2 {
		t.Fatalf("candidate = %+v", c)
	}

	if err := e.st.InstallationSettings().SetDefaultTicketProject(ctx, "inst-1", e.project); err != nil {
		t.Fatal(err)
	}
	if c, _ = e.st.UpdateTickets().Candidate(ctx, "pr-1"); c.TicketProjectID != e.project {
		t.Fatalf("project = %q, want the installation default %q", c.TicketProjectID, e.project)
	}
	if _, err := e.st.Repositories().Update(ctx, e.repos[0], store.RepositoryChange{SetTicketProject: true, TicketProjectID: other}); err != nil {
		t.Fatal(err)
	}
	if c, _ = e.st.UpdateTickets().Candidate(ctx, "pr-1"); c.TicketProjectID != other {
		t.Fatalf("project = %q, want the repository override %q", c.TicketProjectID, other)
	}
	if c, _ = e.st.UpdateTickets().Candidate(ctx, "pr-2"); c.TicketProjectID != e.project || len(c.Dependencies) != 0 {
		t.Fatalf("pr-2 = %+v, want the default and no dependencies", c)
	}

	e.exec(t, `UPDATE repositories SET removed_at = '2026-11-15T11:00:00.000Z' WHERE id = ?`, e.repos[1])
	if _, err := e.st.UpdateTickets().Candidate(ctx, "pr-2"); !errors.Is(err, store.ErrUpdatePRNotFound) {
		t.Fatalf("Candidate of a removed repository's PR = %v, want ErrUpdatePRNotFound", err)
	}
	if _, err := e.st.UpdateTickets().Candidate(ctx, "nope"); !errors.Is(err, store.ErrUpdatePRNotFound) {
		t.Fatalf("Candidate of an unknown PR = %v", err)
	}
}

// A candidate is read from one snapshot: while a writer flips a PR between two revisions (open
// with one dependency, closed with three) in single transactions, no candidate mixes the two.
func TestCandidateIsReadFromOneSnapshot(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	e.addPR(t, "pr-1", e.repos[0], 1, "open", 1)
	revision := func(tx *sql.Tx, state string, deps int) error {
		if _, err := tx.Exec(`UPDATE update_prs SET state = ? WHERE id = 'pr-1'`, state); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM update_pr_dependencies WHERE update_pr_id = 'pr-1'`); err != nil {
			return err
		}
		for i := range deps {
			if _, err := tx.Exec(`INSERT INTO update_pr_dependencies (id, update_pr_id, name, source) VALUES (?, 'pr-1', ?, 'report')`,
				"d"+string(rune('a'+i)), "dep"+string(rune('a'+i))); err != nil {
				return err
			}
		}
		return nil
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				done <- nil
				return
			default:
			}
			tx, err := e.db.Writer.BeginTx(ctx, nil)
			if err != nil {
				done <- err
				return
			}
			state, deps := "open", 1
			if i%2 == 1 {
				state, deps = "closed", 3
			}
			if err := revision(tx, state, deps); err != nil {
				_ = tx.Rollback()
				done <- err
				return
			}
			if err := tx.Commit(); err != nil {
				done <- err
				return
			}
		}
	}()
	var failure string
	for i := 0; i < 500 && failure == ""; i++ {
		c, err := e.st.UpdateTickets().Candidate(ctx, "pr-1")
		if err != nil {
			failure = err.Error()
		} else if (c.State == "open") != (len(c.Dependencies) == 1) {
			failure = "candidate mixes revisions: state " + c.State + " with " + string(rune('0'+len(c.Dependencies))) + " dependencies"
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if failure != "" {
		t.Fatal(failure)
	}
}

func TestBeginCreateWritesTheMarkerOnceAndRefusesARepeat(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	e.addPR(t, "pr-1", e.repos[0], 1, "open", 1)
	in := store.BeginCreateInput{UpdatePRID: "pr-1", TicketProjectID: e.project, Now: ticketEpoch}

	id, err := tickets.BeginCreate(ctx, in)
	if err != nil || id == "" {
		t.Fatalf("BeginCreate = %q, %v", id, err)
	}
	var state string
	var marker sql.NullString
	if err := e.db.Reader.QueryRow(`SELECT state, pending_since FROM update_tickets WHERE id = ?`, id).Scan(&state, &marker); err != nil || state != "pending" || marker.String != "2026-11-15T12:00:00.000Z" {
		t.Fatalf("row = %q %v (%v), want pending with the marker set", state, marker, err)
	}
	c, _ := tickets.Candidate(ctx, "pr-1")
	if c.Ticket == nil || c.Ticket.ID != id || !c.Ticket.Unconfirmed || c.Ticket.Created {
		t.Fatalf("ticket = %+v", c.Ticket)
	}

	again, err := tickets.BeginCreate(ctx, in)
	if !errors.Is(err, store.ErrCreateUnconfirmed) || again != id {
		t.Fatalf("a repeated BeginCreate = %q, %v, want ErrCreateUnconfirmed with the row id", again, err)
	}
	var rows int
	_ = e.db.Reader.QueryRow(`SELECT COUNT(*) FROM update_tickets`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}

	if err := tickets.LinkCreated(ctx, store.LinkCreatedInput{ID: id, ExternalID: "DEV-1", ExternalURL: "https://x/DEV-1", SyncedHash: "h", Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	var ext, url, hash, pending sql.NullString
	if err := e.db.Reader.QueryRow(`SELECT state, external_id, external_url, synced_hash, pending_since FROM update_tickets WHERE id = ?`, id).Scan(&state, &ext, &url, &hash, &pending); err != nil {
		t.Fatal(err)
	}
	if state != "open" || ext.String != "DEV-1" || url.String != "https://x/DEV-1" || hash.String != "h" || pending.Valid {
		t.Fatalf("row = %s %v %v %v %v, want the link stored, open, the marker cleared", state, ext, url, hash, pending)
	}
	if _, err := tickets.BeginCreate(ctx, in); !errors.Is(err, store.ErrTicketExists) {
		t.Fatalf("BeginCreate after the link = %v, want ErrTicketExists", err)
	}
	if err := tickets.LinkCreated(ctx, store.LinkCreatedInput{ID: id, ExternalID: "DEV-2", Now: ticketEpoch}); !errors.Is(err, store.ErrTicketExists) {
		t.Fatalf("a second link = %v, want ErrTicketExists, so a ticket is never replaced", err)
	}
}

func TestFailCreateClearsTheMarkerOnlyWhenAskedAndAFailureBeforeTheMarkerInsertsTheRow(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	e.addPR(t, "pr-1", e.repos[0], 1, "open", 1)
	e.addPR(t, "pr-2", e.repos[0], 2, "open", 1)
	next := ticketEpoch.Add(time.Hour)

	// A failure before any marker: the row is created without one.
	if err := tickets.FailCreate(ctx, store.FailCreateInput{UpdatePRID: "pr-2", TicketProjectID: e.project, Message: "boom", NextAttemptAt: next, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	c, _ := tickets.Candidate(ctx, "pr-2")
	if c.Ticket == nil || c.Ticket.Unconfirmed || c.Ticket.Attempts != 1 || !c.Ticket.NextAttemptAt.Equal(next) {
		t.Fatalf("ticket = %+v, want a row with no marker, one attempt and the retry time", c.Ticket)
	}

	id, err := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "pr-1", TicketProjectID: e.project, Now: ticketEpoch})
	if err != nil {
		t.Fatal(err)
	}
	if err := tickets.FailCreate(ctx, store.FailCreateInput{UpdatePRID: "pr-1", TicketProjectID: e.project, Message: "500", ClearMarker: false, NextAttemptAt: next, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	if c, _ = tickets.Candidate(ctx, "pr-1"); !c.Ticket.Unconfirmed || c.Ticket.Attempts != 1 {
		t.Fatalf("ticket = %+v, want the marker kept", c.Ticket)
	}
	if err := tickets.FailCreate(ctx, store.FailCreateInput{UpdatePRID: "pr-1", TicketProjectID: e.project, Message: "400", ClearMarker: true, NextAttemptAt: next, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	if c, _ = tickets.Candidate(ctx, "pr-1"); c.Ticket.Unconfirmed || c.Ticket.Attempts != 2 || c.Ticket.ID != id {
		t.Fatalf("ticket = %+v, want the same row with the marker cleared", c.Ticket)
	}

	// The cleared row is armed again by the next attempt, in the project resolved then.
	other := e.newProject(t, "OPS")
	if again, err := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "pr-1", TicketProjectID: other, Now: ticketEpoch}); err != nil || again != id {
		t.Fatalf("BeginCreate after a cleared marker = %q, %v, want the same row %q", again, err, id)
	}
	var project string
	_ = e.db.Reader.QueryRow(`SELECT ticket_project_id FROM update_tickets WHERE id = ?`, id).Scan(&project)
	if project != other {
		t.Fatalf("project = %s, want the one resolved for the new attempt %s", project, other)
	}

	// A failure never touches a row that has its ticket.
	if err := tickets.LinkCreated(ctx, store.LinkCreatedInput{ID: id, ExternalID: "DEV-1", Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := tickets.FailCreate(ctx, store.FailCreateInput{UpdatePRID: "pr-1", Message: "late", ClearMarker: true, NextAttemptAt: next, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	var errText sql.NullString
	var attempts int
	_ = e.db.Reader.QueryRow(`SELECT last_error, attempts FROM update_tickets WHERE id = ?`, id).Scan(&errText, &attempts)
	if errText.Valid || attempts != 0 {
		t.Fatalf("a failure was recorded on a linked row: %v %d", errText, attempts)
	}
}

func TestNoteErrorRecordsTheMessageOnceWithoutCountingAnAttempt(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	e.addPR(t, "pr-1", e.repos[0], 1, "open", 1)
	id, _ := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "pr-1", TicketProjectID: e.project, Now: ticketEpoch})

	for _, at := range []time.Time{ticketEpoch, ticketEpoch.Add(time.Hour)} {
		if err := tickets.NoteError(ctx, id, "unconfirmed", at); err != nil {
			t.Fatal(err)
		}
	}
	var msg, errAt sql.NullString
	var attempts int
	_ = e.db.Reader.QueryRow(`SELECT last_error, last_error_at, attempts FROM update_tickets WHERE id = ?`, id).Scan(&msg, &errAt, &attempts)
	if msg.String != "unconfirmed" || errAt.String != "2026-11-15T12:00:00.000Z" || attempts != 0 {
		t.Fatalf("row = %v %v %d, want the message stamped once and no attempt counted", msg, errAt, attempts)
	}
}

func TestPendingPRsListsOpenPRsWithDependenciesAndNoTicketMarkerOrWaitingRetry(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	e.addPR(t, "ready", e.repos[0], 1, "open", 1)
	e.addPR(t, "no-deps", e.repos[0], 2, "open", 0)
	e.addPR(t, "merged", e.repos[0], 3, "merged", 1)
	e.addPR(t, "marked", e.repos[0], 4, "open", 1)
	e.addPR(t, "waiting", e.repos[0], 5, "open", 1)
	e.addPR(t, "linked", e.repos[0], 6, "open", 1)
	e.addPR(t, "removed-repo", e.repos[1], 7, "open", 1)
	e.exec(t, `UPDATE repositories SET removed_at = '2026-11-15T11:00:00.000Z' WHERE id = ?`, e.repos[1])

	if _, err := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "marked", TicketProjectID: e.project, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	if err := tickets.FailCreate(ctx, store.FailCreateInput{UpdatePRID: "waiting", TicketProjectID: e.project, Message: "x", ClearMarker: true, NextAttemptAt: ticketEpoch.Add(time.Hour), Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	id, _ := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "linked", TicketProjectID: e.project, Now: ticketEpoch})
	if err := tickets.LinkCreated(ctx, store.LinkCreatedInput{ID: id, ExternalID: "DEV-1", Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}

	got, err := tickets.PendingPRs(ctx, ticketEpoch)
	if err != nil || !reflect.DeepEqual(got, []string{"ready"}) {
		t.Fatalf("PendingPRs = %v, %v, want only the ready PR", got, err)
	}
	got, _ = tickets.PendingPRs(ctx, ticketEpoch.Add(2*time.Hour))
	if !reflect.DeepEqual(got, []string{"ready", "waiting"}) {
		t.Fatalf("PendingPRs once the retry is due = %v, want the waiting PR too", got)
	}
}

func TestTicketProjectFilterIsStoredDefaultedAndUpdated(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tp, _ := e.st.TicketProjects().Get(ctx, e.project)
	if tp.Filter != `{"updateTypes":[],"labels":[],"skipAutomerged":true}` {
		t.Fatalf("filter = %q, want the default", tp.Filter)
	}
	const f = `{"updateTypes":["major"],"labels":[],"skipAutomerged":false}`
	updated, err := e.st.TicketProjects().Update(ctx, e.project, store.UpdateTicketProjectInput{Name: "DEV", CreateConfig: `{}`, StatusMapping: `{}`, Filter: f})
	if err != nil || updated.Filter != f {
		t.Fatalf("Update = %+v, %v, want the filter stored", updated, err)
	}
	updated, _ = e.st.TicketProjects().Update(ctx, e.project, store.UpdateTicketProjectInput{Name: "DEV", CreateConfig: `{}`, StatusMapping: `{}`})
	if updated.Filter != `{"updateTypes":[],"labels":[],"skipAutomerged":true}` {
		t.Fatalf("filter = %q, want an update without one to store the default", updated.Filter)
	}
}

func TestRepositoryUpdateIsAtomicAndSetsAndClearsTheOverride(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	repos := e.st.Repositories()

	l, err := repos.Update(ctx, e.repos[0], store.RepositoryChange{SetTicketProject: true, TicketProjectID: e.project})
	if err != nil || l.TicketProjectID != e.project || l.EffectiveTicketProjectID != e.project || l.EffectiveTicketProjectName != "DEV" || !l.Enabled {
		t.Fatalf("Update = %+v, %v", l, err)
	}
	disable := false
	if _, err := repos.Update(ctx, e.repos[0], store.RepositoryChange{Enabled: &disable, SetTicketProject: true, TicketProjectID: "no-such"}); !errors.Is(err, store.ErrTicketProjectNotFound) {
		t.Fatalf("Update with an unknown project = %v", err)
	}
	if got, _ := repos.Get(ctx, e.repos[0]); !got.Enabled {
		t.Fatal("a refused change paused the repository")
	}
	l, err = repos.Update(ctx, e.repos[0], store.RepositoryChange{SetTicketProject: true})
	if err != nil || l.TicketProjectID != "" || l.EffectiveTicketProjectID != "" || l.EffectiveTicketProjectName != "" || !l.Enabled {
		t.Fatalf("clearing = %+v, %v, want no override and no default in effect", l, err)
	}
	if _, err := repos.Update(ctx, "no-such-repo", store.RepositoryChange{SetTicketProject: true}); !errors.Is(err, store.ErrRepositoryNotFound) {
		t.Fatalf("Update of an unknown repository = %v", err)
	}
}

func TestInstallationDefaultIsUpsertedAndRefusedForUnknownProjectsAndInstallations(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	settings := e.st.InstallationSettings()

	if err := settings.SetDefaultTicketProject(ctx, "inst-1", e.project); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetDefaultTicketProject(ctx, "inst-1", "no-such"); !errors.Is(err, store.ErrTicketProjectNotFound) {
		t.Fatalf("unknown project = %v", err)
	}
	if err := settings.SetDefaultTicketProject(ctx, "no-such", e.project); !errors.Is(err, store.ErrInstallationNotFound) {
		t.Fatalf("unknown installation = %v", err)
	}
	list, err := settings.List(ctx)
	if err != nil || len(list) != 1 || list[0].DefaultTicketProjectID != e.project || list[0].RepositoryCount != 2 || list[0].AccountLogin != "acme" {
		t.Fatalf("List = %+v, %v, want the default kept after the refusals", list, err)
	}
	if err := settings.SetDefaultTicketProject(ctx, "inst-1", ""); err != nil {
		t.Fatal(err)
	}
	if list, _ = settings.List(ctx); list[0].DefaultTicketProjectID != "" {
		t.Fatalf("default = %q, want it cleared", list[0].DefaultTicketProjectID)
	}
}

// ticketed stores a PR with its linked ticket and returns the ticket's row id.
func (e *ticketEnv) ticketed(t *testing.T, prID, repo string, number int) string {
	t.Helper()
	e.addPR(t, prID, repo, number, "open", 1)
	id, err := e.st.UpdateTickets().BeginCreate(context.Background(), store.BeginCreateInput{UpdatePRID: prID, TicketProjectID: e.project, Now: ticketEpoch})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpdateTickets().LinkCreated(context.Background(), store.LinkCreatedInput{ID: id, ExternalID: "DEV-1", ExternalURL: "https://x/DEV-1", SyncedHash: "h", Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *ticketEnv) commentColumns(t *testing.T, id string) (commentID sql.NullInt64, hash, lastError sql.NullString) {
	t.Helper()
	if err := e.db.Reader.QueryRow(`SELECT pr_comment_id, pr_comment_hash, last_error FROM update_tickets WHERE id = ?`, id).Scan(&commentID, &hash, &lastError); err != nil {
		t.Fatal(err)
	}
	return commentID, hash, lastError
}

// The PR comment is due for a ticket that exists and whose comment was never written, changed
// state or failed since; a ticket without a link, one whose comment is current and one of a removed
// repository are not listed.
func TestCommentablePRsListsTheTicketsWhoseCommentIsDue(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	e.addPR(t, "pr-pending", e.repos[0], 1, "open", 1)
	if _, err := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "pr-pending", TicketProjectID: e.project, Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	e.ticketed(t, "pr-fresh", e.repos[0], 2)
	current := e.ticketed(t, "pr-current", e.repos[0], 3)
	e.ticketed(t, "pr-gone", e.repos[1], 4)
	if err := tickets.StorePRComment(ctx, current, 11, "open", "h1", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE repositories SET removed_at = '2026-11-15T10:00:00.000Z' WHERE id = ?`, e.repos[1])

	ids, err := tickets.CommentablePRs(ctx)
	if err != nil || !reflect.DeepEqual(ids, []string{"pr-fresh"}) {
		t.Fatalf("CommentablePRs = %v, %v, want only pr-fresh", ids, err)
	}

	// A failed write makes the current one due again; a stored one takes it out.
	if err := tickets.NotePRCommentError(ctx, current, "github request failed", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if ids, _ = tickets.CommentablePRs(ctx); !reflect.DeepEqual(ids, []string{"pr-current", "pr-fresh"}) && !reflect.DeepEqual(ids, []string{"pr-fresh", "pr-current"}) {
		t.Fatalf("CommentablePRs after a failed write = %v, want both", ids)
	}
}

// A new state changes the comment, so completing the transition to it makes the comment due; a
// completion that leaves the state as it is keeps the hash.
func TestCompletingATransitionToANewStateMakesTheCommentDue(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	id := e.ticketed(t, "pr-1", e.repos[0], 1)
	if err := tickets.StorePRComment(ctx, id, 11, "open", "h-open", ticketEpoch); err != nil {
		t.Fatal(err)
	}

	if err := tickets.CompleteTransition(ctx, id, "open", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if _, hash, _ := e.commentColumns(t, id); hash.String != "h-open" {
		t.Fatalf("hash = %q after a completion that left the state, want it kept", hash.String)
	}

	if err := tickets.CompleteTransition(ctx, id, "done", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	commentID, hash, _ := e.commentColumns(t, id)
	if hash.Valid || commentID.Int64 != 11 {
		t.Fatalf("comment id %v, hash %v: want the id kept and the hash dropped so the comment is due", commentID, hash)
	}
}

// The hash is kept only for the state the body was written for: a ticket that moved on while the
// comment was being written stays due, with the comment's id stored so it is edited, not posted.
func TestStorePRCommentKeepsTheHashOnlyForTheStateItWasWrittenFor(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	id := e.ticketed(t, "pr-1", e.repos[0], 1)
	if err := tickets.CompleteTransition(ctx, id, "done", ticketEpoch); err != nil {
		t.Fatal(err)
	}

	if err := tickets.StorePRComment(ctx, id, 11, "open", "h-open", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	commentID, hash, _ := e.commentColumns(t, id)
	if hash.Valid || commentID.Int64 != 11 {
		t.Fatalf("comment id %v, hash %v: want the id stored and the stale hash refused", commentID, hash)
	}
	if err := tickets.StorePRComment(ctx, id, 11, "done", "h-done", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if _, hash, _ = e.commentColumns(t, id); hash.String != "h-done" {
		t.Fatalf("hash = %v, want h-done", hash)
	}
	c, _ := tickets.Candidate(context.Background(), "pr-1")
	if c.Ticket.PRCommentID != 11 || c.Ticket.PRCommentHash != "h-done" || c.GitHubInstallationID != 1 {
		t.Fatalf("candidate = %+v, want the comment and the GitHub installation id read", c)
	}
}

// A comment failure is recorded for the Problems view without a retry time or an attempt, the
// error of a ticket operation is neither replaced nor cleared by it, and a row without a ticket
// takes neither write.
func TestPRCommentErrorsAreRecordedApartFromTheTicketsOwn(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	id := e.ticketed(t, "pr-1", e.repos[0], 1)

	if err := tickets.NotePRCommentError(ctx, id, "github request failed", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var next sql.NullString
	_ = e.db.Reader.QueryRow(`SELECT attempts, next_attempt_at FROM update_tickets WHERE id = ?`, id).Scan(&attempts, &next)
	if _, _, msg := e.commentColumns(t, id); msg.String != "PR comment: github request failed" || attempts != 0 || next.Valid {
		t.Fatalf("last_error = %q, attempts %d, next %v, want the prefixed message and no retry held against the ticket", msg.String, attempts, next)
	}
	if err := tickets.StorePRComment(ctx, id, 11, "open", "h", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if _, _, msg := e.commentColumns(t, id); msg.Valid {
		t.Fatalf("last_error = %q after the comment was written, want it cleared", msg.String)
	}

	e.exec(t, `UPDATE update_tickets SET last_error = 'update ticket: boom' WHERE id = ?`, id)
	if err := tickets.NotePRCommentError(ctx, id, "github request failed", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if err := tickets.StorePRComment(ctx, id, 11, "open", "h", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if _, _, msg := e.commentColumns(t, id); msg.String != "update ticket: boom" {
		t.Fatalf("last_error = %q, want the ticket operation's error kept", msg.String)
	}

	e.addPR(t, "pr-2", e.repos[0], 2, "open", 1)
	pendingID, err := tickets.BeginCreate(ctx, store.BeginCreateInput{UpdatePRID: "pr-2", TicketProjectID: e.project, Now: ticketEpoch})
	if err != nil {
		t.Fatal(err)
	}
	if err := tickets.NotePRCommentError(ctx, pendingID, "x", ticketEpoch); !errors.Is(err, store.ErrTicketNotSyncable) {
		t.Fatalf("NotePRCommentError on a row without a ticket = %v, want ErrTicketNotSyncable", err)
	}
	if err := tickets.StorePRComment(ctx, pendingID, 1, "open", "h", ticketEpoch); !errors.Is(err, store.ErrTicketNotSyncable) {
		t.Fatalf("StorePRComment on a row without a ticket = %v, want ErrTicketNotSyncable", err)
	}
}

// A recorded content update is read back with the ticket, survives a failed attempt, and is
// dropped only by MarkSynced; a ticket that was handed off, or has no link, refuses the record.
func TestAContentUpdateIsRecordedUntilTheTicketIsMarkedSynced(t *testing.T) {
	e := newTicketEnv(t)
	ctx := context.Background()
	tickets := e.st.UpdateTickets()
	id := e.ticketed(t, "pr-1", e.repos[0], 1)

	pending := func() string {
		t.Helper()
		c, err := tickets.Candidate(ctx, "pr-1")
		if err != nil || c.Ticket == nil {
			t.Fatalf("Candidate = %v, %v", c.Ticket, err)
		}
		return c.Ticket.PendingContentHash
	}
	if got := pending(); got != "" {
		t.Fatalf("pending content hash = %q on a new ticket, want none", got)
	}

	if err := tickets.BeginContentUpdate(ctx, id, "h-b", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if got := pending(); got != "h-b" {
		t.Fatalf("pending content hash = %q, want h-b", got)
	}
	if err := tickets.FailSync(ctx, store.FailSyncInput{ID: id, Message: "boom", NextAttemptAt: ticketEpoch.Add(time.Hour), Now: ticketEpoch}); err != nil {
		t.Fatal(err)
	}
	if got := pending(); got != "h-b" {
		t.Fatalf("pending content hash = %q after a failed attempt, want it kept", got)
	}
	if err := tickets.MarkSynced(ctx, id, "h-b", ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if got := pending(); got != "" {
		t.Fatalf("pending content hash = %q after MarkSynced, want it dropped", got)
	}

	if err := tickets.HandOff(ctx, id, ticketEpoch); err != nil {
		t.Fatal(err)
	}
	if err := tickets.BeginContentUpdate(ctx, id, "h-c", ticketEpoch); !errors.Is(err, store.ErrTicketNotSyncable) {
		t.Fatalf("BeginContentUpdate on a handed-off ticket = %v, want ErrTicketNotSyncable", err)
	}
}
