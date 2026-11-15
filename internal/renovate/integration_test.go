package renovate_test

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/renovate"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const integrationSecret = "integration-worker-secret-0123456789abcdef"

type appSide struct {
	db    *store.DB
	q     *store.JobQueue
	fake  *githubtest.Fake
	srv   *httptest.Server
	repos []string
}

// newAppSide is the app: the real queue and App store, the real token minting against a fake
// GitHub and the real internal listener's handler.
func newAppSide(t *testing.T) *appSide {
	t.Helper()
	db := storetest.OpenMigrated(t)
	clock := githubtest.NewClock(time.Now())
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
	if _, err := db.Writer.Exec(`UPDATE github_app SET bot_user_id = 424242`); err != nil {
		t.Fatal(err)
	}

	q := store.NewJobQueue(db, store.JobQueueOptions{Rand: func() float64 { return 0.5 }})
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), st.GitHubApp(), github.AppClientOptions{Now: clock.Now})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{
		Secret: integrationSecret, ClaimWait: 100 * time.Millisecond, Tokens: app,
		TokenRepositories: []string{"acme/renovate-presets"},
	}))
	t.Cleanup(srv.Close)
	return &appSide{db: db, q: q, fake: fake, srv: srv, repos: storetest.SeedRepositories(t, db, 2)}
}

func (a *appSide) enqueue(t *testing.T, repo string) string {
	t.Helper()
	res, err := a.q.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerRunNow})
	if err != nil {
		t.Fatal(err)
	}
	return res.JobID
}

type runRow struct {
	Attempt                               int
	Outcome                               string
	ExitCode                              sql.NullInt64
	ErrorCode, ErrorMessage, ReportStatus sql.NullString
	LogExcerpt, Report                    sql.NullString
}

func (a *appSide) runs(t *testing.T, jobID string) []runRow {
	t.Helper()
	rows, err := a.db.Reader.Query(`SELECT attempt, outcome, exit_code, error_code, error_message, report_status, log_excerpt, report FROM job_runs WHERE job_id = ? ORDER BY attempt`, jobID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []runRow
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.Attempt, &r.Outcome, &r.ExitCode, &r.ErrorCode, &r.ErrorMessage, &r.ReportStatus, &r.LogExcerpt, &r.Report); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (a *appSide) waitFor(t *testing.T, jobID string, states ...string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var state string
		if err := a.db.Reader.QueryRow(`SELECT state FROM jobs WHERE id = ?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		var attempts int
		_ = a.db.Reader.QueryRow(`SELECT attempts FROM jobs WHERE id = ?`, jobID).Scan(&attempts)
		if slices.Contains(states, state) && attempts > 0 {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s is %s (attempts %d), want one of %v", jobID, state, attempts, states)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// runWorker starts the worker over the Renovate handler and stops it with the test.
func runWorker(t *testing.T, app *appSide, e *env) *worker.Client {
	t.Helper()
	client := worker.NewClient(app.srv.URL, integrationSecret, nil)
	h, err := renovate.NewHandler(renovate.Options{
		Tokens: client, GitHubAPIURL: "https://api.github.example", SameUser: true, WorkerSecret: integrationSecret,
		Binary: e.binary, CacheRoot: e.cacheRoot, ConfigMount: e.cfgMount, ToolsMount: filepath.Join(e.out, "no-tools"),
		Logger: slog.New(slog.NewTextHandler(e.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runner := worker.NewRunner(client, worker.Registry{queue.KindRenovateRun: h}, worker.Options{
		Slots: 1, HeartbeatInterval: 50 * time.Millisecond, ShutdownGrace: time.Second, RetryDelay: 50 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(e.logs, nil)),
	})
	go func() { defer close(done); runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return client
}

// The worker claims a job, takes its token from the app, runs the fake Renovate and the app
// records the run in job_runs: exit code, log excerpt and report, without the token.
func TestWorkerRunsAClaimedJobAndTheAppRecordsTheRun(t *testing.T) {
	app := newAppSide(t)
	e := newEnv(t, `
env > "$OUT/env"
echo "log line with $RENOVATE_TOKEN in it"
echo "stderr line" >&2
printf '{"repositories":{"acme/repo-1":{"token":"%s"}}}' "$RENOVATE_TOKEN" > "$RENOVATE_REPORT_PATH"`)
	runWorker(t, app, e)

	id := app.enqueue(t, app.repos[0])
	if state := app.waitFor(t, id, queue.StateSucceeded); state != queue.StateSucceeded {
		t.Fatalf("job is %s", state)
	}

	runs := app.runs(t, id)
	if len(runs) != 1 {
		t.Fatalf("job_runs = %+v, want one row", runs)
	}
	r := runs[0]
	if r.Attempt != 1 || r.Outcome != "succeeded" || !r.ExitCode.Valid || r.ExitCode.Int64 != 0 || r.ReportStatus.String != "ok" || r.ErrorCode.Valid {
		t.Errorf("run = %+v", r)
	}
	if !strings.Contains(r.LogExcerpt.String, "log line with [REDACTED] in it") || !strings.Contains(r.LogExcerpt.String, "stderr line") {
		t.Errorf("log excerpt = %q", r.LogExcerpt.String)
	}
	if !strings.Contains(r.Report.String, `"acme/repo-1"`) {
		t.Errorf("report = %q", r.Report.String)
	}

	// The token the child was given is the one the app minted for the job's repository and the
	// configured preset repository, and it is stored nowhere.
	envDump := e.read("env")
	var token string
	for _, line := range strings.Split(envDump, "\n") {
		if v, ok := strings.CutPrefix(line, "RENOVATE_TOKEN="); ok {
			token = v
		}
	}
	if !strings.HasPrefix(token, githubtest.TokenPrefix) {
		t.Fatalf("RENOVATE_TOKEN = %q: not a token minted by the app", token)
	}
	if reqs := app.fake.TokenRequests(); len(reqs) != 1 || !slices.Equal(reqs[0], []string{"repo-1", "renovate-presets"}) {
		t.Errorf("token requests = %v", reqs)
	}
	if !strings.Contains(envDump, "RENOVATE_GIT_AUTHOR=release-ops-acme[bot] <424242+release-ops-acme[bot]@users.noreply.github.com>\n") {
		t.Errorf("the git author did not come with the claimed job:\n%s", envDump)
	}
	for _, table := range []string{"job_runs", "job_events", "jobs"} {
		rows, err := app.db.Reader.Query("SELECT * FROM " + table)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			for i, v := range vals {
				if s, ok := v.(string); ok && (strings.Contains(s, token) || strings.Contains(s, integrationSecret)) {
					t.Errorf("a secret is stored in %s.%s", table, cols[i])
				}
			}
		}
		_ = rows.Close()
	}
	if strings.Contains(e.logs.String(), token) {
		t.Error("the token is in the worker's logs")
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

func TestWorkerRecordsAFailedRunAndTheAppRequeuesTheJob(t *testing.T) {
	app := newAppSide(t)
	e := newEnv(t, `echo "died with $RENOVATE_TOKEN"; exit 2`)
	runWorker(t, app, e)

	id := app.enqueue(t, app.repos[0])
	if state := app.waitFor(t, id, queue.StateQueued); state != queue.StateQueued {
		t.Fatalf("job is %s", state)
	}
	runs := app.runs(t, id)
	if len(runs) < 1 {
		t.Fatal("no job_runs row for the failed attempt")
	}
	r := runs[0]
	if r.Outcome != "failed" || r.ExitCode.Int64 != 2 || r.ErrorCode.String != renovate.CodeRenovateExit || r.ErrorMessage.String != "renovate exited with status 2" {
		t.Errorf("run = %+v", r)
	}
	if strings.Contains(r.LogExcerpt.String, githubtest.TokenPrefix) {
		t.Errorf("the token is in the stored log: %q", r.LogExcerpt.String)
	}
}

// The app refuses a token for a job the worker has not claimed, and GitHub is not asked.
func TestTheAppRefusesATokenForAJobTheWorkerHasNotClaimed(t *testing.T) {
	app := newAppSide(t)
	queued := app.enqueue(t, app.repos[1])
	client := worker.NewClient(app.srv.URL, integrationSecret, nil)

	for name, c := range map[string]struct{ id, owner string }{
		"a queued job":   {queued, "0.0123456789abcdef"},
		"an unknown job": {"no-such-job", "0.0123456789abcdef"},
		"no lease owner": {queued, ""},
	} {
		if _, err := client.Token(context.Background(), c.id, c.owner); !errors.Is(err, worker.ErrLeaseLost) {
			t.Errorf("%s: err = %v, want ErrLeaseLost", name, err)
		}
	}
	if n := len(app.fake.TokenRequests()); n != 0 {
		t.Errorf("GitHub saw %d token requests", n)
	}
}

func TestAnUnreachableTokenEndpointFailsTheJob(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`)
	srv := httptest.NewServer(nil)
	url := srv.URL
	srv.Close()
	h, err := renovate.NewHandler(renovate.Options{
		Tokens: worker.NewClient(url, integrationSecret, nil), GitHubAPIURL: "https://api.github.example", SameUser: true,
		WorkerSecret: integrationSecret, Binary: e.binary, CacheRoot: e.cacheRoot, ConfigMount: e.cfgMount, Logger: slog.New(slog.NewTextHandler(e.logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	res := h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeTokenUnavailable)
	if strings.Contains(res.Err.Error(), url) || strings.Contains(res.Err.Error(), "127.0.0.1") {
		t.Errorf("the reason echoes the endpoint: %q", res.Err)
	}
	if _, err := os.Stat(filepath.Join(e.out, "started")); err == nil {
		t.Error("Renovate ran")
	}
}
