package updateprs_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/updateprs"
)

func TestReconcileCreatesTheOpenBotPRsGitHubListsAndNothingElse(t *testing.T) {
	r := newRig(t, true)
	r.bot(11)
	r.bot(12, func(p *githubtest.PullRequest) { p.HeadRef, p.Title = "deps/custom", "Bump" })
	r.bot(13, func(p *githubtest.PullRequest) { p.AuthorID = 1 })                        // a person, with a Renovate body
	r.bot(14, func(p *githubtest.PullRequest) { p.Body = "Enable Renovate." })           // the bot, no markers
	r.bot(15, func(p *githubtest.PullRequest) { p.State, p.ClosedAt = "closed", at(2) }) // not listed

	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got, want := r.states(t), map[int64]string{11: "open", 12: "open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if !r.woken() {
		t.Fatal("creating rows did not wake the engine")
	}
	for _, req := range r.fake.Requests() {
		if req.Method == "GET" && req.Path == "/repos/acme/repo-1/pulls" && req.Query != "state=open&per_page=100" {
			t.Errorf("listing query = %q", req.Query)
		}
	}
}

func TestReconcileReadsEveryPage(t *testing.T) {
	r := newRig(t, true)
	r.fake.SetPageSize(2)
	for n := int64(1); n <= 5; n++ {
		r.bot(n)
	}
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	if got := len(r.states(t)); got != 5 {
		t.Fatalf("%d rows over three pages, want 5", got)
	}
}

func TestReconcileSetsAVanishedOpenPRToMergedOrClosedAsGitHubReports(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	r.bot(2)
	r.bot(3)
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	r.woken()

	r.bot(1, func(p *githubtest.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = "closed", true, at(30), at(30), at(30)
	})
	r.bot(2, func(p *githubtest.PullRequest) { p.State, p.ClosedAt, p.UpdatedAt = "closed", at(31), at(31) })
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	if got, want := r.states(t), map[int64]string{1: "merged", 2: "closed", 3: "open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if !r.woken() {
		t.Fatal("a state change found by the reconcile did not wake the engine")
	}
}

func TestReconcileLeavesAnUnchangedRepositoryAloneAndWakesNothing(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	ctx := context.Background()
	if err := r.rec.Reconcile(ctx, r.repo); err != nil {
		t.Fatal(err)
	}
	r.woken()
	if err := r.rec.Reconcile(ctx, r.repo); err != nil {
		t.Fatal(err)
	}
	if r.woken() {
		t.Fatal("a reconcile that changed nothing woke the engine")
	}
}

// The second listing is conditional and GitHub answers 304; the stored rows are still compared
// with the cached listing, so a row lost locally comes back.
func TestReconcileUsesTheETagAndStillRepairsDriftOnANotModifiedPage(t *testing.T) {
	r := newRig(t, true)
	r.fake.SetPageSize(1)
	r.bot(1)
	r.bot(2)
	ctx := context.Background()
	if err := r.rec.Reconcile(ctx, r.repo); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Writer.Exec(`DELETE FROM update_prs WHERE number = 2`); err != nil {
		t.Fatal(err)
	}
	r.woken()

	if err := r.rec.Reconcile(ctx, r.repo); err != nil {
		t.Fatal(err)
	}
	if got := r.fake.NotModifiedAnswers(); got != 2 {
		t.Fatalf("%d pages answered 304, want both", got)
	}
	var conditional int
	for _, req := range r.fake.Requests() {
		if req.Path == "/repos/acme/repo-1/pulls" && req.IfNoneMatch != "" {
			conditional++
		}
	}
	if conditional != 2 {
		t.Fatalf("%d conditional requests, want 2", conditional)
	}
	if got, want := r.states(t), map[int64]string{1: "open", 2: "open"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want the lost row back", got)
	}
	if !r.woken() {
		t.Fatal("the recreated row did not wake the engine")
	}

	// A page that changed is read again, not served from the cache.
	r.bot(2, func(p *githubtest.PullRequest) { p.Title, p.UpdatedAt = "renamed", at(5) })
	if err := r.rec.Reconcile(ctx, r.repo); err != nil {
		t.Fatal(err)
	}
	var title string
	if err := r.db.Reader.QueryRow(`SELECT title FROM update_prs WHERE number = 2`).Scan(&title); err != nil || title != "renamed" {
		t.Fatalf("title = %q, %v", title, err)
	}
}

// A failed call leaves what was stored as it was: nothing is half applied and the error comes
// back, so the caller does not report the repository reconciled.
func TestReconcileFailureAppliesNothing(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	r.woken()

	t.Run("listing", func(t *testing.T) {
		r.bot(2)
		r.fake.FailPulls("acme/repo-1", 500)
		if err := r.rec.Reconcile(context.Background(), r.repo); err == nil {
			t.Fatal("a failed listing returned no error")
		}
		r.fake.FailPulls("acme/repo-1", 0)
		if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "open"}) {
			t.Fatalf("rows = %v", got)
		}
	})
	t.Run("fetch of a vanished PR after others were listed", func(t *testing.T) {
		r.bot(1, func(p *githubtest.PullRequest) { p.State, p.ClosedAt, p.UpdatedAt = "closed", at(9), at(9) })
		r.fake.FailPullRequest("acme/repo-1", 1, 502)
		if err := r.rec.Reconcile(context.Background(), r.repo); err == nil {
			t.Fatal("a failed fetch returned no error")
		}
		if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "open"}) {
			t.Fatalf("rows = %v, want the listed PR 2 not created and PR 1 still open", got)
		}
		if r.woken() {
			t.Fatal("a failed reconcile woke the engine")
		}
		r.fake.FailPullRequest("acme/repo-1", 1, 0)
		if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
			t.Fatal(err)
		}
		if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "closed", 2: "open"}) {
			t.Fatalf("rows after the retry = %v", got)
		}
	})
}

func TestReconcileSkipsAStoredPRGitHubDoesNotKnow(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	r.bot(2)
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	r.fake.FailPullRequest("acme/repo-1", 1, 404)
	r.bot(1, func(p *githubtest.PullRequest) { p.State, p.UpdatedAt = "closed", at(9) })
	r.bot(2, func(p *githubtest.PullRequest) { p.State, p.ClosedAt, p.UpdatedAt = "closed", at(9), at(9) })
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "open", 2: "closed"}) {
		t.Fatalf("rows = %v", got)
	}
}

func TestReconcileDoesNothingWithoutABotIdentityOrForAPausedRepository(t *testing.T) {
	t.Run("bot user id not known", func(t *testing.T) {
		r := newRig(t, false)
		r.bot(1)
		if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
			t.Fatal(err)
		}
		if got := r.states(t); len(got) != 0 {
			t.Fatalf("rows = %v, want none", got)
		}
		if n := len(r.fake.Requests()); n != 0 {
			t.Fatalf("%d GitHub calls without a bot id, want none", n)
		}
	})
	t.Run("paused repository", func(t *testing.T) {
		r := newRig(t, true)
		r.bot(1)
		if _, err := r.db.Writer.Exec(`UPDATE repositories SET enabled = 0`); err != nil {
			t.Fatal(err)
		}
		if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
			t.Fatal(err)
		}
		if n := len(r.fake.Requests()); n != 0 {
			t.Fatalf("%d GitHub calls for a paused repository, want none", n)
		}
	})
	t.Run("unknown repository", func(t *testing.T) {
		r := newRig(t, true)
		if err := r.rec.Reconcile(context.Background(), "nope"); err != nil {
			t.Fatal(err)
		}
	})
}

func TestReconcileReadsWithATokenScopedToTheRepository(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	if err := r.rec.Reconcile(context.Background(), r.repo); err != nil {
		t.Fatal(err)
	}
	if got, want := r.fake.TokenRequests(), [][]string{{"repo-1"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("token requests = %v, want %v", got, want)
	}
}

type recorder struct {
	mu   sync.Mutex
	done []string
	errs []error
	ch   chan struct{}
}

func (r *recorder) report(id string, err error) {
	r.mu.Lock()
	r.done = append(r.done, id)
	r.errs = append(r.errs, err)
	r.mu.Unlock()
	r.ch <- struct{}{}
}

func TestRunnerReconcilesWhatWasAskedForOffTheCallerAndCoalescesRepeats(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	rec := &recorder{ch: make(chan struct{}, 8)}
	runner := &updateprs.Runner{Reconciler: r.rec, Repositories: r.st.UpdatePRs(), Report: rec.report}

	// Asked before Run starts: three requests for one repository wait as one.
	runner.Trigger(r.repo)
	runner.Trigger(r.repo)
	runner.Trigger(r.repo)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { runner.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	wait := func() {
		select {
		case <-rec.ch:
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for a reconcile")
		}
	}
	wait()
	if got := r.states(t); !reflect.DeepEqual(got, map[int64]string{1: "open"}) {
		t.Fatalf("rows = %v", got)
	}
	if err := runner.TriggerAll(ctx); err != nil {
		t.Fatal(err)
	}
	wait()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if !reflect.DeepEqual(rec.done, []string{r.repo, r.repo}) || rec.errs[0] != nil || rec.errs[1] != nil {
		t.Fatalf("reconciled %v with %v, want the repository once per ask", rec.done, rec.errs)
	}
}

func TestRunnerReportsAFailedReconcileAndGoesOn(t *testing.T) {
	r := newRig(t, true)
	r.bot(1)
	r.fake.FailPulls("acme/repo-1", 500)
	rec := &recorder{ch: make(chan struct{}, 8)}
	runner := &updateprs.Runner{Reconciler: r.rec, Repositories: r.st.UpdatePRs(), Report: rec.report}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { runner.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })

	runner.Trigger(r.repo)
	<-rec.ch
	r.fake.FailPulls("acme/repo-1", 0)
	runner.Trigger(r.repo)
	<-rec.ch
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.errs[0] == nil || rec.errs[1] != nil {
		t.Fatalf("errors = %v, want the first failed and the second to succeed", rec.errs)
	}
	if got := r.states(t); len(got) != 1 {
		t.Fatalf("rows = %v, want the PR stored by the second reconcile", got)
	}
}
