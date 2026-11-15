package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	prBotID   int64 = 424242
	prBody          = "Updates.\n\n - [ ] <!-- rebase-check -->rebase\n\n<!--renovate-debug:eyJ4IjoxfQ==-->\n"
	prMetrics       = `release_ops_open_update_prs{repository="acme/repo-1"}`
)

type prServer struct {
	fake     *githubtest.Fake
	db       *store.DB
	repo     string
	internal string
	metrics  string
}

func prAt(minutes int) time.Time {
	return time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC).Add(time.Duration(minutes) * time.Minute)
}

func (s *prServer) botPR(number int64, mutate ...func(*githubtest.PullRequest)) {
	pr := githubtest.PullRequest{
		Number: number, Title: "chore(deps): update", Body: prBody, AuthorID: prBotID,
		HeadRef: "renovate/dep", HeadSHA: "abc", State: "open", CreatedAt: prAt(0), UpdatedAt: prAt(1),
	}
	for _, m := range mutate {
		m(&pr)
	}
	s.fake.SetPullRequest("acme/repo-1", pr)
}

func (s *prServer) states(t *testing.T) string {
	t.Helper()
	rows, err := s.db.Reader.Query(`SELECT number || ':' || state FROM update_prs ORDER BY number`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func (s *prServer) waitStates(t *testing.T, what, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for s.states(t) != want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s: update_prs = %q, want %q", what, s.states(t), want)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// startPRServer starts the real server on a database that already holds an installation, one
// repository (acme/repo-1) and a stale open row for PR 9, which GitHub reports merged. Each seed
// runs on the database before the server starts.
func startPRServer(t *testing.T, seeds ...func(t *testing.T, st *store.Store)) *prServer {
	t.Helper()
	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1, githubtest.AppID)
	fake.SetRepositories(1, githubtest.Repo{ID: 1, FullName: "acme/repo-1", DefaultBranch: "main"})
	fake.SetFiles("acme/repo-1", "renovate.json")
	target, err := url.Parse(fake.URL)
	if err != nil {
		t.Fatal(err)
	}
	github := httptest.NewServer(http.StripPrefix("/api/v3", httputil.NewSingleHostReverseProxy(target)))
	t.Cleanup(github.Close)

	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := storetest.SeedRepositories(t, db, 1)[0]
	cipher, err := crypto.NewCipherFromHex("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	app := store.New(db, cipher).GitHubApp()
	for _, seed := range seeds {
		seed(t, store.New(db, cipher))
	}
	if err := app.Create(context.Background(), store.NewGitHubApp{
		AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "ws",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordBotUserID(context.Background(), prBotID); err != nil {
		t.Fatal(err)
	}
	const old = "2026-11-15T09:00:00.000Z"
	if _, err := db.Writer.Exec(`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		VALUES ('stale', ?, 9, 'n', 'u', 't', 'h', 'open', ?, ?, ?)`, repo, old, old, old); err != nil {
		t.Fatal(err)
	}

	s := &prServer{fake: fake, db: db, repo: repo}
	s.botPR(9, func(p *githubtest.PullRequest) {
		p.State, p.Merged, p.MergedAt, p.ClosedAt, p.UpdatedAt = "closed", true, prAt(5), prAt(5), prAt(5)
	})
	s.botPR(1)
	s.botPR(2)
	s.botPR(3, func(p *githubtest.PullRequest) { p.AuthorID = 1 }) // a person's PR with the bot's body

	var public string
	withFreePorts(t, 3, func(ports []int) error {
		cfg := &config.Config{
			SessionSecret:    strings.Repeat("s", 32),
			AppEncryptionKey: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
			AppDBPath:        dbPath,
			AppURL:           "https://release-ops.example.com",
			GitHubBaseURL:    github.URL,
			GoInternalPort:   ports[0],
			InternalAddr:     fmt.Sprintf("127.0.0.1:%d", ports[1]),
			WorkerSecret:     testWorkerSecret,
			MetricsAddr:      fmt.Sprintf("127.0.0.1:%d", ports[2]),
		}
		public = fmt.Sprintf("http://127.0.0.1:%d", ports[0])
		s.internal = fmt.Sprintf("http://127.0.0.1:%d", ports[1])
		s.metrics = fmt.Sprintf("http://127.0.0.1:%d", ports[2])
		return launch(t, func(ctx context.Context) error { return run(ctx, cfg) },
			probe{url: public + "/healthz", status: http.StatusOK},
			probe{url: s.internal + "/internal/v1/jobs/x/heartbeat", status: http.StatusMethodNotAllowed, bearer: true},
			probe{url: s.metrics + "/metrics", status: http.StatusOK})
	})
	return s
}

func (s *prServer) gauge(t *testing.T) string {
	t.Helper()
	for _, line := range strings.Split(getBody(t, s.metrics+"/metrics", http.StatusOK), "\n") {
		if strings.HasPrefix(line, prMetrics+" ") {
			return strings.TrimPrefix(line, prMetrics+" ")
		}
	}
	return ""
}

// At start the server reconciles every repository: the PRs GitHub lists are stored, a stored
// open PR GitHub no longer lists is set to what GitHub reports, a person's PR is not stored, and
// the open-PR gauge equals the open rows. After a job result the repository is reconciled again
// without waiting for a tick.
func TestServerReconcilesUpdatePRsAtStartAndAfterAJobResult(t *testing.T) {
	s := startPRServer(t)
	s.waitStates(t, "the reconcile at start", "1:open 2:open 9:merged")
	if got := s.gauge(t); got != "2" {
		t.Fatalf("%s = %q, want 2", prMetrics, got)
	}

	// Renovate opens PR 4 and the next job reports its result.
	s.botPR(4)
	jobs := store.NewJobQueue(s.db, store.JobQueueOptions{})
	if _, err := jobs.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: s.repo, Kind: queue.KindRenovateRun, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush}); err != nil {
		t.Fatal(err)
	}
	w := worker.NewClient(s.internal, testWorkerSecret, nil)
	job, err := w.Claim(context.Background())
	if err != nil || job == nil {
		t.Fatalf("claim = %v, %v", job, err)
	}
	if err := w.Result(context.Background(), job.ID, queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: queue.OutcomeSucceeded}); err != nil {
		t.Fatalf("result: %v", err)
	}
	s.waitStates(t, "the reconcile after the result", "1:open 2:open 4:open 9:merged")
	deadline := time.Now().Add(20 * time.Second)
	for s.gauge(t) != "3" {
		if time.Now().After(deadline) {
			t.Fatalf("%s = %q, want 3", prMetrics, s.gauge(t))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// The reconcile also runs on every tick of the safety net.
func TestServerReconcilesUpdatePRsOnEverySchedulerTick(t *testing.T) {
	interval := schedulerInterval
	t.Cleanup(func() { schedulerInterval = interval })
	schedulerInterval = 100 * time.Millisecond

	s := startPRServer(t)
	s.waitStates(t, "the first reconciles", "1:open 2:open 9:merged")
	s.botPR(5)
	s.botPR(1, func(p *githubtest.PullRequest) {
		p.State, p.ClosedAt, p.UpdatedAt = "closed", prAt(9), prAt(9)
	})
	s.waitStates(t, "a tick's reconcile", "1:closed 2:open 5:open 9:merged")
}
