package internalapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const secret = "0123456789abcdef0123456789abcdef-test-secret"

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

type env struct {
	srv   *httptest.Server
	q     *store.JobQueue
	repos []string
}

func newEnv(t *testing.T, claimWait time.Duration) *env {
	t.Helper()

	db := storetest.OpenMigrated(t)
	c := &clock{now: time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)}
	q := store.NewJobQueue(db, store.JobQueueOptions{Now: c.Now})
	srv := httptest.NewServer(internalapi.NewHandler(q, internalapi.Options{Secret: secret, ClaimWait: claimWait}))
	t.Cleanup(srv.Close)
	return &env{srv: srv, q: q, repos: storetest.SeedRepositories(t, db, 2)}
}

func (e *env) post(t *testing.T, path, auth string, body any) (int, []byte) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, e.srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (e *env) enqueue(t *testing.T, repo string) {
	t.Helper()
	if _, err := e.q.Enqueue(context.Background(), queue.EnqueueParams{
		RepositoryID: repo, Priority: queue.PriorityHigh, Trigger: queue.TriggerPush,
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
}

func TestEveryRequestNeedsTheWorkerSecret(t *testing.T) {
	e := newEnv(t, 50*time.Millisecond)
	e.enqueue(t, e.repos[0])

	paths := []string{
		"/internal/v1/jobs/claim",
		"/internal/v1/jobs/some-id/heartbeat",
		"/internal/v1/jobs/some-id/result",
		"/internal/v1/jobs/some-id/release",
		"/internal/v1/jobs/some-id/token",
		"/api/v1/anything",
		"/",
	}
	for _, auth := range []string{
		"",
		"Bearer ",
		"Bearer wrong",
		"Bearer " + secret + "x",
		"Bearer " + secret[:len(secret)-1],
		secret,
		"Basic " + secret,
	} {
		for _, path := range paths {
			if status, body := e.post(t, path, auth, map[string]string{}); status != http.StatusUnauthorized {
				t.Errorf("POST %s with Authorization %q = %d, want 401", path, auth, status)
			} else if strings.Contains(string(body), secret) {
				t.Errorf("401 body echoes the secret: %s", body)
			}
		}
	}

	// A refused claim must not have taken the queued job.
	if job, err := e.q.Claim(context.Background()); err != nil || job == nil {
		t.Fatalf("job was claimed by an unauthenticated request: %v", err)
	}
}

func TestRoutesOutsideTheWorkerTableAreNotFound(t *testing.T) {
	e := newEnv(t, 50*time.Millisecond)
	auth := "Bearer " + secret

	for _, path := range []string{"/", "/healthz", "/api/v1/status", "/internal/v1/jobs/some-id/token", "/internal/v1/jobs"} {
		if status, _ := e.post(t, path, auth, map[string]string{}); status != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, status)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, e.srv.URL+queue.PathClaim, nil)
	req.Header.Set("Authorization", auth)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET claim = %d, want 405", resp.StatusCode)
	}
}

func TestClaimReturns204AfterTheBoundedWait(t *testing.T) {
	e := newEnv(t, 80*time.Millisecond)

	start := time.Now()
	status, _ := e.post(t, queue.PathClaim, "Bearer "+secret, struct{}{})
	if status != http.StatusNoContent {
		t.Fatalf("claim on an empty queue = %d, want 204", status)
	}
	if waited := time.Since(start); waited < 70*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("claim waited %v, want about the 80ms bound", waited)
	}
}

func TestEnqueueWakesAWaitingClaim(t *testing.T) {
	e := newEnv(t, 30*time.Second)

	type reply struct {
		status int
		body   []byte
	}
	got := make(chan reply, 1)
	go func() {
		status, body := e.post(t, queue.PathClaim, "Bearer "+secret, struct{}{})
		got <- reply{status, body}
	}()

	select {
	case r := <-got:
		t.Fatalf("claim returned %d before any enqueue", r.status)
	case <-time.After(150 * time.Millisecond):
	}
	enqueued := time.Now()
	e.enqueue(t, e.repos[0])

	select {
	case r := <-got:
		if r.status != http.StatusOK {
			t.Fatalf("woken claim = %d, want 200", r.status)
		}
		var job queue.ClaimedJob
		if err := json.Unmarshal(r.body, &job); err != nil || job.RepositoryFullName != "acme/repo-1" || job.LeaseOwner == "" {
			t.Fatalf("claim body = %s (%v), want the job with its lease", r.body, err)
		}
		if woke := time.Since(enqueued); woke > 2*time.Second {
			t.Fatalf("claim answered %v after the enqueue, want it woken at once", woke)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the waiting claim was not woken by the enqueue")
	}
}

func TestHeartbeatResultAndReleaseThroughTheRoutes(t *testing.T) {
	e := newEnv(t, time.Second)
	auth := "Bearer " + secret
	e.enqueue(t, e.repos[0])

	var job queue.ClaimedJob
	status, body := e.post(t, queue.PathClaim, auth, struct{}{})
	if status != http.StatusOK || json.Unmarshal(body, &job) != nil {
		t.Fatalf("claim = %d %s", status, body)
	}
	route := func(pattern string) string { return strings.Replace(pattern, "{id}", job.ID, 1) }

	status, body = e.post(t, route(queue.PathHeartbeat), auth, queue.LeaseRequest{LeaseOwner: job.LeaseOwner})
	if status != http.StatusOK || strings.TrimSpace(string(body)) != `{"abort":false}` {
		t.Fatalf("heartbeat = %d %s, want 200 no abort", status, body)
	}
	status, body = e.post(t, route(queue.PathHeartbeat), auth, queue.LeaseRequest{LeaseOwner: "0.other"})
	if status != http.StatusOK || !strings.Contains(string(body), `"abort":true`) {
		t.Fatalf("heartbeat of a foreign lease = %d %s, want an abort", status, body)
	}

	if status, _ = e.post(t, route(queue.PathResult), auth, queue.ResultRequest{LeaseOwner: "0.other", Outcome: queue.OutcomeSucceeded}); status != http.StatusConflict {
		t.Errorf("result with a foreign lease = %d, want 409", status)
	}
	if status, _ = e.post(t, route(queue.PathResult), auth, queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: "maybe"}); status != http.StatusUnprocessableEntity {
		t.Errorf("result with an unknown outcome = %d, want 422", status)
	}
	if status, _ = e.post(t, route(queue.PathResult), auth, "not an object"); status != http.StatusBadRequest {
		t.Errorf("result with a malformed body = %d, want 400", status)
	}

	if status, _ = e.post(t, route(queue.PathRelease), auth, queue.LeaseRequest{LeaseOwner: job.LeaseOwner}); status != http.StatusNoContent {
		t.Fatalf("release = %d, want 204", status)
	}
	status, body = e.post(t, queue.PathClaim, auth, struct{}{})
	if status != http.StatusOK || json.Unmarshal(body, &job) != nil || job.Attempt != 1 {
		t.Fatalf("claim after release = %d %s, want the same job again at attempt 1", status, body)
	}
	for i := range 2 {
		if status, _ = e.post(t, route(queue.PathResult), auth, queue.ResultRequest{LeaseOwner: job.LeaseOwner, Outcome: queue.OutcomeSucceeded}); status != http.StatusNoContent {
			t.Fatalf("result %d = %d, want 204 (a repeat is a no-op)", i+1, status)
		}
	}
}

func TestResultBodyIsSizeLimited(t *testing.T) {
	e := newEnv(t, time.Second)

	big := queue.ResultRequest{LeaseOwner: strings.Repeat("a", 200<<10), Outcome: queue.OutcomeFailed}
	if status, _ := e.post(t, strings.Replace(queue.PathResult, "{id}", "x", 1), "Bearer "+secret, big); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", status)
	}
}
