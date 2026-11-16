package internalapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type tally struct {
	mu       sync.Mutex
	hits     int
	deferred map[string]int
}

func (r *tally) Hit(int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits++
}

func (r *tally) Deferred(_ int64, consumer string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deferred == nil {
		r.deferred = map[string]int{}
	}
	r.deferred[consumer]++
}

type rateEnv struct {
	*tokenEnv
	db     *store.DB
	budget *ratelimit.Budget
	tally  *tally
}

// newRateEnv is newTokenEnv with the shared rate-limit budget behind the token route and the
// result route, on one fake clock.
func newRateEnv(t *testing.T) *rateEnv {
	t.Helper()

	db := storetest.OpenMigrated(t)
	clock := githubtest.NewClock(tokenT0)
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1, githubtest.AppID)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("cd", 32))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	if err := st.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "webhook-secret-value",
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}
	counts := &tally{}
	budget := ratelimit.NewBudget(ratelimit.Options{Now: clock.Now, Store: st.RateLimits(), Recorder: counts})
	q := store.NewJobQueue(db, store.JobQueueOptions{Now: clock.Now, Rand: func() float64 { return 0.5 }})
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second).UseRateLimits(budget), st.GitHubApp(),
		github.AppClientOptions{Now: clock.Now, Budget: budget})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: secret, Tokens: app, RateLimits: budget, Now: clock.Now}))
	t.Cleanup(srv.Close)
	return &rateEnv{
		tokenEnv: &tokenEnv{srv: srv, q: q, fake: fake, clock: clock, repos: storetest.SeedRepositories(t, db, 2), logs: &syncBuffer{}},
		db:       db, budget: budget, tally: counts,
	}
}

func (e *rateEnv) result(t *testing.T, job *queue.ClaimedJob, req queue.ResultRequest) int {
	t.Helper()
	req.LeaseOwner = job.LeaseOwner
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	httpReq, err := http.NewRequest(http.MethodPost, e.srv.URL+strings.Replace(queue.PathResult, "{id}", job.ID, 1), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func (e *rateEnv) jobRow(t *testing.T, id string) (state string, attempts int, runAfter time.Time) {
	t.Helper()
	var ra string
	if err := e.db.Writer.QueryRow(`SELECT state, attempts, run_after FROM jobs WHERE id = ?`, id).Scan(&state, &attempts, &ra); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse("2006-01-02T15:04:05.000Z", ra)
	if err != nil {
		t.Fatal(err)
	}
	return state, attempts, at
}

func i64(n int64) *int64 { return &n }

// While the installation's budget is exhausted the token route refuses with the time to wait and
// never calls GitHub; after the reset it serves a token again.
func TestTokenRouteDefersWhileTheSharedBudgetIsExhaustedAndResumesAfterTheReset(t *testing.T) {
	e := newRateEnv(t)
	job := e.claim(t, e.repos[0])
	e.budget.Observe(1, ratelimit.Signal{Remaining: 0, HasRemaining: true, ResetAt: tokenT0.Add(time.Minute)})

	raw, _ := json.Marshal(queue.LeaseRequest{LeaseOwner: job.LeaseOwner})
	post := func() *http.Response {
		req, _ := http.NewRequest(http.MethodPost, e.srv.URL+strings.Replace(queue.PathToken, "{id}", job.ID, 1), bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+secret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}
	resp := post()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "60" {
		t.Fatalf("token while exhausted = %d, Retry-After %q, want 429 with 60", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if len(e.fake.Requests()) != 0 {
		t.Fatalf("GitHub was called %d times while the budget was exhausted", len(e.fake.Requests()))
	}
	if e.tally.deferred[ratelimit.ConsumerRenovateRun] != 1 {
		t.Errorf("deferrals = %v, want the run start counted once", e.tally.deferred)
	}

	e.clock.Advance(61 * time.Second)
	if resp := post(); resp.StatusCode != http.StatusOK {
		t.Fatalf("token after the reset = %d, want 200", resp.StatusCode)
	}
}

// A failed result that carries what Renovate saw of the rate limit is a rate-limited attempt: the
// job goes back to the queue no earlier than the reset, the budget of the installation is
// exhausted until then, and the attempt is an event of the job.
func TestFailedResultWithARateLimitReportReschedulesTheJobAtTheReset(t *testing.T) {
	e := newRateEnv(t)
	job := e.claim(t, e.repos[0])
	reset := tokenT0.Add(20 * time.Minute)

	status := e.result(t, job, queue.ResultRequest{
		Outcome: queue.OutcomeFailed,
		RateLimit: &queue.RateLimitReport{
			Limited: true, Remaining: i64(0), Limit: i64(5000), ResetUnix: reset.Unix(),
		},
		Run: &queue.RunRecord{ErrorCode: queue.ErrorCodeRateLimited, ErrorMessage: "GitHub's rate limit stopped the run"},
	})
	if status != http.StatusNoContent {
		t.Fatalf("result = %d, want 204", status)
	}

	state, attempts, runAfter := e.jobRow(t, job.ID)
	if state != "queued" || attempts != 1 {
		t.Fatalf("job = %s after %d attempts, want queued after 1", state, attempts)
	}
	if want := reset.Add(reset.Sub(tokenT0) / 10); !runAfter.Equal(want) {
		t.Errorf("run_after = %v, want the reset plus the 10%% jitter (%v): not before the reset", runAfter, want)
	}
	if until, exhausted := e.budget.Until(1); !exhausted || !until.Equal(reset) {
		t.Errorf("budget = %v, %v, want exhausted until the reset", until, exhausted)
	}
	if e.tally.hits != 1 || e.tally.deferred[ratelimit.ConsumerRenovateRun] != 1 {
		t.Errorf("hits %d, deferrals %v, want one of each", e.tally.hits, e.tally.deferred)
	}
	var kind string
	if err := e.db.Writer.QueryRow(`SELECT kind FROM job_events WHERE job_id = ?`, job.ID).Scan(&kind); err != nil || kind != "rate_limited" {
		t.Errorf("job_events = %q, %v, want a rate_limited event", kind, err)
	}
	if err := e.budget.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := e.db.Writer.QueryRow(`SELECT rate_limit_remaining FROM installations`).Scan(&remaining); err != nil || remaining != 0 {
		t.Errorf("the installation row holds remaining %d, %v, want 0", remaining, err)
	}

	// The queue holds the job back, however often a worker asks, until the reset has passed.
	e.clock.Advance(19 * time.Minute)
	if _, err := e.q.Claim(context.Background()); err == nil {
		t.Fatal("the job was claimed before the reset")
	}
	e.clock.Advance(5 * time.Minute)
	if again, err := e.q.Claim(context.Background()); err != nil || again.ID != job.ID || again.Attempt != 2 {
		t.Fatalf("claim after the reset = %+v, %v, want the same job at its second attempt", again, err)
	}
}

// What a worker reports is read from Renovate's log, so it is bounded and never trusted: an
// implausible reset time is dropped and a huge Retry-After cannot overflow or park a job for good.
func TestRateLimitReportsAreBounded(t *testing.T) {
	e := newRateEnv(t)
	job := e.claim(t, e.repos[0])

	status := e.result(t, job, queue.ResultRequest{
		Outcome: queue.OutcomeFailed, RetryAfterSeconds: 1 << 62,
		RateLimit: &queue.RateLimitReport{Limited: true, Remaining: i64(-5), Limit: i64(1 << 60), ResetUnix: time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC).Unix(), RetryAfterSeconds: 1 << 62},
	})
	if status != http.StatusNoContent {
		t.Fatalf("result = %d, want 204", status)
	}
	_, _, runAfter := e.jobRow(t, job.ID)
	if got := runAfter.Sub(tokenT0); got < 24*time.Hour || got > 24*time.Hour+24*time.Hour/5 {
		t.Errorf("run_after is %v after now, want the 24 hour cap of an upstream delay plus at most the 20%% jitter", got)
	}
	until, exhausted := e.budget.Until(1)
	if !exhausted || until.After(tokenT0.Add(25*time.Hour)) {
		t.Errorf("budget until %v (%v), want it held no longer than the capped delay", until, exhausted)
	}
	for _, s := range e.budget.Snapshot() {
		if s.Limit != 0 {
			t.Errorf("an absurd limit %d was recorded", s.Limit)
		}
	}
}

// A run the app refused to start carries only the rate_limited code: it gets the rate-limit
// attempts and delay but is not a new hit, and its deferral was counted at the token route.
func TestARateLimitedCodeWithoutAReportIsDeferredButNotCountedAgain(t *testing.T) {
	e := newRateEnv(t)
	job := e.claim(t, e.repos[0])

	status := e.result(t, job, queue.ResultRequest{
		Outcome: queue.OutcomeFailed, RetryAfterSeconds: 600,
		Run: &queue.RunRecord{ErrorCode: queue.ErrorCodeRateLimited, ErrorMessage: "deferred"},
	})
	if status != http.StatusNoContent {
		t.Fatalf("result = %d", status)
	}
	_, attempts, runAfter := e.jobRow(t, job.ID)
	if attempts != 1 || !runAfter.Equal(tokenT0.Add(11*time.Minute)) {
		t.Errorf("attempts %d, run_after %v, want the 10 minutes asked for plus the 10%% jitter", attempts, runAfter)
	}
	if e.tally.hits != 0 || len(e.tally.deferred) != 0 {
		t.Errorf("hits %d, deferrals %v, want nothing counted a second time", e.tally.hits, e.tally.deferred)
	}
	var kinds []string
	rows, err := e.db.Writer.Query(`SELECT kind FROM job_events WHERE job_id = ?`, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		kinds = append(kinds, k)
	}
	if len(kinds) != 1 || kinds[0] != "rate_limited" {
		t.Errorf("events = %v, want one rate_limited event", kinds)
	}
}

// A successful result still tells the budget what Renovate saw.
func TestSucceededResultUpdatesTheBudget(t *testing.T) {
	e := newRateEnv(t)
	job := e.claim(t, e.repos[0])
	status := e.result(t, job, queue.ResultRequest{
		Outcome:   queue.OutcomeSucceeded,
		RateLimit: &queue.RateLimitReport{Remaining: i64(321), Limit: i64(5000), ResetUnix: tokenT0.Add(30 * time.Minute).Unix()},
	})
	if status != http.StatusNoContent {
		t.Fatalf("result = %d", status)
	}
	snap := e.budget.Snapshot()
	if len(snap) != 1 || snap[0].InstallationID != 1 || snap[0].Remaining != 321 || snap[0].Limit != 5000 {
		t.Fatalf("budget = %+v, want installation 1 at 321 of 5000", snap)
	}
	if _, exhausted := e.budget.Until(1); exhausted {
		t.Error("a budget with requests left defers work")
	}
}
