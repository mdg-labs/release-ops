package updatetickets_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/updateprs"
	"github.com/mdg-labs/release-ops/internal/updatetickets"
)

const prRepo = "acme/repo-1"

type appCreds struct{ pem []byte }

func (c appCreds) SigningKey(context.Context) (int64, []byte, error) {
	return githubtest.AppID, append([]byte(nil), c.pem...), nil
}

type botIdentity struct{}

func (botIdentity) BotIdentity(context.Context) (store.GitHubAppBot, error) {
	return store.GitHubAppBot{Slug: "release-ops-fixture", UserID: botID, Known: true}, nil
}
func (botIdentity) RecordBotUserID(context.Context, int64) (bool, error) { return false, nil }

// gh is the fake GitHub the engine's PR comments go to.
type gh struct {
	fake   *githubtest.Fake
	budget *ratelimit.Budget
}

// withGitHub gives the engine a PR commenter that talks to a fake GitHub, whose installation and
// repository are the ones the store holds.
func (e *env) withGitHub(t *testing.T) *gh {
	t.Helper()
	fake := githubtest.New(t, e.clock)
	fake.AddInstallation(1, githubtest.AppID)
	fake.SetRepositories(1, githubtest.Repo{ID: 1, FullName: prRepo, DefaultBranch: "main"})
	fake.SetCommentAuthor(botID)
	budget := ratelimit.NewBudget(ratelimit.Options{Now: e.clock})
	api := github.NewClient(fake.URL, 5*time.Second).UseRateLimits(budget)
	app := github.NewAppClient(api, appCreds{fake.PEM}, github.AppClientOptions{Now: e.clock, Budget: budget})
	e.engine.Comments = github.NewPRCommenter(api, app, botIdentity{})
	e.engine.SweepInterval = 20 * time.Millisecond
	return &gh{fake: fake, budget: budget}
}

// openPullRequest makes GitHub know the PR the way addPR stored it.
func (g *gh) openPullRequest(number int64) {
	g.fake.SetPullRequest(prRepo, githubtest.PullRequest{Number: number, Title: "Update dependency foo to v2", State: "open", HeadRef: "renovate/foo",
		AuthorID: botID, CreatedAt: epoch, UpdatedAt: epoch})
}

// writes are the requests that change something on GitHub.
func (g *gh) writes() []string {
	var out []string
	for _, r := range g.fake.Requests() {
		if strings.HasPrefix(r.Path, "/repos/") && r.Method != http.MethodGet {
			out = append(out, r.Method+" "+r.Path)
		}
	}
	return out
}

func (g *gh) comments(number int64) []githubtest.Comment { return g.fake.Comments(prRepo, number) }

func (g *gh) assertPullRequestNeverWritten(t *testing.T) {
	t.Helper()
	for _, r := range g.fake.Requests() {
		if strings.Contains(r.Path, "/pulls") && r.Method != http.MethodGet {
			t.Fatalf("%s %s: the PR itself must never be written", r.Method, r.Path)
		}
	}
}

// run starts the engine's loop and stops it when the test ends.
func (e *env) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.engine.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// deps tells the engine that the PR's dependencies were recorded, as the PR tracker does.
func (e *env) deps(id string) {
	e.engine.Handle([]updateprs.Event{{Kind: updateprs.EventDependenciesRecorded, UpdatePRID: id}})
}

type commentRow struct {
	id        int64
	hash      string
	lastError string
	attempts  int
	nextAt    string
}

func (e *env) commentRow(t *testing.T, prID string) commentRow {
	t.Helper()
	var r commentRow
	var id *int64
	var hash, lastErr, next *string
	if err := e.db.Reader.QueryRow(`SELECT pr_comment_id, pr_comment_hash, last_error, attempts, next_attempt_at FROM update_tickets WHERE update_pr_id = ?`, prID).Scan(&id, &hash, &lastErr, &r.attempts, &next); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return commentRow{}
		}
		t.Fatal(err)
	}
	if id != nil {
		r.id = *id
	}
	for dst, src := range map[*string]*string{&r.hash: hash, &r.lastError: lastErr, &r.nextAt: next} {
		if src != nil {
			*dst = *src
		}
	}
	return r
}

func (e *env) waitComment(t *testing.T, g *gh, number int64, contains string) githubtest.Comment {
	t.Helper()
	var got githubtest.Comment
	eventually(t, "the comment to contain "+contains, func() bool {
		for _, c := range g.comments(number) {
			if strings.Contains(c.Body, contains) {
				got = c
				return true
			}
		}
		return false
	})
	return got
}

// A ticketed PR gets exactly one comment by the bot: the marker, the ticket's key and link, and
// its state. The PR itself is never written, and the stored id and hash make the passes that
// follow call nothing.
func TestATicketedPRGetsOneBotCommentWithTheMarkerTheKeyTheLinkAndTheState(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)

	c := e.waitComment(t, g, 21, "Tracked in")
	for _, want := range []string{updatetickets.CommentMarker, "task-1", e.tracker.srv.URL + "/dashboard/workspace/ws-1/project/DEV/task/task-1", "Release Ops ticket state: open"} {
		if !strings.Contains(c.Body, want) {
			t.Fatalf("comment = %q, want it to contain %q", c.Body, want)
		}
	}
	if !strings.HasPrefix(c.Body, updatetickets.CommentMarker) || c.UserID != botID {
		t.Fatalf("comment = %+v, want the marker first and the bot as author", c)
	}
	eventually(t, "the comment id to be stored", func() bool { return e.commentRow(t, id).hash != "" })
	if row := e.commentRow(t, id); row.id != c.ID || row.lastError != "" {
		t.Fatalf("row = %+v, want the comment %d stored without an error", row, c.ID)
	}

	time.Sleep(200 * time.Millisecond)
	if w := g.writes(); len(w) != 1 || !strings.HasPrefix(w[0], "POST ") {
		t.Fatalf("GitHub writes = %v, want exactly one POST", w)
	}
	if n := len(g.comments(21)); n != 1 {
		t.Fatalf("%d comments, want one", n)
	}
	g.assertPullRequestNeverWritten(t)
}

// A change of the ticket edits the same comment, and a restart neither posts again nor edits what
// is unchanged.
func TestATicketChangeEditsTheSameCommentAndARestartPostsNothing(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	e.useMapping(t, distinctMapping)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)
	first := e.waitComment(t, g, 21, "state: open")

	e.setState(t, id, "merged")
	e.deps(id)
	done := e.waitComment(t, g, 21, "state: done")
	if done.ID != first.ID || len(g.comments(21)) != 1 {
		t.Fatalf("comments = %+v, want the first comment %d edited in place", g.comments(21), first.ID)
	}
	eventually(t, "the new hash to be stored", func() bool { return e.commentRow(t, id).hash != "" })
	want := []string{"POST /repos/acme/repo-1/issues/21/comments", "PATCH /repos/acme/repo-1/issues/comments/" + itoa(first.ID)}
	if w := g.writes(); strings.Join(w, ";") != strings.Join(want, ";") {
		t.Fatalf("GitHub writes = %v, want %v", w, want)
	}

	// A restart: a fresh engine on the same database finds the id and the hash and writes nothing.
	restarted := newEngineLike(e)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() { restarted.Run(ctx); close(finished) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-finished
	if w := g.writes(); strings.Join(w, ";") != strings.Join(want, ";") {
		t.Fatalf("GitHub writes after the restart = %v, want no more", w)
	}
	g.assertPullRequestNeverWritten(t)
}

func newEngineLike(e *env) *updatetickets.Engine {
	return &updatetickets.Engine{
		Tickets: e.st.UpdateTickets(), Projects: e.st.TicketProjects(), Targets: targets{e.tracker}, Now: e.clock,
		Comments: e.engine.Comments, SweepInterval: 20 * time.Millisecond,
	}
}

// A crash between posting the comment and storing its id: the stored id is gone, and the next
// pass adopts the comment it finds by its marker instead of posting a second one.
func TestACommentWhoseIDWasNeverStoredIsAdoptedNotPostedAgain(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.engine.Tickets = &failingStore{UpdateTicketRepository: e.st.UpdateTickets(), failStore: 1}
	e.run(t)
	e.deps(id)

	first := e.waitComment(t, g, 21, "Tracked in")
	eventually(t, "the failed id write to be recorded", func() bool {
		return strings.HasPrefix(e.commentRow(t, id).lastError, "PR comment: ")
	})
	if row := e.commentRow(t, id); row.id != 0 || row.hash != "" {
		t.Fatalf("row = %+v, want no id stored", row)
	}

	e.advance(2 * time.Minute)
	eventually(t, "the retry to store the adopted comment", func() bool { return e.commentRow(t, id).hash != "" })
	if got := g.comments(21); len(got) != 1 || got[0].ID != first.ID {
		t.Fatalf("comments = %+v, want the one comment %d", got, first.ID)
	}
	if row := e.commentRow(t, id); row.id != first.ID || row.lastError != "" {
		t.Fatalf("row = %+v, want the adopted comment stored and the error cleared", row)
	}
	if w := g.writes(); len(w) != 1 {
		t.Fatalf("GitHub writes = %v, want only the first POST", w)
	}
}

type failingStore struct {
	store.UpdateTicketRepository
	mu        sync.Mutex
	failStore int
}

func (f *failingStore) StorePRComment(ctx context.Context, id string, commentID int64, state, hash string, now time.Time) error {
	f.mu.Lock()
	fail := f.failStore > 0
	if fail {
		f.failStore--
	}
	f.mu.Unlock()
	if fail {
		return errors.New("database is locked")
	}
	return f.UpdateTicketRepository.StorePRComment(ctx, id, commentID, state, hash, now)
}

// A person deleted the comment: the next change posts one new comment, and only one.
func TestACommentADeletedByAPersonIsRecreatedOnce(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	e.useMapping(t, distinctMapping)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)
	first := e.waitComment(t, g, 21, "state: open")
	eventually(t, "the id to be stored", func() bool { return e.commentRow(t, id).hash != "" })

	g.fake.DeleteComment(prRepo, 21, first.ID)
	e.setState(t, id, "closed")
	e.deps(id)
	again := e.waitComment(t, g, 21, "state: won't do")
	if again.ID == first.ID {
		t.Fatalf("the comment id is still %d, want a new comment", first.ID)
	}
	eventually(t, "the new id to be stored", func() bool { return e.commentRow(t, id).id == again.ID })
	time.Sleep(150 * time.Millisecond)
	if n := len(g.comments(21)); n != 1 {
		t.Fatalf("%d comments, want exactly one", n)
	}
}

// GitHub answers 5xx to the comment: the ticket is created and linked all the same, the failure
// is recorded on the ticket for the Problems view without holding any of its operations back, and
// the retry after the pause writes exactly one comment.
func TestAFailedCommentWriteIsRecordedAndRetriedWithoutHoldingBackTheTicket(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	e.useMapping(t, distinctMapping)
	g.fake.FailComments(http.MethodPost, http.StatusInternalServerError, 2, false)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)

	eventually(t, "the failure to be recorded", func() bool { return strings.HasPrefix(e.commentRow(t, id).lastError, "PR comment: ") })
	row, _ := e.row(t, id)
	if !row.ExternalID.Valid || row.State != "open" || row.NextAttemptAt.Valid || row.Attempts != 0 {
		t.Fatalf("ticket row = %+v, want the ticket created and linked, no retry or attempt held against it", row)
	}
	if n := len(g.comments(21)); n != 0 {
		t.Fatalf("%d comments, want none yet", n)
	}

	// While the comment keeps failing, the ticket still follows its PR.
	e.setState(t, id, "merged")
	e.deps(id)
	eventually(t, "the ticket to move to done", func() bool { r, _ := e.row(t, id); return r.State == "done" })
	if got := e.tracker.task("task-1").Status; got != "done" {
		t.Fatalf("ticket status = %q, want done", got)
	}

	// The next pause is over: the write succeeds, with the state the ticket has now.
	e.advance(time.Hour)
	c := e.waitComment(t, g, 21, "state: done")
	eventually(t, "the error to be cleared", func() bool { r := e.commentRow(t, id); return r.id == c.ID && r.lastError == "" && r.hash != "" })
	if n := len(g.comments(21)); n != 1 {
		t.Fatalf("%d comments, want one", n)
	}
}

// A comment that GitHub stored and then failed to answer for is adopted by the retry.
func TestACommentGitHubStoredBeforeItFailedToAnswerIsNotPostedTwice(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	g.fake.FailComments(http.MethodPost, http.StatusBadGateway, 1, true)
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)

	eventually(t, "the failure to be recorded", func() bool { return strings.HasPrefix(e.commentRow(t, id).lastError, "PR comment: ") })
	e.advance(2 * time.Minute)
	eventually(t, "the retry to store the comment", func() bool { return e.commentRow(t, id).hash != "" })
	if n := len(g.comments(21)); n != 1 {
		t.Fatalf("%d comments, want exactly one", n)
	}
}

// While the installation's rate-limit budget is used up the comment is deferred without a call
// to GitHub, recorded on the ticket, and written after the reset; the ticket is not touched.
func TestACommentIsDeferredWhileTheRateLimitBudgetIsUsedUp(t *testing.T) {
	e := newEnv(t)
	g := e.withGitHub(t)
	reset := epoch.Add(30 * time.Minute)
	g.budget.Observe(1, ratelimit.Signal{Limit: 5000, Remaining: 0, HasLimit: true, HasRemaining: true, ResetAt: reset})
	id := e.addPR(t, pr{Deps: foo})
	g.openPullRequest(21)
	e.run(t)
	e.deps(id)

	eventually(t, "the deferral to be recorded", func() bool { return strings.Contains(e.commentRow(t, id).lastError, "rate limited") })
	row, _ := e.row(t, id)
	if !row.ExternalID.Valid || row.NextAttemptAt.Valid {
		t.Fatalf("ticket row = %+v, want the ticket created and not held back", row)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(g.fake.Requests()); n != 0 {
		t.Fatalf("%d calls reached GitHub while the budget was used up, want none", n)
	}

	e.advance(time.Hour)
	g.budget.Observe(1, ratelimit.Signal{Limit: 5000, Remaining: 5000, HasLimit: true, HasRemaining: true, ResetAt: reset.Add(2 * time.Hour)})
	e.waitComment(t, g, 21, "Tracked in")
	eventually(t, "the deferral to be cleared", func() bool { return e.commentRow(t, id).lastError == "" })
}

// The error of a ticket operation is not replaced by a comment failure, and a PR comment error
// shows only when nothing else does.
func TestACommentFailureDoesNotReplaceTheErrorOfATicketOperation(t *testing.T) {
	e := newEnv(t)
	id := e.ticketed(t, pr{})
	row, _ := e.row(t, id)
	e.exec(t, `UPDATE update_tickets SET last_error = 'update ticket: boom', last_error_at = '2026-11-15T10:00:00.000Z' WHERE id = ?`, row.ID)

	if err := e.st.UpdateTickets().NotePRCommentError(context.Background(), row.ID, "github request failed", epoch); err != nil {
		t.Fatal(err)
	}
	if got := e.commentRow(t, id).lastError; got != "update ticket: boom" {
		t.Fatalf("last_error = %q, want the ticket operation's error kept", got)
	}
	if err := e.st.UpdateTickets().StorePRComment(context.Background(), row.ID, 5, "open", "h", epoch); err != nil {
		t.Fatal(err)
	}
	if got := e.commentRow(t, id).lastError; got != "update ticket: boom" {
		t.Fatalf("last_error = %q after a stored comment, want the ticket operation's error kept", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
