package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

const enableRepoName = "acme/repo-1"

func newEnableServer(t *testing.T, user github.User, accessible ...int64) *rolesServer {
	t.Helper()
	s := newRolesServer(t, user, accessible...)
	s.fake.SetFiles(enableRepoName, "README.md")
	s.fake.SetBranchHead(enableRepoName, "main", "base-sha")
	return s
}

func repositoryState(t *testing.T, s *rolesServer, id string) (enabled bool, configState string) {
	t.Helper()
	var e int
	if err := s.deps.DB.QueryRow(`SELECT enabled, config_state FROM repositories WHERE id = ?`, id).Scan(&e, &configState); err != nil {
		t.Fatalf("read repository: %v", err)
	}
	return e == 1, configState
}

func enableResult(t *testing.T, body string) (url string, number int64) {
	t.Helper()
	var out struct {
		URL    string `json:"pullRequestUrl"`
		Number int64  `json:"number"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.URL == "" || out.Number == 0 {
		t.Fatalf("body %q is no pull request: %v", body, err)
	}
	return out.URL, out.Number
}

func TestEnableRepoOpensOnePullRequestExtendingTheConfiguredPreset(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	s.deps.Store.SetInstanceConfig(store.InstanceConfig{PresetRepositories: []string{"acme/presets", "acme/other"}})
	admin := s.signIn(t)

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", "")
	if code != http.StatusCreated {
		t.Fatalf("enable = %d %s, want 201", code, body)
	}
	url, number := enableResult(t, body)
	if !strings.HasSuffix(url, "/pull/1") || number != 1 {
		t.Errorf("pull request = %s #%d, want #1", url, number)
	}
	file, ok := s.fake.BranchFile(enableRepoName, github.EnableRenovateBranch, "renovate.json")
	if !ok || !strings.Contains(file.Content, `"local>acme/presets"`) || strings.Contains(file.Content, "acme/other") {
		t.Errorf("renovate.json = %q (%v), want it to extend the first preset repository only", file.Content, ok)
	}
	if _, ok := s.fake.BranchFile(enableRepoName, "main", "renovate.json"); ok {
		t.Error("renovate.json was written to the default branch")
	}
	if enabled, state := repositoryState(t, s, "repo-1"); !enabled || state != "unknown" {
		t.Errorf("repo-1 = enabled %v, config %s, want it unchanged", enabled, state)
	}

	code, body = s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", "")
	if code != http.StatusOK {
		t.Fatalf("second enable = %d %s, want 200 with the open pull request", code, body)
	}
	if again, n := enableResult(t, body); again != url || n != number {
		t.Errorf("second enable = %s #%d, want %s #%d", again, n, url, number)
	}
	opened := 0
	for _, req := range s.fake.Requests() {
		if req.Method == http.MethodPost && req.Path == "/repos/acme/repo-1/pulls" {
			opened++
		}
	}
	if opened != 1 {
		t.Errorf("%d pull requests were opened, want 1", opened)
	}
}

func TestEnableRepoMakesNoGitHubCallWhileTheInstallationBudgetIsUsedUp(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	s.budget.Observe(1, ratelimit.Signal{Remaining: 0, HasRemaining: true, ResetAt: time.Now().Add(10 * time.Minute)})
	before := len(s.fake.Requests())

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", "")
	if code != http.StatusBadGateway || errorCodeOf(body) != "GITHUB_UNAVAILABLE" {
		t.Errorf("enable with the budget used up = %d %s, want 502 GITHUB_UNAVAILABLE", code, body)
	}
	if n := len(s.fake.Requests()); n != before {
		t.Errorf("enable made %d GitHub calls while the budget was used up, want none", n-before)
	}
	if _, ok := s.fake.BranchFile(enableRepoName, github.EnableRenovateBranch, "renovate.json"); ok {
		t.Error("a branch was written while the budget was used up")
	}
}

func TestEnableRepoMintsATokenLimitedToContentsAndPullRequestWrite(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""); code != http.StatusCreated {
		t.Fatalf("enable = %d %s", code, body)
	}
	var enable *githubtest.TokenGrant
	for _, g := range s.fake.TokenGrants() {
		if len(g.Repositories) == 1 && g.Repositories[0] == "repo-1" {
			enable = &g
		}
	}
	if enable == nil || len(enable.Permissions) != 2 || enable.Permissions["contents"] != "write" || enable.Permissions["pull_requests"] != "write" {
		t.Errorf("enable token request = %+v, want repo-1 with contents and pull_requests write only", enable)
	}
}

func TestEnableRepoExtendsTheDefaultPresetWhenNoneIsConfigured(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""); code != http.StatusCreated {
		t.Fatalf("enable = %d %s", code, body)
	}
	if file, _ := s.fake.BranchFile(enableRepoName, github.EnableRenovateBranch, "renovate.json"); !strings.Contains(file.Content, `"config:recommended"`) {
		t.Errorf("renovate.json = %q, want config:recommended", file.Content)
	}
}

func TestEnableRepoFailureIsReportedAndDoesNotEnableTheRepository(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusUnprocessableEntity} {
		s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
		admin := s.signIn(t)
		if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false}`); code != http.StatusOK {
			t.Fatalf("pause = %d %s", code, body)
		}
		s.fake.FailPullCreation(enableRepoName, status)

		code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", "")
		if code != http.StatusBadGateway || errorCodeOf(body) != "GITHUB_UNAVAILABLE" {
			t.Errorf("GitHub %d: enable = %d %s, want 502 GITHUB_UNAVAILABLE", status, code, body)
		}
		if enabled, state := repositoryState(t, s, "repo-1"); enabled || state != "unknown" {
			t.Errorf("GitHub %d: repo-1 = enabled %v, config %s, want it still paused with its config state", status, enabled, state)
		}
		if _, ok := s.fake.BranchFile(enableRepoName, "main", "renovate.json"); ok {
			t.Errorf("GitHub %d: the default branch was written", status)
		}
	}
}

func TestEnableRepoRefusesARepositoryThatHasAConfig(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	if _, err := s.deps.DB.Exec(`UPDATE repositories SET config_state = 'present' WHERE id = 'repo-1'`); err != nil {
		t.Fatal(err)
	}
	before := len(s.fake.Requests())
	if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""); code != http.StatusConflict || errorCodeOf(body) != "CONFIG_EXISTS" {
		t.Fatalf("enable of a repository with a recorded config = %d %s, want 409 CONFIG_EXISTS", code, body)
	}
	if n := len(s.fake.Requests()); n != before {
		t.Errorf("a recorded config still made %d GitHub calls", n-before)
	}

	if _, err := s.deps.DB.Exec(`UPDATE repositories SET config_state = 'missing' WHERE id = 'repo-1'`); err != nil {
		t.Fatal(err)
	}
	s.fake.SetFiles(enableRepoName, "README.md", "renovate.json")
	if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""); code != http.StatusConflict || errorCodeOf(body) != "CONFIG_EXISTS" {
		t.Fatalf("enable of a repository whose default branch has a config = %d %s, want 409 CONFIG_EXISTS", code, body)
	}
	for _, req := range s.fake.Requests() {
		if req.Method != http.MethodGet && strings.HasPrefix(req.Path, "/repos/") {
			t.Errorf("%s %s: nothing is written for a repository that has a config", req.Method, req.Path)
		}
	}
}

func TestEnableRepoAnswers409WhenTheBranchHoldsCommitsOfSomeoneElseAndLeavesItAlone(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	s.fake.SetBranchHead(enableRepoName, github.EnableRenovateBranch, "base-sha")
	s.fake.AddBranchCommit(enableRepoName, github.EnableRenovateBranch, "fix: tune the preset", "renovate.json")
	headBefore, _ := s.fake.BranchHeadSHA(enableRepoName, github.EnableRenovateBranch)

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", "")
	if code != http.StatusConflict || errorCodeOf(body) != "ENABLE_BRANCH_EXISTS" {
		t.Fatalf("enable with a foreign commit on the branch = %d %s, want 409 ENABLE_BRANCH_EXISTS", code, body)
	}
	if !strings.Contains(body, github.EnableRenovateBranch) {
		t.Errorf("body %s does not name the branch", body)
	}
	if head, _ := s.fake.BranchHeadSHA(enableRepoName, github.EnableRenovateBranch); head != headBefore {
		t.Errorf("the branch moved from %q to %q", headBefore, head)
	}
	for _, req := range s.fake.Requests() {
		if req.Method != http.MethodGet && strings.HasPrefix(req.Path, "/repos/") {
			t.Errorf("%s %s: nothing is written while the branch holds someone else's commits", req.Method, req.Path)
		}
	}
	if enabled, state := repositoryState(t, s, "repo-1"); !enabled || state != "unknown" {
		t.Errorf("repo-1 = enabled %v, config %s, want it unchanged", enabled, state)
	}
}

func TestEnableRepoIsRefusedToAViewerWithoutCallingGitHub(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	before := len(s.fake.Requests())

	if code, body := s.send(t, viewer, http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""); code != http.StatusForbidden || errorCodeOf(body) != "FORBIDDEN" {
		t.Fatalf("viewer enable = %d %s, want 403 FORBIDDEN", code, body)
	}
	if n := len(s.fake.Requests()); n != before {
		t.Errorf("a refused viewer made %d GitHub calls", n-before)
	}
}

func TestEnableRepoAnswers404ForARepositoryOutsideTheInstallation(t *testing.T) {
	t.Parallel()
	s := newEnableServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET removed_at = '2026-11-14T11:00:00.000Z' WHERE id = 'repo-1'`); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"no-such-repository", "repo-1"} {
		if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/"+id+"/enable-renovate", ""); code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
			t.Errorf("enable of %s = %d %s, want 404 NOT_FOUND", id, code, body)
		}
	}
}

type listedForViewer struct {
	ID            string         `json:"id"`
	FullName      string         `json:"fullName"`
	HTMLURL       *string        `json:"htmlUrl"`
	OpenUpdatePRs int            `json:"openUpdatePrs"`
	DashboardURL  *string        `json:"dashboardUrl"`
	LastJob       *listedLastJob `json:"lastJob"`
}

type listedLastJob struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	FinishedAt *string `json:"finishedAt"`
}

func rawRepositories(t *testing.T, body string) []map[string]json.RawMessage {
	t.Helper()
	var out struct {
		Items []map[string]json.RawMessage `json:"items"`
		Total int                          `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out.Items
}

func TestAViewerListsOnlyTheirRepositoriesWithoutTicketProjects(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET ticket_project_id = ? WHERE id = 'repo-1'`, seedTicketProject(t, s, "DEV")); err != nil {
		t.Fatal(err)
	}

	code, body := s.send(t, viewer, http.MethodGet, "/api/v1/repositories", "")
	if code != http.StatusOK {
		t.Fatalf("viewer list = %d %s, want 200", code, body)
	}
	items := rawRepositories(t, body)
	if len(items) != 1 || string(items[0]["id"]) != `"repo-1"` {
		t.Fatalf("viewer list = %s, want repo-1 alone", body)
	}
	if strings.Contains(body, "repo-2") || !strings.Contains(body, `"total":1`) {
		t.Errorf("viewer list = %s, want the repository outside the scope absent and not counted", body)
	}
	for _, field := range []string{"ticketProjectId", "effectiveTicketProjectId", "effectiveTicketProjectName"} {
		if _, ok := items[0][field]; ok {
			t.Errorf("viewer list carries %s", field)
		}
	}

	code, body = s.send(t, viewer, http.MethodGet, "/api/v1/repositories/repo-1", "")
	if code != http.StatusOK || strings.Contains(body, "ticketProject") || strings.Contains(body, "TicketProject") {
		t.Errorf("viewer detail of repo-1 = %d %s, want 200 without ticket project fields", code, body)
	}
	for _, id := range []string{"repo-2", "no-such-repository"} {
		if code, body := s.send(t, viewer, http.MethodGet, "/api/v1/repositories/"+id, ""); code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
			t.Errorf("viewer detail of %s = %d %s, want 404 NOT_FOUND", id, code, body)
		}
	}
}

func TestAViewerWithNoRepositoriesListsNone(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"})
	viewer := s.signIn(t)

	code, body := s.send(t, viewer, http.MethodGet, "/api/v1/repositories", "")
	if code != http.StatusOK || len(rawRepositories(t, body)) != 0 || !strings.Contains(body, `"total":0`) {
		t.Errorf("viewer list = %d %s, want an empty list", code, body)
	}
}

func TestAnAdminListsEveryRepositoryWithTheTicketProjectFields(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	items := rawRepositories(t, func() string {
		_, body := s.send(t, admin, http.MethodGet, "/api/v1/repositories", "")
		return body
	}())
	if len(items) != 2 {
		t.Fatalf("admin list has %d repositories, want 2", len(items))
	}
	for _, field := range []string{"ticketProjectId", "effectiveTicketProjectId", "effectiveTicketProjectName"} {
		if v, ok := items[0][field]; !ok || string(v) != "null" {
			t.Errorf("admin list %s = %s (present %v), want an explicit null", field, v, ok)
		}
	}
	if code, body := s.send(t, admin, http.MethodGet, "/api/v1/repositories/repo-2", ""); code != http.StatusOK || !strings.Contains(body, `"ticketProjectId":null`) {
		t.Errorf("admin detail = %d %s, want 200 with the ticket project fields", code, body)
	}
}

func TestTheRepositoryListCarriesOpenPullRequestsTheLastJobAndTheDashboardLink(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	for _, stmt := range []string{
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'n1', 'https://example.test/pr/1', 'a', 'renovate/a', 'open', 't', 't', 't'),
		        ('pr-2', 'repo-1', 2, 'n2', 'https://example.test/pr/2', 'b', 'renovate/b', 'open', 't', 't', 't'),
		        ('pr-3', 'repo-1', 3, 'n3', 'https://example.test/pr/3', 'c', 'renovate/c', 'merged', 't', 't', 't'),
		        ('pr-4', 'repo-2', 4, 'n4', 'https://example.test/pr/4', 'd', 'renovate/d', 'open', 't', 't', 't')`,
		`UPDATE repositories SET dashboard_issue_number = 7 WHERE id = 'repo-1'`,
	} {
		if _, err := s.deps.DB.Exec(stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	queued, err := s.deps.Queue.Enqueue(t.Context(), queue.EnqueueParams{RepositoryID: "repo-1", Priority: queue.PriorityHigh, Trigger: queue.TriggerRunNow})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	list := func() map[string]listedForViewer {
		_, body := s.send(t, admin, http.MethodGet, "/api/v1/repositories", "")
		var out struct {
			Items []listedForViewer `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		byID := map[string]listedForViewer{}
		for _, r := range out.Items {
			byID[r.ID] = r
		}
		return byID
	}
	got := list()
	one, two := got["repo-1"], got["repo-2"]
	if one.OpenUpdatePRs != 2 || two.OpenUpdatePRs != 1 {
		t.Errorf("open update PRs = %d and %d, want 2 (the merged one not counted) and 1", one.OpenUpdatePRs, two.OpenUpdatePRs)
	}
	if one.HTMLURL == nil || *one.HTMLURL != "https://github.example.com/acme/repo-1" {
		t.Errorf("repository page = %v, want its GitHub page", one.HTMLURL)
	}
	if one.DashboardURL == nil || *one.DashboardURL != "https://github.example.com/acme/repo-1/issues/7" || two.DashboardURL != nil {
		t.Errorf("dashboard URLs = %v and %v, want the issue link of repo-1 and none for repo-2", one.DashboardURL, two.DashboardURL)
	}
	if one.LastJob == nil || one.LastJob.ID != queued.JobID || one.LastJob.State != "queued" || one.LastJob.FinishedAt != nil || two.LastJob != nil {
		t.Errorf("last jobs = %+v and %+v, want repo-1's queued job and none for repo-2", one.LastJob, two.LastJob)
	}

	job, err := s.deps.Queue.Claim(t.Context())
	if err != nil || job.ID != queued.JobID {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	if lj := list()["repo-1"].LastJob; lj == nil || lj.State != "running" {
		t.Errorf("last job while claimed = %+v, want running", lj)
	}
}
