package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/internalapi"
)

// seedJobHistory stores the jobs of the two repositories: repo-1 has a succeeded job (a
// checkbox tick) and then a failed one with its attempt and events; repo-2 has a failed job whose
// error must never reach a viewer who cannot see repo-2.
func seedJobHistory(t *testing.T, s *rolesServer) {
	t.Helper()
	for _, stmt := range []string{
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, "trigger", matched_markers, webhook_received_at, started_at, created_at, finished_at)
			VALUES ('job-a1', 'renovate_run', 'repo-1', 'high', 'succeeded', '2026-11-16T08:00:00.000Z', 1, 'dashboard_checkbox', '["rebase-branch=renovate/foo-2.x"]',
			        '2026-11-16T08:00:00.000Z', '2026-11-16T08:00:03.200Z', '2026-11-16T08:00:00.000Z', '2026-11-16T08:01:00.000Z')`,
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, "trigger", started_at, created_at, finished_at)
			VALUES ('job-a2', 'renovate_run', 'repo-1', 'normal', 'failed', '2026-11-16T09:00:00.000Z', 3, 'schedule',
			        '2026-11-16T09:00:05.000Z', '2026-11-16T09:00:00.000Z', '2026-11-16T09:10:00.000Z')`,
		`INSERT INTO job_runs (id, job_id, attempt, started_at, finished_at, outcome, exit_code, error_code, error_message, report_status, log_excerpt)
			VALUES ('run-a2-1', 'job-a2', 1, '2026-11-16T09:00:05.000Z', '2026-11-16T09:02:00.000Z', 'failed', 3, 'renovate_failed', 'renovate exited with status 3', 'ok', 'boom in repo-1')`,
		`INSERT INTO job_events (id, job_id, job_run_id, at, kind, message)
			VALUES ('ev-a2-1', 'job-a2', 'run-a2-1', '2026-11-16T09:02:00.000Z', 'retry_scheduled', 'attempt 1 of 3 failed')`,
		`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, "trigger", created_at, finished_at)
			VALUES ('job-b1', 'renovate_run', 'repo-2', 'normal', 'failed', '2026-11-16T10:00:00.000Z', 3, 'run_now',
			        '2026-11-16T10:00:00.000Z', '2026-11-16T10:05:00.000Z')`,
		`INSERT INTO job_runs (id, job_id, attempt, started_at, finished_at, outcome, error_message, log_excerpt)
			VALUES ('run-b1-1', 'job-b1', 1, '2026-11-16T10:00:05.000Z', '2026-11-16T10:05:00.000Z', 'failed', 'repo-2 only failure', 'repo-2 log')`,
	} {
		if _, err := s.deps.DB.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

type jobsPage struct {
	Items []struct {
		ID         string `json:"id"`
		Repository struct {
			ID       string `json:"id"`
			FullName string `json:"fullName"`
		} `json:"repository"`
		State          string   `json:"state"`
		Trigger        string   `json:"trigger"`
		MatchedMarkers []string `json:"matchedMarkers"`
		StartLatencyMs *int64   `json:"startLatencyMs"`
	} `json:"items"`
	Page, PageSize, Total int
}

func listJobs(t *testing.T, s *rolesServer, c *http.Client, query string) (jobsPage, string) {
	t.Helper()
	code, body := s.send(t, c, http.MethodGet, "/api/v1/jobs"+query, "")
	if code != http.StatusOK {
		t.Fatalf("GET /jobs%s = %d %s, want 200", query, code, body)
	}
	var page jobsPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return page, body
}

func jobIDs(p jobsPage) []string {
	ids := []string{}
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestAViewerListsOnlyTheJobsOfTheirRepositories(t *testing.T) {
	t.Parallel()
	// The member can access repository 1 on GitHub, not repository 2.
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	seedJobHistory(t, s)

	page, body := listJobs(t, s, viewer, "")
	if got := jobIDs(page); !slices.Equal(got, []string{"job-a2", "job-a1"}) || page.Total != 2 {
		t.Fatalf("viewer list = %v (total %d), want job-a2 then job-a1", got, page.Total)
	}
	if strings.Contains(body, "repo-2") || strings.Contains(body, "job-b1") {
		t.Fatalf("a viewer's list mentions a repository outside their scope: %s", body)
	}

	failed, _ := listJobs(t, s, viewer, "?state=failed")
	if got := jobIDs(failed); !slices.Equal(got, []string{"job-a2"}) || failed.Total != 1 {
		t.Fatalf("viewer failed jobs = %v (total %d), want only the failing job of repo-1, not the one of repo-2", got, failed.Total)
	}

	outside, outsideBody := listJobs(t, s, viewer, "?repositoryId=repo-2")
	_, missingBody := listJobs(t, s, viewer, "?repositoryId=no-such-repository")
	if len(outside.Items) != 0 || outside.Total != 0 || outsideBody != missingBody {
		t.Fatalf("repositoryId outside the scope = %s, unknown = %s; want the same empty page", outsideBody, missingBody)
	}
}

func TestAnAdminListsTheJobsOfEveryRepositoryWithTheirTriggerAndLatency(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	seedJobHistory(t, s)

	page, _ := listJobs(t, s, admin, "")
	if got := jobIDs(page); !slices.Equal(got, []string{"job-b1", "job-a2", "job-a1"}) || page.Total != 3 {
		t.Fatalf("admin list = %v (total %d), want every job, newest first", got, page.Total)
	}
	tick := page.Items[2]
	if tick.Trigger != "dashboard_checkbox" || !slices.Equal(tick.MatchedMarkers, []string{"rebase-branch=renovate/foo-2.x"}) {
		t.Errorf("tick job trigger = %q, markers = %v", tick.Trigger, tick.MatchedMarkers)
	}
	if tick.StartLatencyMs == nil || *tick.StartLatencyMs != 3200 {
		t.Errorf("tick job latency = %v, want 3200 ms from the webhook to the first heartbeat", tick.StartLatencyMs)
	}
	if scheduled := page.Items[1]; scheduled.StartLatencyMs != nil || scheduled.MatchedMarkers == nil || len(scheduled.MatchedMarkers) != 0 {
		t.Errorf("scheduled job latency = %v, markers = %v, want null and []", scheduled.StartLatencyMs, scheduled.MatchedMarkers)
	}

	if page, _ := listJobs(t, s, admin, "?repositoryId=repo-2&state=failed"); !slices.Equal(jobIDs(page), []string{"job-b1"}) {
		t.Fatalf("admin repo-2 failed = %v, want job-b1", jobIDs(page))
	}
	paged, _ := listJobs(t, s, admin, "?pageSize=2&page=2")
	if !slices.Equal(jobIDs(paged), []string{"job-a1"}) || paged.Total != 3 || paged.Page != 2 || paged.PageSize != 2 {
		t.Fatalf("second page of two = %v (total %d, page %d, size %d), want job-a1 of 3", jobIDs(paged), paged.Total, paged.Page, paged.PageSize)
	}
}

func TestTheJobListRejectsAMalformedQuery(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	for _, query := range []string{"?state=gone", "?state=running%27%20OR%201=1", "?page=0", "?page=x", "?pageSize=0", "?pageSize=101"} {
		code, body := s.send(t, viewer, http.MethodGet, "/api/v1/jobs"+query, "")
		if code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("GET /jobs%s = %d %s, want 400 VALIDATION_ERROR", query, code, body)
		}
	}
}

func TestAJobShowsItsAttemptsAndEventsAndNeverLeaksAcrossTheScope(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	seedJobHistory(t, s)

	code, body := s.send(t, viewer, http.MethodGet, "/api/v1/jobs/job-a2", "")
	if code != http.StatusOK {
		t.Fatalf("GET /jobs/job-a2 = %d %s, want 200", code, body)
	}
	var detail struct {
		ID   string `json:"id"`
		Runs []struct {
			Attempt      int     `json:"attempt"`
			Outcome      string  `json:"outcome"`
			ExitCode     *int    `json:"exitCode"`
			ErrorMessage *string `json:"errorMessage"`
			LogExcerpt   *string `json:"logExcerpt"`
		} `json:"runs"`
		Events []struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != "job-a2" || len(detail.Runs) != 1 || detail.Runs[0].Outcome != "failed" || detail.Runs[0].ExitCode == nil || *detail.Runs[0].ExitCode != 3 ||
		detail.Runs[0].LogExcerpt == nil || *detail.Runs[0].LogExcerpt != "boom in repo-1" || detail.Runs[0].ErrorMessage == nil {
		t.Fatalf("job detail = %s, want its failed attempt with exit code, error and log excerpt", body)
	}
	if len(detail.Events) != 1 || detail.Events[0].Kind != "retry_scheduled" {
		t.Fatalf("job events = %+v, want the retry event", detail.Events)
	}

	outsideCode, outsideBody := s.send(t, viewer, http.MethodGet, "/api/v1/jobs/job-b1", "")
	missingCode, missingBody := s.send(t, viewer, http.MethodGet, "/api/v1/jobs/no-such-job", "")
	if outsideCode != http.StatusNotFound || errorCodeOf(outsideBody) != "NOT_FOUND" || outsideCode != missingCode || outsideBody != missingBody {
		t.Fatalf("job of another repository = %d %s, unknown = %d %s; want the same 404", outsideCode, outsideBody, missingCode, missingBody)
	}
	if strings.Contains(outsideBody, "repo-2") {
		t.Fatalf("the 404 for a job outside the scope mentions it: %s", outsideBody)
	}

	admin := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	adminClient := admin.signIn(t)
	seedJobHistory(t, admin)
	if code, body := admin.send(t, adminClient, http.MethodGet, "/api/v1/jobs/job-b1", ""); code != http.StatusOK || !strings.Contains(body, "repo-2 log") {
		t.Fatalf("admin GET /jobs/job-b1 = %d %s, want 200 with its log", code, body)
	}
}

func TestTheJobEndpointsNeedASignedInUser(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	seedJobHistory(t, s)
	for _, path := range []string{"/api/v1/jobs", "/api/v1/jobs/job-a1", "/api/v1/problems"} {
		if code, body := s.send(t, noRedirects(t), http.MethodGet, path, ""); code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d %s, want 401", path, code, body)
		}
	}
}

// A worker result is redacted by the app when it arrives, so what the jobs API returns holds no
// token even when the worker missed one: the log excerpt posted here carries a token-shaped
// string and a private key block, and the viewer's read of the job shows neither.
func TestAJobLogExcerptCarriesNoTokenWhenItReachesTheAPI(t *testing.T) {
	t.Parallel()
	const workerSecret = "0123456789abcdef0123456789abcdef-test-secret"
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	internal := httptest.NewServer(internalapi.NewHandler(s.deps.Queue, internalapi.Options{Secret: workerSecret, ClaimWait: 50 * time.Millisecond}))
	t.Cleanup(internal.Close)
	post := func(path string, body any) (int, []byte) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, internal.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+workerSecret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.Bytes()
	}

	if _, err := s.deps.Queue.Enqueue(context.Background(), queue.EnqueueParams{RepositoryID: "repo-1", Priority: queue.PriorityHigh, Trigger: queue.TriggerRunNow}); err != nil {
		t.Fatal(err)
	}
	code, claimed := post(queue.PathClaim, map[string]string{})
	if code != http.StatusOK {
		t.Fatalf("claim = %d %s", code, claimed)
	}
	var job queue.ClaimedJob
	if err := json.Unmarshal(claimed, &job); err != nil {
		t.Fatal(err)
	}
	token := "ghs_" + strings.Repeat("Q", 36)
	// A throwaway key block of generated bytes, not a real key.
	keyBody := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x5a, 0xa5, 0x3c}, 40))
	keyBlock := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: bytes.Repeat([]byte{0x5a, 0xa5, 0x3c}, 40)}))
	code, body := post(strings.Replace(queue.PathResult, "{id}", job.ID, 1), queue.ResultRequest{
		LeaseOwner: job.LeaseOwner, Outcome: queue.OutcomeSucceeded,
		Run: &queue.RunRecord{ExitCode: new(int), ReportStatus: queue.ReportOK, LogExcerpt: "cloning with " + token + " done\nworker secret " + workerSecret + "\n" + keyBlock + "after the key"},
	})
	if code != http.StatusNoContent {
		t.Fatalf("result = %d %s", code, body)
	}

	code, got := s.send(t, viewer, http.MethodGet, "/api/v1/jobs/"+job.ID, "")
	if code != http.StatusOK || !strings.Contains(got, "cloning with") {
		t.Fatalf("GET /jobs/%s = %d %s, want 200 with the excerpt", job.ID, code, got)
	}
	for _, leaked := range []string{token, "ghs_QQQQ", workerSecret, "PRIVATE KEY", keyBody[:40]} {
		if strings.Contains(got, leaked) {
			t.Fatalf("the job detail leaks %q: %s", leaked, got)
		}
	}
}

// A viewer whose session holds no repository sees no job and no problem, rather than everything.
func TestAViewerWithoutRepositoriesSeesNoJobsOrProblems(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"})
	viewer := s.signIn(t)
	seedProblems(t, s)

	if page, body := listJobs(t, s, viewer, ""); page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("jobs of a viewer without repositories = %s, want none", body)
	}
	if code, body := s.send(t, viewer, http.MethodGet, "/api/v1/jobs/job-a1", ""); code != http.StatusNotFound {
		t.Fatalf("GET /jobs/job-a1 = %d %s, want 404", code, body)
	}
	if page, body := readProblems(t, s, viewer, ""); page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("problems of a viewer without repositories = %s, want none", body)
	}
}
