package pollwatch_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/pollwatch"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const botID int64 = 99887766

const (
	repoName  = "acme/repo-1"
	dashboard = "## Dependency Dashboard\n\n - [ ] <!-- create-all-rate-limited-prs -->Create all\n - [ ] <!-- approve-branch=renovate/foo -->Approve foo\n"
	prBody    = "Updates.\n\n - [ ] <!-- rebase-check -->rebase\n"
)

var epoch = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

type rig struct {
	db    *store.DB
	st    *store.Store
	fake  *githubtest.Fake
	jobs  *store.JobQueue
	queue pollwatch.Enqueuer
	w     *pollwatch.Watcher
	repo  string
}

// newRig stores an App with the bot user id, one repository (acme/repo-1, default branch main,
// installation 1) and serves a fake GitHub that knows the installation. The bot's dashboard is
// issue 1, one bot PR is number 5, and main is at commit c1.
func newRig(t *testing.T, withBot bool) *rig {
	t.Helper()
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1, githubtest.AppID)
	if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "ws",
	}); err != nil {
		t.Fatal(err)
	}
	if withBot {
		if _, err := st.GitHubApp().RecordBotUserID(context.Background(), botID); err != nil {
			t.Fatal(err)
		}
	}
	client := github.NewClient(fake.URL, 0)
	jobs := store.NewJobQueue(db, store.JobQueueOptions{})
	r := &rig{db: db, st: st, fake: fake, jobs: jobs, queue: jobs, repo: storetest.SeedRepositories(t, db, 1)[0]}
	r.w = &pollwatch.Watcher{
		Repositories: st.UpdatePRs(),
		Branches:     st.Repositories(),
		Bot:          st.GitHubApp(),
		Tokens:       github.NewAppClient(client, st.GitHubApp(), github.AppClientOptions{}),
		API:          client,
		Queue:        r,
	}
	r.dashboard(dashboard)
	r.pr(5, prBody)
	fake.SetBranchHead(repoName, "main", "c1")
	return r
}

// Enqueue lets a test make the queue refuse a request.
type refusing struct {
	pollwatch.Enqueuer
	err error
}

func (r *refusing) Enqueue(ctx context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error) {
	if r.err != nil {
		return queue.EnqueueResult{}, r.err
	}
	return r.Enqueuer.Enqueue(ctx, p)
}

func (r *rig) Enqueue(ctx context.Context, p queue.EnqueueParams) (queue.EnqueueResult, error) {
	return r.queue.Enqueue(ctx, p)
}

func (r *rig) dashboard(body string) {
	r.fake.SetIssue(repoName, githubtest.Issue{Number: 1, Title: "Dependency Dashboard", Body: body, AuthorID: botID, State: "open"})
}

func (r *rig) pr(number int64, body string, mutate ...func(*githubtest.PullRequest)) {
	pr := githubtest.PullRequest{
		Number: number, Title: "chore(deps): update", Body: body, AuthorID: botID,
		HeadRef: "renovate/dep", HeadSHA: "abc", State: "open", CreatedAt: epoch, UpdatedAt: epoch.Add(time.Minute),
	}
	for _, m := range mutate {
		m(&pr)
	}
	r.fake.SetPullRequest(repoName, pr)
}

func (r *rig) poll(t *testing.T) pollwatch.Result {
	t.Helper()
	res, err := r.w.Poll(context.Background())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	return res
}

type job struct{ priority, trigger, markers string }

func (r *rig) jobRows(t *testing.T) []job {
	t.Helper()
	rows, err := r.db.Reader.Query(`SELECT priority, "trigger", COALESCE(matched_markers, '') FROM jobs ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.priority, &j.trigger, &j.markers); err != nil {
			t.Fatal(err)
		}
		out = append(out, j)
	}
	return out
}

func (r *rig) wantNoJob(t *testing.T) {
	t.Helper()
	if got := r.jobRows(t); len(got) != 0 {
		t.Fatalf("jobs = %v, want none", got)
	}
}

// wantOneJob checks that exactly one normal-priority polling job exists whose markers hold each
// of the given texts.
func (r *rig) wantOneJob(t *testing.T, markers ...string) {
	t.Helper()
	got := r.jobRows(t)
	if len(got) != 1 || got[0].priority != "normal" || got[0].trigger != "polling" {
		t.Fatalf("jobs = %v, want one normal polling job", got)
	}
	for _, m := range markers {
		if !strings.Contains(got[0].markers, m) {
			t.Fatalf("job markers = %q, want %q in them", got[0].markers, m)
		}
	}
}

func tick(body, marker string) string {
	return strings.Replace(body, "[ ] <!-- "+marker+" -->", "[x] <!-- "+marker+" -->", 1)
}

func TestThePassThatFirstSeesARepositoryOnlyTakesTheBaseline(t *testing.T) {
	r := newRig(t, true)
	if res := r.poll(t); res.Checked != 1 || res.Requested != 0 {
		t.Fatalf("result = %+v", res)
	}
	r.wantNoJob(t)
}

func TestATickedDashboardCheckboxRequestsOneNormalPriorityRun(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	if res := r.poll(t); res.Requested != 1 {
		t.Fatalf("result = %+v", res)
	}
	r.wantOneJob(t, "approve-branch=renovate/foo")

	// The same body on the next pass is not a new tick.
	r.poll(t)
	if got := r.jobRows(t); len(got) != 1 {
		t.Fatalf("jobs after an unchanged pass = %v", got)
	}
}

func TestATickedBotPRCheckboxRequestsARun(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.pr(5, tick(prBody, "rebase-check"))
	r.poll(t)
	r.wantOneJob(t, "rebase-check")
}

func TestAnEditThatTicksNothingRequestsNoRun(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	// The bot rewrites its dashboard and unticks a box; a person's edit of the text ticks none.
	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	r.poll(t)
	r.jobsReset(t)
	r.dashboard(dashboard + "\nMore text.\n")
	r.pr(5, prBody+"\nMore text.\n")
	r.poll(t)
	r.wantNoJob(t)
}

func (r *rig) jobsReset(t *testing.T) {
	t.Helper()
	if _, err := r.db.Writer.Exec(`DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
}

func TestIssuesAndPRsOfAPersonAreNotWatched(t *testing.T) {
	r := newRig(t, true)
	r.fake.SetIssue(repoName, githubtest.Issue{Number: 2, Body: dashboard, AuthorID: 1, State: "open"})
	r.pr(6, prBody, func(p *githubtest.PullRequest) { p.AuthorID = 1 })
	r.poll(t)

	r.fake.SetIssue(repoName, githubtest.Issue{Number: 2, Body: tick(dashboard, "create-all-rate-limited-prs"), AuthorID: 1, State: "open"})
	r.pr(6, tick(prBody, "rebase-check"), func(p *githubtest.PullRequest) { p.AuthorID = 1 })
	r.poll(t)
	r.fake.SetIssue(repoName, githubtest.Issue{Number: 2, Body: dashboard, AuthorID: 1, State: "closed"})
	r.fake.SetPullRequest(repoName, githubtest.PullRequest{
		Number: 6, Body: prBody, AuthorID: 1, HeadRef: "x", State: "closed", CreatedAt: epoch, UpdatedAt: epoch,
	})
	r.poll(t)
	r.wantNoJob(t)
}

func TestAClosedBotPRRequestsARun(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.pr(5, prBody, func(p *githubtest.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = "closed", true, epoch.Add(time.Hour), epoch.Add(time.Hour), epoch.Add(time.Hour)
	})
	r.poll(t)
	r.wantOneJob(t)
}

func TestANewHeadOfTheDefaultBranchRequestsARunAndAnotherBranchDoesNot(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.fake.SetBranchHead(repoName, "feature/x", "f1")
	r.poll(t)
	r.wantNoJob(t)

	r.fake.SetBranchHead(repoName, "main", "c2")
	r.poll(t)
	r.wantOneJob(t)
}

func TestSeveralChangesOfOnePassAreOneRun(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	r.pr(5, tick(prBody, "rebase-check"))
	r.fake.SetBranchHead(repoName, "main", "c2")
	if res := r.poll(t); res.Requested != 1 {
		t.Fatalf("result = %+v", res)
	}
	r.wantOneJob(t, "approve-branch=renovate/foo", "rebase-check")
}

// An unchanged repository answers 304 to every request, so it costs no rate-limit budget, and
// is compared with what the last pass read.
func TestAnUnchangedRepositoryAnswersNotModifiedAndRequestsNothing(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)
	pulls := r.fake.NotModifiedAnswers()
	others := r.fake.WatchNotModifiedAnswers()

	r.poll(t)
	r.wantNoJob(t)
	if got := r.fake.NotModifiedAnswers() - pulls; got != 1 {
		t.Errorf("pulls pages answered 304 = %d, want 1", got)
	}
	if got := r.fake.WatchNotModifiedAnswers() - others; got != 2 {
		t.Errorf("issue pages and branch references answered 304 = %d, want 2", got)
	}
	var conditional int
	for _, req := range r.fake.Requests() {
		if req.IfNoneMatch != "" {
			conditional++
		}
	}
	if conditional != 3 {
		t.Errorf("%d conditional requests, want one per request of the second pass", conditional)
	}
}

func TestEveryPageOfAListingIsWatched(t *testing.T) {
	r := newRig(t, true)
	r.fake.SetPageSize(1)
	r.pr(6, prBody)
	r.pr(7, prBody)
	r.poll(t)

	r.pr(5, tick(prBody, "rebase-check")) // the last page of the PRs
	r.poll(t)
	r.wantOneJob(t, "rebase-check")
}

// A request the queue refused leaves the baseline alone, so the next pass finds the change again,
// also when GitHub answers 304 for it by then.
func TestARefusedRequestIsMadeAgainOnTheNextPass(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)
	refuse := &refusing{Enqueuer: r.jobs, err: errors.New("database is locked")}
	r.w.Queue = refuse

	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	r.pr(5, prBody, func(p *githubtest.PullRequest) { p.State, p.ClosedAt = "closed", epoch.Add(time.Hour) })
	r.fake.SetBranchHead(repoName, "main", "c2")
	if _, err := r.w.Poll(context.Background()); err == nil {
		t.Fatal("Poll succeeded although the queue refused the request")
	}
	r.wantNoJob(t)

	refuse.err = nil
	r.poll(t)
	r.wantOneJob(t, "approve-branch=renovate/foo")
	r.jobsReset(t)

	r.poll(t)
	r.wantNoJob(t)
}

func TestARefusedRequestForANewHeadIsMadeAgainWhenGitHubAnswersNotModified(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)
	refuse := &refusing{Enqueuer: r.jobs, err: errors.New("database is locked")}
	r.w.Queue = refuse

	r.fake.SetBranchHead(repoName, "main", "c2")
	if _, err := r.w.Poll(context.Background()); err == nil {
		t.Fatal("Poll succeeded although the queue refused the request")
	}
	refuse.err = nil
	notModified := r.fake.WatchNotModifiedAnswers()
	r.poll(t)
	if r.fake.WatchNotModifiedAnswers() <= notModified {
		t.Fatal("the branch reference was not read conditionally")
	}
	r.wantOneJob(t)
}

func TestAPausedRepositoryIsNotAnError(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)
	r.w.Queue = &refusing{Enqueuer: r.jobs, err: queue.ErrRepositoryPaused}

	r.fake.SetBranchHead(repoName, "main", "c2")
	r.poll(t)
	r.w.Queue = r.jobs
	r.fake.SetBranchHead(repoName, "main", "c2")
	r.poll(t)
	r.wantNoJob(t)
}

// A listing that fails does not hide what the others found.
func TestAFailingListingDoesNotHideAChangeTheOthersFound(t *testing.T) {
	r := newRig(t, true)
	r.poll(t)

	r.fake.FailPulls(repoName, 500)
	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	if _, err := r.w.Poll(context.Background()); err == nil {
		t.Fatal("Poll succeeded although the pull request listing failed")
	}
	r.wantOneJob(t, "approve-branch=renovate/foo")
}

func TestWithoutTheBotUserIdOnlyTheDefaultBranchIsWatched(t *testing.T) {
	r := newRig(t, false)
	r.poll(t)

	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	r.poll(t)
	r.wantNoJob(t)
	for _, req := range r.fake.Requests() {
		if req.Path == "/repos/acme/repo-1/issues" || req.Path == "/repos/acme/repo-1/pulls" {
			t.Fatalf("%s was read without a bot user id", req.Path)
		}
	}

	r.fake.SetBranchHead(repoName, "main", "c2")
	r.poll(t)
	r.wantOneJob(t)
}

func TestAPausedRepositoryIsNotRead(t *testing.T) {
	r := newRig(t, true)
	if _, err := r.db.Writer.Exec(`UPDATE repositories SET enabled = 0`); err != nil {
		t.Fatal(err)
	}
	if res := r.poll(t); res.Checked != 0 {
		t.Fatalf("result = %+v", res)
	}
	for _, req := range r.fake.Requests() {
		if strings.HasPrefix(req.Path, "/repos/") {
			t.Fatalf("%s was read for a paused repository", req.Path)
		}
	}
}

// Run takes the baseline at once and polls again on every tick, which the test drives itself.
func TestRunPollsOnEveryTick(t *testing.T) {
	r := newRig(t, true)
	ticks := make(chan time.Time)
	results := make(chan pollwatch.Result)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.w.Run(ctx, ticks, func(res pollwatch.Result, err error) {
			if err != nil {
				t.Errorf("pass failed: %v", err)
			}
			results <- res
		})
	}()
	t.Cleanup(func() { cancel(); <-done })

	if res := <-results; res.Requested != 0 {
		t.Fatalf("first pass = %+v, want the baseline only", res)
	}
	r.dashboard(tick(dashboard, "approve-branch=renovate/foo"))
	ticks <- time.Now()
	if res := <-results; res.Requested != 1 {
		t.Fatalf("second pass = %+v", res)
	}
	r.wantOneJob(t, "approve-branch=renovate/foo")
}
