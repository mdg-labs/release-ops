package github_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	commentBot  int64 = 424242
	commentMark       = "<!-- release-ops:ticket -->"
	commentRepo       = "acme/api"
	commentPR   int64 = 7
)

type botIdentity struct {
	bot store.GitHubAppBot
	err error
}

func (b botIdentity) BotIdentity(context.Context) (store.GitHubAppBot, error) { return b.bot, b.err }
func (b botIdentity) RecordBotUserID(context.Context, int64) (bool, error)    { return false, nil }

type commentRig struct {
	fake      *githubtest.Fake
	clock     *githubtest.Clock
	budget    *ratelimit.Budget
	commenter *github.PRCommenter
}

func newCommentRig(t *testing.T, bot botIdentity) *commentRig {
	t.Helper()
	clock := githubtest.NewClock(t0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	fake.SetRepositories(1000, githubtest.Repo{ID: 1, FullName: commentRepo, DefaultBranch: "main"})
	fake.SetPullRequest(commentRepo, githubtest.PullRequest{Number: commentPR, Title: "t", State: "open", HeadRef: "renovate/x", CreatedAt: t0, UpdatedAt: t0})
	fake.SetCommentAuthor(commentBot)
	budget := ratelimit.NewBudget(ratelimit.Options{Now: clock.Now})
	api := github.NewClient(fake.URL, 5*time.Second).UseRateLimits(budget)
	app := github.NewAppClient(api, staticCreds{appID: githubtest.AppID, pem: fake.PEM}, github.AppClientOptions{Now: clock.Now, Budget: budget})
	if bot.bot.Slug == "" && bot.err == nil {
		bot.bot = store.GitHubAppBot{Slug: "release-ops-fixture", UserID: commentBot, Known: true}
	}
	return &commentRig{fake: fake, clock: clock, budget: budget, commenter: github.NewPRCommenter(api, app, bot)}
}

func (r *commentRig) upsert(id int64, body string) (int64, error) {
	return r.commenter.Upsert(context.Background(), github.CommentUpsert{
		InstallationID: 1000, Repository: commentRepo, Number: commentPR, Marker: commentMark, CommentID: id, Body: body,
	})
}

// commentCalls are the requests that reach the comment endpoints or the pull request itself.
func (r *commentRig) writes() []string {
	var out []string
	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/repos/") && req.Method != http.MethodGet {
			out = append(out, req.Method+" "+req.Path)
		}
	}
	return out
}

func (r *commentRig) assertPullRequestUntouched(t *testing.T) {
	t.Helper()
	for _, req := range r.fake.Requests() {
		if strings.Contains(req.Path, "/pulls/") && req.Method != http.MethodGet {
			t.Fatalf("%s %s: the pull request itself must never be written", req.Method, req.Path)
		}
	}
}

func TestUpsertPostsOneCommentOnAPullRequestWithNone(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	body := commentMark + "\nTracked in [KEY-1](https://tracker.example/KEY-1)"

	id, err := r.upsert(0, body)
	if err != nil || id == 0 {
		t.Fatalf("Upsert = %d, %v", id, err)
	}
	got := r.fake.Comments(commentRepo, commentPR)
	if len(got) != 1 || got[0].ID != id || got[0].Body != body || got[0].UserID != commentBot {
		t.Fatalf("comments = %+v, want the one comment", got)
	}
	if w := r.writes(); len(w) != 1 || w[0] != "POST /repos/acme/api/issues/7/comments" {
		t.Fatalf("writes = %v, want one POST", w)
	}
	r.assertPullRequestUntouched(t)
}

func TestUpsertEditsTheStoredCommentInPlace(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	id, err := r.upsert(0, commentMark+"\nopen")
	if err != nil {
		t.Fatal(err)
	}
	again, err := r.upsert(id, commentMark+"\ndone")
	if err != nil || again != id {
		t.Fatalf("Upsert = %d, %v, want the same comment %d", again, err, id)
	}
	got := r.fake.Comments(commentRepo, commentPR)
	if len(got) != 1 || got[0].Body != commentMark+"\ndone" {
		t.Fatalf("comments = %+v, want the one comment edited", got)
	}
	want := []string{"POST /repos/acme/api/issues/7/comments", "PATCH /repos/acme/api/issues/comments/" + itoa(id)}
	if w := r.writes(); strings.Join(w, ";") != strings.Join(want, ";") {
		t.Fatalf("writes = %v, want %v", w, want)
	}
	r.assertPullRequestUntouched(t)
}

// With no stored id the comments are listed, across pages, and the bot's comment with the marker
// is adopted; another user's comment that quotes the marker and the bot's comments without it are
// not taken for it.
func TestUpsertAdoptsTheBotsMarkerCommentAmongOthersWithoutPostingAgain(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	r.fake.SetPageSize(2)
	r.fake.AddComment(commentRepo, commentPR, 7, "looks good")
	r.fake.AddComment(commentRepo, commentPR, 99, commentMark+"\nquoted by a person")
	r.fake.AddComment(commentRepo, commentPR, commentBot, "the bot said something else")
	mine := r.fake.AddComment(commentRepo, commentPR, commentBot, commentMark+"\nopen")
	r.fake.AddComment(commentRepo, commentPR, 8, "thanks")

	id, err := r.upsert(0, commentMark+"\ndone")
	if err != nil || id != mine {
		t.Fatalf("Upsert = %d, %v, want the adopted comment %d", id, err, mine)
	}
	if got := r.fake.Comments(commentRepo, commentPR); len(got) != 5 {
		t.Fatalf("%d comments, want the same 5: nothing is posted", len(got))
	}
	for _, c := range r.fake.Comments(commentRepo, commentPR) {
		if c.ID == mine && c.Body != commentMark+"\ndone" {
			t.Fatalf("adopted comment = %q, want it brought to the new body", c.Body)
		}
		if c.UserID == 99 && !strings.HasSuffix(c.Body, "quoted by a person") {
			t.Fatalf("another user's comment was edited: %q", c.Body)
		}
	}
	if w := r.writes(); len(w) != 1 || !strings.HasPrefix(w[0], "PATCH ") {
		t.Fatalf("writes = %v, want one PATCH", w)
	}
}

func TestUpsertLeavesAnAdoptedCommentThatAlreadyHasTheBodyAlone(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	body := commentMark + "\nopen"
	mine := r.fake.AddComment(commentRepo, commentPR, commentBot, body)
	if id, err := r.upsert(0, body); err != nil || id != mine {
		t.Fatalf("Upsert = %d, %v, want %d", id, err, mine)
	}
	if w := r.writes(); len(w) != 0 {
		t.Fatalf("writes = %v, want none", w)
	}
}

// A person deleted the comment: the stored id answers 404 and exactly one new comment is posted.
func TestUpsertRecreatesACommentThatWasDeletedOnce(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	id, err := r.upsert(0, commentMark+"\nopen")
	if err != nil {
		t.Fatal(err)
	}
	r.fake.DeleteComment(commentRepo, commentPR, id)

	fresh, err := r.upsert(id, commentMark+"\ndone")
	if err != nil || fresh == id || fresh == 0 {
		t.Fatalf("Upsert = %d, %v, want a new comment", fresh, err)
	}
	if got := r.fake.Comments(commentRepo, commentPR); len(got) != 1 || got[0].ID != fresh {
		t.Fatalf("comments = %+v, want exactly the new one", got)
	}
	// The stored id is the new one now: the next change edits it.
	if again, err := r.upsert(fresh, commentMark+"\nsuperseded"); err != nil || again != fresh {
		t.Fatalf("Upsert = %d, %v, want %d edited", again, err, fresh)
	}
	if got := r.fake.Comments(commentRepo, commentPR); len(got) != 1 {
		t.Fatalf("comments = %+v, want still one", got)
	}
}

// A comment that was posted but whose id was never stored (a crash, or an answer that was lost)
// is adopted by the retry, never posted twice, including when the id that was stored is a deleted
// one.
func TestUpsertAfterALostAnswerOrAStaleIDNeverPostsASecondComment(t *testing.T) {
	for name, stale := range map[string]bool{"no id stored": false, "a deleted id stored": true} {
		t.Run(name, func(t *testing.T) {
			r := newCommentRig(t, botIdentity{})
			var storedID int64
			if stale {
				id, err := r.upsert(0, commentMark+"\nopen")
				if err != nil {
					t.Fatal(err)
				}
				r.fake.DeleteComment(commentRepo, commentPR, id)
				storedID = id
			}
			r.fake.FailComments(http.MethodPost, http.StatusBadGateway, 1, true)
			if _, err := r.upsert(storedID, commentMark+"\ndone"); !errors.Is(err, github.ErrRequestFailed) {
				t.Fatalf("Upsert = %v, want the failed answer", err)
			}
			if got := r.fake.Comments(commentRepo, commentPR); len(got) != 1 {
				t.Fatalf("comments = %+v, want the one GitHub stored", got)
			}
			id, err := r.upsert(storedID, commentMark+"\ndone")
			if err != nil {
				t.Fatal(err)
			}
			if got := r.fake.Comments(commentRepo, commentPR); len(got) != 1 || got[0].ID != id {
				t.Fatalf("comments = %+v, want the one comment adopted as %d", got, id)
			}
		})
	}
}

func TestUpsertReportsAServerErrorAndPostsNothingUntilItIsRetried(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r.fake.FailComments(method, http.StatusInternalServerError, 1, false)
		if _, err := r.upsert(0, commentMark+"\nopen"); !errors.Is(err, github.ErrRequestFailed) {
			t.Fatalf("%s failing: Upsert = %v, want ErrRequestFailed", method, err)
		}
		if got := r.fake.Comments(commentRepo, commentPR); len(got) != 0 {
			t.Fatalf("%s failing: comments = %+v, want none", method, got)
		}
	}
	if _, err := r.upsert(0, commentMark+"\nopen"); err != nil {
		t.Fatal(err)
	}
	if got := r.fake.Comments(commentRepo, commentPR); len(got) != 1 {
		t.Fatalf("comments = %+v, want one", got)
	}
}

func TestUpsertDoesNotAdoptOnAFailedPage(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	r.fake.AddComment(commentRepo, commentPR, commentBot, commentMark+"\nopen")
	r.fake.FailComments(http.MethodGet, http.StatusInternalServerError, 1, false)
	if _, err := r.upsert(0, commentMark+"\ndone"); err == nil {
		t.Fatal("Upsert succeeded although the listing failed")
	}
	if w := r.writes(); len(w) != 0 {
		t.Fatalf("writes = %v, want none: a comment is never posted without having seen all of them", w)
	}
}

func TestUpsertIsDeferredWhileTheRateLimitBudgetIsUsedUp(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	reset := t0.Add(20 * time.Minute)
	r.budget.Observe(1000, ratelimit.Signal{Limit: 5000, Remaining: 0, HasLimit: true, HasRemaining: true, ResetAt: reset})
	before := len(r.fake.Requests())

	_, err := r.upsert(0, commentMark+"\nopen")
	if !errors.Is(err, ratelimit.ErrRateLimited) {
		t.Fatalf("Upsert = %v, want ErrRateLimited", err)
	}
	if d, ok := ratelimit.DelayOf(err); !ok || d != 20*time.Minute {
		t.Fatalf("delay = %v, %v, want 20 minutes", d, ok)
	}
	if after := len(r.fake.Requests()); after != before {
		t.Fatalf("GitHub received %d calls while deferred, want none", after-before)
	}
}

func TestUpsertNeedsTheBotsIdentity(t *testing.T) {
	for name, bot := range map[string]botIdentity{
		"id not read yet": {bot: store.GitHubAppBot{Slug: "ro"}},
		"no app stored":   {err: store.ErrGitHubAppNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			r := newCommentRig(t, bot)
			if _, err := r.upsert(0, commentMark+"\nopen"); !errors.Is(err, github.ErrBotUnknown) {
				t.Fatalf("Upsert = %v, want ErrBotUnknown", err)
			}
			if n := len(r.fake.Requests()); n != 0 {
				t.Fatalf("%d calls reached GitHub, want none", n)
			}
		})
	}
}

func TestUpsertRefusesABodyWithoutItsMarker(t *testing.T) {
	r := newCommentRig(t, botIdentity{})
	if _, err := r.upsert(0, "Tracked"); err == nil {
		t.Fatal("Upsert accepted a body without the marker")
	}
	if n := len(r.fake.Requests()); n != 0 {
		t.Fatalf("%d calls reached GitHub, want none", n)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
