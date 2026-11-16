package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	renovateWorkerSecret = "0123456789abcdef0123456789abcdef-worker-secret"
	renovateBotUserID    = 99887766
	// latencyBound is the spec's target from webhook arrival to the job being started
	// (specs.html 2.9).
	latencyBound = 10 * time.Second
)

// renovateFlow is the app as the server wires it: the real router with the webhook route and
// the installation handlers, and the internal listener a worker claims through.
type renovateFlow struct {
	*webhookFlow
	jobs    *store.JobQueue
	worker  *worker.Client
	started chan time.Duration
}

func newRenovateFlow(t *testing.T, claimWait time.Duration) *renovateFlow {
	t.Helper()
	return newRenovateFlowWith(t, claimWait, nil)
}

// newRenovateFlowWith is newRenovateFlow with a hook to tune the queue and the internal listener
// before they are built, which the rate-limit tests use for a fake clock and the shared budget.
func newRenovateFlowWith(t *testing.T, claimWait time.Duration, tune func(*ServerDeps, *store.JobQueueOptions, *internalapi.Options)) *renovateFlow {
	t.Helper()

	deps := newRoutesTestDeps(t)
	f := &renovateFlow{started: make(chan time.Duration, 8)}
	queueOpts := store.JobQueueOptions{OnJobStarted: func(lag time.Duration) { f.started <- lag }}
	apiOpts := internalapi.Options{Secret: renovateWorkerSecret, ClaimWait: claimWait}
	if tune != nil {
		tune(deps, &queueOpts, &apiOpts)
	}
	f.jobs = store.NewJobQueue(deps.Store.DB(), queueOpts)
	deps.Queue = f.jobs
	deps.WebhookURL = "https://release-ops.example.com/hooks/github"
	routes := hooks.NewRoutes()
	hooks.RegisterInstallationHandlers(routes, deps.Store.InstallationSync(), nil)
	deps.WebhookRoutes = routes

	if err := deps.Store.GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: 1234567, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "client-secret-value", WebhookSecret: webhookTestSecret,
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}
	if _, err := deps.Store.GitHubApp().RecordBotUserID(context.Background(), renovateBotUserID); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	f.webhookFlow = &webhookFlow{srv: srv, deps: deps}

	internal := httptest.NewServer(internalapi.NewHandler(f.jobs, apiOpts))
	t.Cleanup(internal.Close)
	f.worker = worker.NewClient(internal.URL, renovateWorkerSecret, nil)

	// The first repository comes from the installation sync, as in production.
	if status, _ := f.post(t, signedHook("installation", "seed", fixture(t, "installation_created.json"))); status != http.StatusAccepted {
		t.Fatalf("seed installation = %d, want 202", status)
	}
	return f
}

// seedRepository adds a repository of the recorded installation and returns its id, which
// is its full name.
func (f *renovateFlow) seedRepository(t *testing.T, githubID int64, fullName string) string {
	t.Helper()
	const at = "2026-11-14T12:00:00.000Z"
	if _, err := f.deps.DB.Exec(
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 SELECT ?, id, ?, ?, 'main', ?, ? FROM installations LIMIT 1`, fullName, githubID, fullName, at, at,
	); err != nil {
		t.Fatalf("seed repository %d: %v", githubID, err)
	}
	return fullName
}

func (f *renovateFlow) jobStates(t *testing.T) []string {
	t.Helper()
	rows, err := f.deps.DB.Query(`SELECT j."trigger" || '/' || j.priority || '/' || j.state FROM jobs j ORDER BY j.created_at, j.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s sql.NullString
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s.String)
	}
	return out
}

func TestWebhookFixturesReachAQueuedHighPriorityJobThroughThePublicRoute(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		event, file, want string
	}{
		{"issues", "issues_edited_dashboard_tick.json", "dashboard_checkbox/high/queued"},
		{"pull_request", "pull_request_edited_rebase_tick.json", "pr_checkbox/high/queued"},
		{"pull_request", "pull_request_closed_merged.json", "pr_closed/high/queued"},
		{"push", "push_default_branch.json", "push/high/queued"},
		{"installation_repositories", "installation_repositories_added.json", "repository_added/high/queued"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			f := newRenovateFlow(t, time.Second)
			status, _ := f.post(t, signedHook(tc.event, "g-1", fixture(t, tc.file)))
			if status != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", status)
			}
			if got := f.jobStates(t); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("jobs = %v, want [%s]", got, tc.want)
			}
		})
	}
}

func TestWebhookDuplicateDeliveryThroughTheRouteQueuesOneJob(t *testing.T) {
	t.Parallel()
	f := newRenovateFlow(t, time.Second)

	h := signedHook("issues", "g-dup", fixture(t, "issues_edited_dashboard_tick.json"))
	for range 2 {
		if status, _ := f.post(t, h); status != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", status)
		}
	}
	job, err := f.worker.Claim(context.Background())
	if err != nil || job == nil {
		t.Fatalf("Claim = %+v, %v", job, err)
	}
	if err := f.jobs.Complete(context.Background(), job.ID, job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	if status, _ := f.post(t, h); status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	if got := f.jobStates(t); len(got) != 1 {
		t.Fatalf("jobs = %v, want one: the redelivery of a finished job's trigger starts nothing", got)
	}
}

func TestAWebhookJobIsClaimedInTheSlotReservedForHighPriorityWhileScheduledJobsFillTheRest(t *testing.T) {
	t.Parallel()
	f := newRenovateFlow(t, 300*time.Millisecond)
	ctx := context.Background()
	a := f.seedRepository(t, 7101, "acme/scheduled-a")
	b := f.seedRepository(t, 7102, "acme/scheduled-b")
	for _, repo := range []string{a, b} {
		if _, err := f.jobs.Enqueue(ctx, queue.EnqueueParams{RepositoryID: repo, Priority: queue.PriorityNormal, Trigger: queue.TriggerSchedule}); err != nil {
			t.Fatal(err)
		}
	}

	// The default cap is two slots, one of them reserved: the first scheduled job takes the
	// other and keeps it busy (it is never completed, as a long Renovate run would not be).
	busy, err := f.worker.Claim(ctx)
	if err != nil || busy == nil || busy.Priority != queue.PriorityNormal {
		t.Fatalf("first claim = %+v, %v, want a scheduled job", busy, err)
	}
	if job, err := f.worker.Claim(ctx); err != nil || job != nil {
		t.Fatalf("second claim = %+v, %v, want nothing: the other scheduled job waits for the reserved slot's owner", job, err)
	}

	if status, _ := f.post(t, signedHook("issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json"))); status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	job, err := f.worker.Claim(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim after the webhook = %+v, %v, want the webhook job", job, err)
	}
	if job.Priority != queue.PriorityHigh || job.Trigger != queue.TriggerDashboardCheckbox || job.RepositoryFullName != "acme/widgets" || job.Slot == busy.Slot {
		t.Errorf("claimed %+v, want the high-priority dashboard job of acme/widgets in the slot the scheduled job does not hold", job)
	}
	if again, err := f.worker.Claim(ctx); err != nil || again != nil {
		t.Errorf("claim with both slots busy = %+v, %v, want nothing", again, err)
	}
}

func TestAWebhookJobIsStartedWithinTenSecondsAcrossTheAppAndTheWorkerClient(t *testing.T) {
	t.Parallel()
	f := newRenovateFlow(t, internalapi.DefaultClaimWait)
	ctx, cancel := context.WithTimeout(context.Background(), 2*latencyBound)
	defer cancel()

	type claim struct {
		job *queue.ClaimedJob
		at  time.Time
		err error
	}
	claimed := make(chan claim, 1)
	go func() {
		job, err := f.worker.Claim(ctx)
		claimed <- claim{job, time.Now(), err}
	}()
	// Wait until the worker's long-poll is parked on the internal listener.
	for deadline := time.Now().Add(5 * time.Second); ; {
		if _, ok, _ := f.jobs.LastWorkerContact(ctx); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker's claim never reached the app")
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)

	entered := time.Now()
	if status, _ := f.post(t, signedHook("issues", "g-1", fixture(t, "issues_edited_dashboard_tick.json"))); status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
	got := <-claimed
	if got.err != nil || got.job == nil {
		t.Fatalf("claim = %+v, %v, want the webhook job: the enqueue did not wake the waiting claim", got.job, got.err)
	}
	claimLatency := got.at.Sub(entered)

	if _, err := f.worker.Heartbeat(ctx, got.job.ID, got.job.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	var startLag time.Duration
	select {
	case startLag = <-f.started:
	case <-time.After(time.Second):
		t.Fatal("the app recorded no webhook-to-start lag")
	}

	t.Logf("webhook entry to claim: %v; webhook arrival to first heartbeat (stored on the job): %v", claimLatency, startLag)
	if claimLatency > latencyBound || startLag > latencyBound {
		t.Errorf("claim after %v and start after %v, want both within %v", claimLatency, startLag, latencyBound)
	}
}
