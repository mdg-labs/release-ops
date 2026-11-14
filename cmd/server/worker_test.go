package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const testWorkerSecret = "0123456789abcdef0123456789abcdef-test-secret"

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type appSide struct {
	db    *store.DB
	q     *store.JobQueue
	clock *testClock
	url   string
	repo  string
}

// newAppSide stands in for the app: the queue over a migrated database and the internal
// listener's handler on a loopback httptest server.
func newAppSide(t *testing.T) *appSide {
	t.Helper()

	db := storetest.OpenMigrated(t)
	clock := &testClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	q := store.NewJobQueue(db, store.JobQueueOptions{Now: clock.Now, Rand: func() float64 { return 0.5 }})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: testWorkerSecret, ClaimWait: 100 * time.Millisecond}))
	t.Cleanup(srv.Close)
	return &appSide{db: db, q: q, clock: clock, url: srv.URL, repo: storetest.SeedRepositories(t, db, 1)[0]}
}

func (a *appSide) enqueue(t *testing.T) string {
	t.Helper()
	res, err := a.q.Enqueue(context.Background(), queue.EnqueueParams{
		RepositoryID: a.repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return res.JobID
}

func (a *appSide) jobState(t *testing.T, id string) (string, int) {
	t.Helper()
	var state string
	var attempts int
	if err := a.db.Writer.QueryRow(`SELECT state, attempts FROM jobs WHERE id = ?`, id).Scan(&state, &attempts); err != nil {
		t.Fatalf("read job: %v", err)
	}
	return state, attempts
}

func (a *appSide) waitState(t *testing.T, id, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if state, _ := a.jobState(t, id); state == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, attempts := a.jobState(t, id)
	t.Fatalf("job %s is %s (attempts %d), want %s", id, state, attempts, want)
}

// The production registry has no handler for renovate_run yet, so a claimed job of that kind
// fails with "no handler" (runner.invoke) instead of reporting a Renovate run that never
// happened.
func TestWorkerRegistryHasNoHandlerForRenovateRun(t *testing.T) {
	if h, ok := workerRegistry()[queue.KindRenovateRun]; ok {
		t.Fatalf("workerRegistry registers %T for %s; only the real Renovate handler may", h, queue.KindRenovateRun)
	}
}

// `release-ops worker` starts with no app secret and no database path in its environment,
// long-polls the internal listener, claims the queued job and reports the result. With no
// handler registered for renovate_run the attempt is reported failed, never succeeded.
func TestWorkerCommandReportsAClaimedRenovateRunFailed(t *testing.T) {
	app := newAppSide(t)
	id := app.enqueue(t)

	for _, name := range []string{"SESSION_SECRET", "APP_ENCRYPTION_KEY", "APP_DB_PATH"} {
		t.Setenv(name, "")
	}
	t.Setenv("INTERNAL_URL", app.url)
	t.Setenv("WORKER_SECRET", testWorkerSecret)
	metricsAddr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	t.Setenv("METRICS_ADDR", metricsAddr)

	done := make(chan error, 1)
	go func() { done <- runWorkerCommand() }()

	// The failed attempt is requeued with backoff; the fake clock does not advance, so no
	// second claim follows.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if state, attempts := app.jobState(t, id); attempts > 0 && state != queue.StateRunning {
			if state != queue.StateQueued || attempts != 1 {
				t.Fatalf("job = %s after %d attempts, want queued after 1 failed attempt", state, attempts)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker did not report the claimed job")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// The worker serves its own /metrics on METRICS_ADDR, with the failed job counted and no
	// succeeded one, and nothing else.
	metricsURL := "http://" + metricsAddr
	waitForStatus(t, metricsURL+"/metrics", http.StatusOK, false)
	deadline = time.Now().Add(15 * time.Second)
	for {
		body := getBody(t, metricsURL+"/metrics", http.StatusOK)
		if strings.Contains(body, `release_ops_job_runs_total{outcome="failed",repository="acme/repo-1"} 1`) &&
			strings.Contains(body, `release_ops_job_duration_seconds_count{repository="acme/repo-1"} 1`) {
			if strings.Contains(body, `outcome="succeeded"`) {
				t.Fatalf("the worker counted a succeeded run:\n%s", body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the worker's metrics lack the failed job:\n%s", body)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, path := range []string{"/", "/healthz", "/readyz", "/internal/v1/jobs/claim"} {
		getBody(t, metricsURL+path, http.StatusNotFound)
	}

	// SIGTERM is what stops a worker container; the command's signal context catches it.
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker command = %v, want a clean stop", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the worker command did not stop on SIGTERM")
	}
}

func TestWorkerCommandRefusesIncompleteConfig(t *testing.T) {
	t.Setenv("INTERNAL_URL", "")
	t.Setenv("WORKER_SECRET", testWorkerSecret)
	if err := runWorkerCommand(); err == nil || !strings.Contains(err.Error(), "INTERNAL_URL") {
		t.Fatalf("runWorkerCommand = %v, want INTERNAL_URL refused", err)
	}
}

// A worker killed mid-job never renews its lease and never reports: the job stays running.
// Once the lease has expired the app's reaper requeues it and a second worker runs it once;
// the dead worker's late result changes nothing.
func TestKilledWorkerJobIsReapedAndRunOnceBySecondWorker(t *testing.T) {
	app := newAppSide(t)
	id := app.enqueue(t)
	ctx := context.Background()

	// The first worker claims and is then killed: modelled by a claim nobody heartbeats.
	dead, err := app.q.Claim(ctx)
	if err != nil || dead.ID != id {
		t.Fatalf("first worker claim = %+v, %v", dead, err)
	}
	if state, _ := app.jobState(t, id); state != "running" {
		t.Fatalf("job = %s with its worker dead, want running", state)
	}
	if res, err := app.q.Reap(ctx); err != nil || res != (queue.ReapResult{}) {
		t.Fatalf("Reap with a live lease = %+v, %v", res, err)
	}

	app.clock.Advance(3 * time.Minute)
	if res, err := app.q.Reap(ctx); err != nil || res.Requeued != 1 {
		t.Fatalf("Reap after the lease expired = %+v, %v; want the job requeued", res, err)
	}
	app.clock.Advance(time.Hour)

	var runs atomic.Int32
	workerCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = runWorker(workerCtx, &config.WorkerConfig{InternalURL: app.url, WorkerSecret: testWorkerSecret}, worker.Registry{
			queue.KindRenovateRun: worker.HandlerFunc(func(_ context.Context, job queue.ClaimedJob) worker.Result {
				runs.Add(1)
				if job.Attempt != 2 {
					t.Errorf("second worker got attempt %d, want 2", job.Attempt)
				}
				return worker.Result{}
			}),
		}, worker.Options{Slots: 2, HeartbeatInterval: 10 * time.Millisecond})
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})

	app.waitState(t, id, "succeeded")
	time.Sleep(300 * time.Millisecond) // two claim loops: neither may run the job again
	if n := runs.Load(); n != 1 {
		t.Fatalf("the job ran %d times on the second worker, want once", n)
	}
	if err := app.q.Complete(ctx, id, dead.LeaseOwner); !errors.Is(err, queue.ErrLeaseLost) {
		t.Fatalf("the dead worker's late result = %v, want ErrLeaseLost", err)
	}
	if state, attempts := app.jobState(t, id); state != "succeeded" || attempts != 2 {
		t.Fatalf("job = %s after %d attempts, want succeeded after 2", state, attempts)
	}
}

// The app serves the worker routes on INTERNAL_ADDR only, behind the worker secret; the
// public Go listener does not mount them, with or without the secret.
func TestServerMountsWorkerRoutesOnTheInternalListenerOnly(t *testing.T) {
	publicPort, internalPort := freePort(t), freePort(t)
	cfg := &config.Config{
		SessionSecret:          strings.Repeat("s", 32),
		AppEncryptionKey:       "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:              filepath.Join(t.TempDir(), "app.db"),
		GoInternalPort:         publicPort,
		BootstrapAdminEmail:    "admin@example.com",
		BootstrapAdminPassword: "correct-horse-battery",
		InternalAddr:           fmt.Sprintf("127.0.0.1:%d", internalPort),
		WorkerSecret:           testWorkerSecret,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Error("server did not shut down")
		}
	})

	public := fmt.Sprintf("http://127.0.0.1:%d", publicPort)
	internal := fmt.Sprintf("http://127.0.0.1:%d", internalPort)
	waitForStatus(t, public+"/healthz", http.StatusOK, false)
	waitForStatus(t, internal+"/internal/v1/jobs/x/heartbeat", http.StatusMethodNotAllowed, true)

	post := func(url string, bearer bool) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		if bearer {
			req.Header.Set("Authorization", "Bearer "+testWorkerSecret)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", url, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(internal+"/internal/v1/jobs/x/heartbeat", false); got != http.StatusUnauthorized {
		t.Errorf("internal heartbeat without the secret = %d, want 401", got)
	}
	if got := post(internal+"/internal/v1/jobs/x/heartbeat", true); got != http.StatusOK {
		t.Errorf("internal heartbeat with the secret = %d, want 200 (an abort for the unknown job)", got)
	}
	if got := post(internal+"/api/v1/auth/login", true); got != http.StatusNotFound {
		t.Errorf("an API route on the internal listener = %d, want 404", got)
	}
	for _, path := range []string{"/internal/v1/jobs/claim", "/internal/v1/jobs/x/heartbeat", "/internal/v1/jobs/x/result", "/api/v1/internal/v1/jobs/claim"} {
		for _, bearer := range []bool{false, true} {
			if got := post(public+path, bearer); got != http.StatusNotFound {
				t.Errorf("public listener POST %s (secret %v) = %d, want 404", path, bearer, got)
			}
		}
	}
}

func waitForStatus(t *testing.T, url string, want int, bearer bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if bearer {
			req.Header.Set("Authorization", "Bearer "+testWorkerSecret)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == want {
				return
			}
			last = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			last = err
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not answer %d: %v", url, want, last)
}

func getBody(t *testing.T, url string, wantStatus int) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s = %d, want %d: %s", url, resp.StatusCode, wantStatus, body)
	}
	return string(body)
}

// The app serves /healthz as liveness, /readyz as readiness (503 until a worker has made
// contact on the internal listener) and /metrics on METRICS_ADDR only: the public Go listener
// answers 404 for it, as the /api/go proxy forwards to it, and the metrics listener serves
// nothing else.
func TestServerServesReadinessAndMetricsOnTheirListeners(t *testing.T) {
	publicPort, internalPort, metricsPort := freePort(t), freePort(t), freePort(t)
	cfg := &config.Config{
		SessionSecret:          strings.Repeat("s", 32),
		AppEncryptionKey:       "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:              filepath.Join(t.TempDir(), "app.db"),
		GoInternalPort:         publicPort,
		BootstrapAdminEmail:    "admin@example.com",
		BootstrapAdminPassword: "correct-horse-battery",
		InternalAddr:           fmt.Sprintf("127.0.0.1:%d", internalPort),
		WorkerSecret:           testWorkerSecret,
		MetricsAddr:            fmt.Sprintf("127.0.0.1:%d", metricsPort),
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Error("server did not shut down")
		}
	})

	public := fmt.Sprintf("http://127.0.0.1:%d", publicPort)
	internal := fmt.Sprintf("http://127.0.0.1:%d", internalPort)
	metricsURL := fmt.Sprintf("http://127.0.0.1:%d", metricsPort)
	waitForStatus(t, public+"/healthz", http.StatusOK, false)
	// The listeners start in their own goroutines. A GET on the internal heartbeat path is a
	// 405 from the router before any queue method runs, so the probe is not a worker contact
	// and /readyz stays 503 below.
	waitForStatus(t, internal+"/internal/v1/jobs/x/heartbeat", http.StatusMethodNotAllowed, true)
	waitForStatus(t, metricsURL+"/metrics", http.StatusOK, false)

	if body := getBody(t, public+"/readyz", http.StatusServiceUnavailable); !strings.Contains(body, "no worker has contacted the app") {
		t.Errorf("readyz before any worker = %s", body)
	}

	// A worker's heartbeat on the internal listener is a contact.
	req, _ := http.NewRequest(http.MethodPost, internal+"/internal/v1/jobs/x/heartbeat", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+testWorkerSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	_ = resp.Body.Close()
	getBody(t, public+"/readyz", http.StatusOK)

	body := getBody(t, metricsURL+"/metrics", http.StatusOK)
	for _, want := range []string{
		`release_ops_queue_depth{priority="high"} 0`,
		`release_ops_queue_depth{priority="normal"} 0`,
		"release_ops_queue_oldest_age_seconds 0",
		"release_ops_webhook_lag_seconds_count 0",
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("app metrics lack %q", want)
		}
	}
	if strings.Contains(body, cfg.WorkerSecret) || strings.Contains(body, cfg.SessionSecret) || strings.Contains(body, cfg.AppEncryptionKey) {
		t.Error("the metrics output carries a secret")
	}
	for _, path := range []string{"/", "/healthz", "/readyz", "/api/v1/status"} {
		getBody(t, metricsURL+path, http.StatusNotFound)
	}
	getBody(t, public+"/metrics", http.StatusNotFound)
	// The internal listener authenticates every request first; with the secret, /metrics is
	// still no route there.
	req, _ = http.NewRequest(http.MethodGet, internal+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testWorkerSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET internal /metrics: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("internal listener GET /metrics with the secret = %d, want 404", resp.StatusCode)
	}
}

// A METRICS_ADDR the app cannot bind stops it with an error, not silently without metrics.
func TestServerFailsWhenTheMetricsListenerCannotBind(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()

	cfg := &config.Config{
		SessionSecret:          strings.Repeat("s", 32),
		AppEncryptionKey:       "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:              filepath.Join(t.TempDir(), "app.db"),
		GoInternalPort:         freePort(t),
		BootstrapAdminEmail:    "admin@example.com",
		BootstrapAdminPassword: "correct-horse-battery",
		MetricsAddr:            busy.Addr().String(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := run(ctx, cfg); err == nil || !strings.Contains(err.Error(), "metrics listener") {
		t.Fatalf("run = %v, want a metrics listener error", err)
	}
}
