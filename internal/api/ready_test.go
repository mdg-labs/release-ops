package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type readyClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *readyClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *readyClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func getReady(t *testing.T, h http.Handler) (int, readyResponse, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body readyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, body, rec.Body.String()
}

// /readyz is 200 only while the database answers and a worker has been heard from inside the
// window; each failure is a 503 with a reason that names the check and nothing else.
func TestReadyzFlipsOnADeadDatabaseAndAStaleWorker(t *testing.T) {
	db := storetest.OpenMigrated(t)
	clock := &readyClock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	jobs := store.NewJobQueue(db, store.JobQueueOptions{Now: clock.Now})

	deps := newRoutesTestDeps(t)
	deps.DB = db.Writer
	deps.Queue = jobs
	router := NewServerRouter(deps)
	ready := Ready(db.Writer, jobs, clock.Now)

	if code, body, _ := getReady(t, ready); code != http.StatusServiceUnavailable || body.Reason != "no worker has contacted the app" {
		t.Fatalf("before any worker contact: %d %+v, want 503 no worker", code, body)
	}

	if _, err := jobs.Claim(t.Context()); err != nil && err != queue.ErrNoJob {
		t.Fatalf("Claim: %v", err)
	}
	if code, body, _ := getReady(t, ready); code != http.StatusOK || body.Status != "ok" {
		t.Fatalf("after a claim poll: %d %+v, want 200 ok", code, body)
	}

	clock.Advance(queue.WorkerContactWindow + time.Second)
	if code, body, _ := getReady(t, ready); code != http.StatusServiceUnavailable || body.Reason != "worker contact is stale" {
		t.Fatalf("stale heartbeat: %d %+v, want 503 stale", code, body)
	}

	if _, err := jobs.Claim(t.Context()); err != nil && err != queue.ErrNoJob {
		t.Fatalf("Claim: %v", err)
	}
	if code, _, _ := getReady(t, ready); code != http.StatusOK {
		t.Fatalf("after a fresh contact: %d, want 200", code)
	}

	if err := db.Writer.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	code, body, raw := getReady(t, ready)
	if code != http.StatusServiceUnavailable || body.Reason != "database unavailable" {
		t.Fatalf("closed database: %d %+v, want 503 database unavailable", code, body)
	}
	if strings.Contains(raw, "app.db") || strings.Contains(raw, "sql:") {
		t.Errorf("reason leaks an error text or a path: %s", raw)
	}

	// The production router serves the same handler on GET /readyz.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("router GET /readyz with a closed database = %d, want 503", rec.Code)
	}
}

func TestReadyzWithoutAQueueIsNotReady(t *testing.T) {
	db := storetest.OpenMigrated(t)
	code, body, _ := getReady(t, Ready(db.Writer, nil, time.Now))
	if code != http.StatusServiceUnavailable || body.Reason != "queue unavailable" {
		t.Fatalf("no queue: %d %+v, want 503 queue unavailable", code, body)
	}
	code, body, _ = getReady(t, Ready(nil, nil, time.Now))
	if code != http.StatusServiceUnavailable || body.Reason != "database unavailable" {
		t.Fatalf("no database: %d %+v, want 503 database unavailable", code, body)
	}
}

// The router serves /readyz and /healthz but never /metrics: metrics live on METRICS_ADDR, so
// /api/go/metrics, which the proxy forwards to this router, is a 404.
func TestRouterServesReadyzAndNoMetrics(t *testing.T) {
	deps := newRoutesTestDeps(t)
	router := NewServerRouter(deps)

	for path, want := range map[string]int{
		"/healthz":        http.StatusOK,
		"/readyz":         http.StatusServiceUnavailable,
		"/metrics":        http.StatusNotFound,
		"/api/v1/metrics": http.StatusNotFound,
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d", path, rec.Code, want)
		}
	}
}
