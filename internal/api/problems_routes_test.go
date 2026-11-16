package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/github"
)

type problemItem struct {
	Kind       string `json:"kind"`
	Repository *struct {
		ID       string `json:"id"`
		FullName string `json:"fullName"`
	} `json:"repository"`
	Since   string  `json:"since"`
	Message *string `json:"message"`
	Ref     *struct {
		JobID          string `json:"jobId"`
		UpdatePRID     string `json:"updatePrId"`
		UpdatePRURL    string `json:"updatePrUrl"`
		UpdateTicketID string `json:"updateTicketId"`
	} `json:"ref"`
}

type problemsPage struct {
	Items []problemItem `json:"items"`
	Total int           `json:"total"`
}

func readProblems(t *testing.T, s *rolesServer, c *http.Client, query string) (problemsPage, string) {
	t.Helper()
	code, body := s.send(t, c, http.MethodGet, "/api/v1/problems"+query, "")
	if code != http.StatusOK {
		t.Fatalf("GET /problems%s = %d %s, want 200", query, code, body)
	}
	var page problemsPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return page, body
}

func (p problemsPage) kinds(repository string) []string {
	var out []string
	for _, it := range p.Items {
		name := ""
		if it.Repository != nil {
			name = it.Repository.ID
		}
		if repository == "*" || name == repository {
			out = append(out, it.Kind)
		}
	}
	return out
}

func (p problemsPage) first(kind, repository string) *problemItem {
	for i, it := range p.Items {
		if it.Kind == kind && it.Repository != nil && it.Repository.ID == repository {
			return &p.Items[i]
		}
	}
	return nil
}

// grantManifestPermissions gives the installation every permission the manifest requests, so it
// has no permissions_lost problem.
func grantManifestPermissions(t *testing.T, s *rolesServer) {
	t.Helper()
	raw, err := json.Marshal(setup.BuildManifest("", "", "").DefaultPermissions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.deps.DB.Exec(`UPDATE installations SET permissions = ?`, string(raw)); err != nil {
		t.Fatalf("grant permissions: %v", err)
	}
}

// seedProblems stores one problem in each repository: a failed latest job (repo-1 after an
// older success, repo-2) and a ticket with an error (repo-2) or a stuck pending marker (repo-1).
// repo-1 also has an older failure behind a later success, which is no problem.
func seedProblems(t *testing.T, s *rolesServer) {
	t.Helper()
	seedJobHistory(t, s)
	tp := seedTicketProject(t, s, "DEV")
	const at = "2026-11-16T11:00:00.000Z"
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
			VALUES ('pr-1', 'repo-1', 1, 'n', 'https://github.com/acme/repo-1/pull/1', 'Bump one', 'renovate/one', 'open', ?, ?, ?)`, []any{at, at, at}},
		{`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
			VALUES ('pr-2', 'repo-2', 2, 'n', 'https://github.com/acme/repo-2/pull/2', 'Bump two', 'renovate/two', 'open', ?, ?, ?)`, []any{at, at, at}},
		{`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, pending_since, attempts, last_error, last_error_at, created_at, updated_at)
			VALUES ('ut-1', 'pr-1', ?, 'pending', ?, 1, 'create ticket: unexpected status 503', ?, ?, ?)`, []any{tp, at, at, at, at}},
		{`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, external_id, state, last_error, last_error_at, created_at, updated_at)
			VALUES ('ut-2', 'pr-2', ?, 'DEV-2', 'open', 'repo-2 ticket failure', ?, ?, ?)`, []any{tp, at, at, at}},
	} {
		if _, err := s.deps.DB.Exec(stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestAViewerSeesOnlyTheJobAndTicketProblemsOfTheirRepositories(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	seedProblems(t, s)
	// Installation-level problems exist, and a viewer sees none of them.
	if _, err := s.deps.DB.Exec(`UPDATE installations SET rate_limit_remaining = 0, rate_limit_reset_at = ?`, time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}

	page, body := readProblems(t, s, viewer, "")
	if page.Total != 2 || len(page.kinds("repo-1")) != 2 || len(page.kinds("*")) != 2 {
		t.Fatalf("viewer problems = %s, want one job_failed and one ticket_failed, both of repo-1", body)
	}
	if strings.Contains(body, "repo-2") || strings.Contains(body, "pr-2") || strings.Contains(body, "rate_limited") || strings.Contains(body, "permissions_lost") {
		t.Fatalf("a viewer's problems mention another repository or an installation-level problem: %s", body)
	}
	job := page.first("job_failed", "repo-1")
	if job == nil || job.Ref == nil || job.Ref.JobID != "job-a2" || job.Message == nil || *job.Message != "renovate exited with status 3" {
		t.Fatalf("job_failed = %+v, want the latest failed job of repo-1 with its error", job)
	}
	ticket := page.first("ticket_failed", "repo-1")
	if ticket == nil || ticket.Message != nil || ticket.Ref == nil || ticket.Ref.UpdatePRID != "pr-1" || ticket.Ref.UpdateTicketID != "" {
		t.Fatalf("viewer ticket_failed = %+v, want the update PR in ref, no message and no ticket row id", ticket)
	}
	if strings.Contains(body, "unexpected status 503") || strings.Contains(body, "ut-1") {
		t.Fatalf("a viewer's problems carry the ticket error text or row id: %s", body)
	}
}

func TestAnAdminSeesEveryProblemKindAndMayClearAStuckMarker(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	seedProblems(t, s)
	if _, err := s.deps.DB.Exec(`UPDATE installations SET rate_limit_remaining = 0, rate_limit_reset_at = ?`, time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
		t.Fatal(err)
	}

	page, body := readProblems(t, s, admin, "")
	count := map[string]int{}
	for _, it := range page.Items {
		count[it.Kind]++
	}
	if count["job_failed"] != 2 || count["ticket_failed"] != 2 || count["rate_limited"] != 1 || count["permissions_lost"] != 1 || page.Total != 6 {
		t.Fatalf("admin problems = %s, want 2 job_failed, 2 ticket_failed, 1 rate_limited and 1 permissions_lost", body)
	}
	for _, it := range page.Items {
		if (it.Kind == "rate_limited" || it.Kind == "permissions_lost") && (it.Repository != nil || it.Ref != nil || it.Message == nil) {
			t.Errorf("%s = %+v, want no repository and no ref, with a message", it.Kind, it)
		}
	}
	stuck := page.first("ticket_failed", "repo-1")
	if stuck == nil || stuck.Message == nil || *stuck.Message != "create ticket: unexpected status 503" || stuck.Ref == nil || stuck.Ref.UpdateTicketID != "ut-1" || stuck.Ref.UpdatePRURL != "https://github.com/acme/repo-1/pull/1" {
		t.Fatalf("stuck ticket problem = %+v, want its error and the row id the clear action takes", stuck)
	}
	other := page.first("ticket_failed", "repo-2")
	if other == nil || other.Ref == nil || other.Ref.UpdateTicketID != "" || other.Message == nil || *other.Message != "repo-2 ticket failure" {
		t.Fatalf("ticket problem with a stored ticket = %+v, want its error and no clear id", other)
	}

	// The id the problem hands out is the one the clear action takes, and the problem goes with
	// the marker's cause: the row has no error and no marker left.
	if code, body := s.send(t, admin, http.MethodPost, "/api/v1/update-tickets/"+stuck.Ref.UpdateTicketID+"/clear-pending-marker", ""); code != http.StatusOK {
		t.Fatalf("clear = %d %s, want 200", code, body)
	}
	after, _ := readProblems(t, s, admin, "")
	if after.first("ticket_failed", "repo-1") != nil || after.Total != 5 {
		t.Fatalf("problems after the clear = %+v, want the stuck ticket gone", after.Items)
	}
}

func TestAProblemGoesWhenItsCauseDoes(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	seedJobHistory(t, s)
	grantManifestPermissions(t, s)

	if page, _ := readProblems(t, s, admin, ""); len(page.kinds("repo-1")) != 1 || len(page.kinds("repo-2")) != 1 || page.Total != 2 {
		t.Fatalf("problems = %+v, want the failed latest job of each repository", page.Items)
	}
	// A later success ends the problem of repo-1; the older failure behind it is history.
	if _, err := s.deps.DB.Exec(`INSERT INTO jobs (id, kind, repository_id, priority, state, run_after, attempts, "trigger", created_at, finished_at)
		VALUES ('job-a3', 'renovate_run', 'repo-1', 'normal', 'succeeded', '2026-11-16T12:00:00.000Z', 1, 'schedule', '2026-11-16T12:00:00.000Z', '2026-11-16T12:01:00.000Z')`); err != nil {
		t.Fatal(err)
	}
	if page, _ := readProblems(t, s, admin, ""); len(page.kinds("repo-1")) != 0 || page.Total != 1 {
		t.Fatalf("problems after a success = %+v, want only the failure of repo-2", page.Items)
	}
	// A repository that left the installation has no problem.
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET removed_at = '2026-11-16T13:00:00.000Z' WHERE id = 'repo-2'`); err != nil {
		t.Fatal(err)
	}
	if page, _ := readProblems(t, s, admin, ""); page.Total != 0 {
		t.Fatalf("problems of a removed repository = %+v, want none", page.Items)
	}
}

func TestInstallationProblemsNameTheCauseAndNeverACredential(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	grantManifestPermissions(t, s)

	if page, _ := readProblems(t, s, admin, ""); page.Total != 0 {
		t.Fatalf("a healthy installation has problems: %+v", page.Items)
	}

	if _, err := s.deps.DB.Exec(`UPDATE installations SET permissions = '{"metadata":"read"}'`); err != nil {
		t.Fatal(err)
	}
	page, _ := readProblems(t, s, admin, "")
	if len(page.Items) != 1 || page.Items[0].Kind != "permissions_lost" || !strings.Contains(*page.Items[0].Message, "contents") {
		t.Fatalf("missing permissions problem = %+v, want permissions_lost naming the missing permissions", page.Items)
	}

	grantManifestPermissions(t, s)
	if _, err := s.deps.DB.Exec(`UPDATE installations SET status = 'suspended'`); err != nil {
		t.Fatal(err)
	}
	if page, _ := readProblems(t, s, admin, ""); len(page.Items) != 1 || page.Items[0].Kind != "permissions_lost" || !strings.Contains(*page.Items[0].Message, "suspended") {
		t.Fatalf("suspended installation problem = %+v, want permissions_lost saying it is suspended", page.Items)
	}

	// An exhausted budget whose reset has passed is no problem; the latest attempt ending
	// rate-limited is.
	if _, err := s.deps.DB.Exec(`UPDATE installations SET status = 'active', rate_limit_remaining = 0, rate_limit_reset_at = '2020-01-01T00:00:00.000Z'`); err != nil {
		t.Fatal(err)
	}
	if page, _ := readProblems(t, s, admin, ""); page.Total != 0 {
		t.Fatalf("an exhausted budget past its reset is a problem: %+v", page.Items)
	}
	seedJobHistory(t, s)
	if _, err := s.deps.DB.Exec(`INSERT INTO job_runs (id, job_id, attempt, started_at, finished_at, outcome, error_code)
		VALUES ('run-limited', 'job-a1', 2, '2026-11-16T20:00:00.000Z', '2026-11-16T20:01:00.000Z', 'failed', 'rate_limited')`); err != nil {
		t.Fatal(err)
	}
	limited := 0
	page, _ = readProblems(t, s, admin, "")
	for _, it := range page.Items {
		if it.Kind == "rate_limited" && it.Since == "2026-11-16T20:01:00.000Z" {
			limited++
		}
	}
	if limited != 1 {
		t.Fatalf("problems = %+v, want one rate_limited since the latest attempt ended", page.Items)
	}
}

func TestTheProblemListIsPaged(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	seedJobHistory(t, s)
	grantManifestPermissions(t, s)

	page, _ := readProblems(t, s, admin, "?pageSize=1&page=2")
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].Ref == nil || page.Items[0].Ref.JobID != "job-a2" {
		t.Fatalf("second page of one = %+v (total %d), want the older problem, job-a2", page.Items, page.Total)
	}
	if page, _ := readProblems(t, s, admin, "?page=9"); len(page.Items) != 0 || page.Total != 2 {
		t.Fatalf("page past the end = %+v (total %d), want no items and the total", page.Items, page.Total)
	}
	for _, query := range []string{"?page=0", "?pageSize=101", "?pageSize=x"} {
		if code, body := s.send(t, admin, http.MethodGet, "/api/v1/problems"+query, ""); code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("GET /problems%s = %d %s, want 400 VALIDATION_ERROR", query, code, body)
		}
	}
}
