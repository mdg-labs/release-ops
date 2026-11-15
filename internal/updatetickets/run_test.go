package updatetickets_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

const botID int64 = 99887766

// renovateBody is the body of a Renovate update PR; rows are the lines of its updates table.
func renovateBody(rows ...string) string {
	table := ""
	if len(rows) > 0 {
		table = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n" + strings.Join(rows, "\n") + "\n\n---\n\n"
	}
	return table + " - [ ] <!-- rebase-check -->rebase\n\n<!--renovate-debug:eyJ4IjoxfQ==-->\n"
}

// openPR applies the PR the way the webhook does: ApplyTx in a transaction, and the events
// published after it commits.
func (e *env) openPR(t *testing.T, tracker *updateprs.Tracker, number int64, body string) {
	t.Helper()
	created, updated := epoch, epoch.Add(time.Minute)
	pr := github.PullRequest{Number: number, NodeID: "PR_n", HTMLURL: "https://github.com/acme/repo-1/pull/" + string(rune('0'+number)), Title: "Update dependency foo to v2", Body: &body,
		State: "open", CreatedAt: &created, UpdatedAt: &updated}
	pr.User.ID = botID
	pr.Head.Ref, pr.Head.SHA = "renovate/foo", "sha"
	tx, err := e.db.Writer.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tracker.ApplyTx(context.Background(), tx, e.repo, pr, botID)
	if err != nil {
		_ = tx.Rollback()
		t.Fatalf("ApplyTx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tracker.Publish(out.Events)
}

// eventually waits, bounded, until cond holds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestADependenciesRecordedEventCreatesTheTicketAndAPRWithoutDependenciesGetsNone(t *testing.T) {
	e := newEnv(t)
	tracker := updateprs.NewTracker(e.st.UpdatePRs(), func() time.Time { return epoch.Add(time.Hour) }, nil)
	tracker.Subscribe(e.engine.Handle)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	// Renovate's config-migration PR and its onboarding PR carry its markers and no updates table.
	e.openPR(t, tracker, 1, renovateBody())
	// A PR whose updates table names a dependency is a normal update PR.
	e.openPR(t, tracker, 2, renovateBody("| [foo](https://foo.dev) | major | `1.4.0` -> `2.0.0` |"))

	eventually(t, "the ticket of PR 2", func() bool { return len(e.tracker.calls()) >= 1 && e.rowCount(t) >= 1 })
	// Give a wrongly created ticket for PR 1 the time to show up.
	time.Sleep(200 * time.Millisecond)

	if calls := e.tracker.calls(); len(calls) != 1 || !strings.Contains(calls[0].Description, "- foo: 1.4.0 → 2.0.0 (major)") {
		t.Fatalf("tracker calls = %+v, want one ticket for the dependency PR", calls)
	}
	var number int
	var external sql.NullString
	if err := e.db.Reader.QueryRow(`SELECT p.number, t.external_id FROM update_tickets t JOIN update_prs p ON p.id = t.update_pr_id`).Scan(&number, &external); err != nil {
		t.Fatalf("update_tickets must hold exactly the dependency PR's row: %v", err)
	}
	if number != 2 || external.String != "task-1" {
		t.Fatalf("ticket row = PR %d %q, want PR 2 linked to task-1", number, external.String)
	}
	if e.rowCount(t) != 1 {
		t.Fatalf("update_tickets rows = %d, want 1 (none for the PR without dependencies)", e.rowCount(t))
	}
}

func TestAPRWhoseEventWasLostIsTicketedByThePeriodicPass(t *testing.T) {
	e := newEnv(t)
	e.addPR(t, pr{Deps: foo})
	e.engine.SweepInterval = 30 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	eventually(t, "the sweep to ticket the PR", func() bool { return len(e.tracker.calls()) == 1 })
	time.Sleep(150 * time.Millisecond)
	if n := len(e.tracker.calls()); n != 1 {
		t.Fatalf("tracker calls = %d, want the sweep to ticket the PR once", n)
	}
}

func TestAWakeStartsAPassOverThePRsWithoutATicket(t *testing.T) {
	e := newEnv(t)
	wake := updateprs.NewNotifier()
	e.engine.Wake = wake
	e.engine.SweepInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(100 * time.Millisecond)
	e.addPR(t, pr{Deps: foo})
	wake.Wake()
	eventually(t, "the wake to ticket the PR", func() bool { return len(e.tracker.calls()) == 1 })
}

func TestAnEventForAPRThatIsNotYetReadyIsLookedAtAgain(t *testing.T) {
	e := newEnv(t)
	tracker := updateprs.NewTracker(e.st.UpdatePRs(), func() time.Time { return epoch.Add(time.Hour) }, nil)
	tracker.Subscribe(e.engine.Handle)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	e.openPR(t, tracker, 3, renovateBody())
	time.Sleep(200 * time.Millisecond)
	if n := len(e.tracker.calls()); n != 0 {
		t.Fatalf("tracker calls = %d, want none while the PR has no dependencies", n)
	}
	// The dependencies are recorded by a later event of the same PR.
	e.openPR(t, tracker, 3, renovateBody("| [foo](https://foo.dev) | major | `1.4.0` -> `2.0.0` |"))
	eventually(t, "the re-evaluation to create the ticket", func() bool { return len(e.tracker.calls()) == 1 })
}
