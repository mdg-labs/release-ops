package metrics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

type statsQueue struct {
	queue.Queue
	stats queue.Stats
	err   error
}

func (q statsQueue) Stats(context.Context) (queue.Stats, error) { return q.stats, q.err }

func scrape(t *testing.T, r *Registry) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Code, rec.Body.String()
}

func TestMetricsServesTheRegisteredNames(t *testing.T) {
	reg := NewRegistry()
	jobs, err := NewJobs(reg)
	if err != nil {
		t.Fatal(err)
	}
	lag, err := NewWebhookLag(reg)
	if err != nil {
		t.Fatal(err)
	}
	q := statsQueue{stats: queue.Stats{
		Depth:     map[queue.Priority]int{queue.PriorityHigh: 2, queue.PriorityNormal: 5},
		OldestAge: 42 * time.Second,
	}}
	if err := RegisterQueue(reg, q); err != nil {
		t.Fatal(err)
	}

	jobs.Ended("acme/api", OutcomeSucceeded)
	jobs.Ended("acme/api", OutcomeFailed)
	jobs.Ended("acme/web", OutcomeLeaseExpired)
	jobs.Took("acme/api", 3*time.Second)
	lag.Observe(1.5)

	code, body := scrape(t, reg)
	if code != http.StatusOK {
		t.Fatalf("scrape = %d: %s", code, body)
	}
	for _, want := range []string{
		`release_ops_job_runs_total{outcome="succeeded",repository="acme/api"} 1`,
		`release_ops_job_runs_total{outcome="failed",repository="acme/api"} 1`,
		`release_ops_job_runs_total{outcome="lease_expired",repository="acme/web"} 1`,
		`release_ops_job_failures_total{repository="acme/api"} 1`,
		`release_ops_job_failures_total{repository="acme/web"} 1`,
		`release_ops_job_duration_seconds_count{repository="acme/api"} 1`,
		`release_ops_queue_depth{priority="high"} 2`,
		`release_ops_queue_depth{priority="normal"} 5`,
		`release_ops_queue_oldest_age_seconds 42`,
		`release_ops_webhook_lag_seconds_count 1`,
		"go_goroutines",
		"process_cpu_seconds_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output lacks %q", want)
		}
	}
}

// A queue that cannot be read fails the scrape instead of serving a stale or empty depth.
func TestMetricsScrapeFailsWhenTheQueueCannotBeRead(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterQueue(reg, statsQueue{err: errors.New("database is closed")}); err != nil {
		t.Fatal(err)
	}
	code, body := scrape(t, reg)
	if code != http.StatusInternalServerError {
		t.Fatalf("scrape = %d, want 500: %s", code, body)
	}
}

func TestNilJobsRecordsNothing(t *testing.T) {
	var j *Jobs
	j.Ended("acme/api", OutcomeFailed)
	j.Took("acme/api", time.Second)
}

func TestRegisteringANameTwiceIsAnError(t *testing.T) {
	reg := NewRegistry()
	if _, err := NewJobs(reg); err != nil {
		t.Fatal(err)
	}
	if _, err := NewJobs(reg); err == nil {
		t.Fatal("a second NewJobs on the same registry succeeded")
	}
}

// The listener serves /metrics and nothing else.
func TestListenerServesOnlyMetrics(t *testing.T) {
	reg := NewRegistry()
	if _, err := NewJobs(reg); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, ln, reg) }()
	base := "http://" + ln.Addr().String()

	get := func(method, path string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	if code, body := get(http.MethodGet, "/metrics"); code != http.StatusOK || !strings.Contains(body, "go_goroutines") {
		t.Errorf("GET /metrics = %d", code)
	}
	for _, path := range []string{"/", "/healthz", "/readyz", "/api/v1/status", "/metrics/x"} {
		if code, _ := get(http.MethodGet, path); code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
	if code, _ := get(http.MethodPost, "/metrics"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST /metrics = %d, want 405", code)
	}

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve = %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the listener did not shut down")
	}
}

func TestServeRefusesABusyAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	if err := Serve(context.Background(), ln.Addr().String(), NewRegistry()); err == nil {
		t.Fatal("Serve on a busy address succeeded")
	}
}

type openCounts struct {
	counts map[string]int
	err    error
}

func (o openCounts) OpenCounts(context.Context) (map[string]int, error) { return o.counts, o.err }

// The gauge is read at each scrape, so it follows the counts, labelled by repository name only.
func TestOpenUpdatePRsGaugeFollowsTheCountsPerRepository(t *testing.T) {
	reg := NewRegistry()
	counts := &openCounts{counts: map[string]int{"acme/api": 2, "acme/web": 0}}
	if err := RegisterOpenUpdatePRs(reg, counts); err != nil {
		t.Fatal(err)
	}
	_, body := scrape(t, reg)
	for _, want := range []string{`release_ops_open_update_prs{repository="acme/api"} 2`, `release_ops_open_update_prs{repository="acme/web"} 0`} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output lacks %q:\n%s", want, body)
		}
	}
	counts.counts = map[string]int{"acme/api": 1}
	_, body = scrape(t, reg)
	if !strings.Contains(body, `release_ops_open_update_prs{repository="acme/api"} 1`) || strings.Contains(body, `repository="acme/web"`) {
		t.Errorf("the gauge did not follow the counts:\n%s", body)
	}
}

func TestOpenUpdatePRsScrapeFailsWhenTheCountsCannotBeRead(t *testing.T) {
	reg := NewRegistry()
	if err := RegisterOpenUpdatePRs(reg, openCounts{err: errors.New("database is closed")}); err != nil {
		t.Fatal(err)
	}
	if code, body := scrape(t, reg); code != http.StatusInternalServerError {
		t.Fatalf("scrape = %d, want 500: %s", code, body)
	}
}
