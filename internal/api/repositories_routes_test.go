package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
)

func (s *rolesServer) send(t *testing.T, c *http.Client, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func jobIDOf(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.JobID == "" {
		t.Fatalf("body %q has no jobId: %v", body, err)
	}
	return out.JobID
}

func errorCodeOf(body string) string {
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	return out.Error.Code
}

func repositoryEnabled(t *testing.T, s *rolesServer, id string) bool {
	t.Helper()
	return rowCount(t, s.deps, `SELECT enabled FROM repositories WHERE id = '`+id+`'`) == 1
}

func jobCount(t *testing.T, s *rolesServer) int {
	t.Helper()
	return rowCount(t, s.deps, `SELECT COUNT(*) FROM jobs`)
}

func TestRunNowEnqueuesAHighPriorityJobAndCoalesces(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if code != http.StatusAccepted {
		t.Fatalf("run now = %d %s, want 202", code, body)
	}
	first := jobIDOf(t, body)
	var state, priority, trigger, kind string
	if err := s.deps.DB.QueryRow(`SELECT state, priority, "trigger", kind FROM jobs WHERE id = ?`, first).Scan(&state, &priority, &trigger, &kind); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if state != "queued" || priority != "high" || trigger != "run_now" || kind != "renovate_run" {
		t.Errorf("job = %s %s %s %s, want queued high run_now renovate_run", state, priority, trigger, kind)
	}

	code, body = s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if code != http.StatusAccepted || jobIDOf(t, body) != first {
		t.Fatalf("second run now = %d %s, want 202 with the queued job %s", code, body, first)
	}
	if n := jobCount(t, s); n != 1 {
		t.Errorf("jobs = %d, want 1", n)
	}
}

func TestRunNowUpgradesAQueuedSafetyNetJobAndIgnoresTheConfigState(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET config_state = 'missing' WHERE id = 'repo-1'`); err != nil {
		t.Fatal(err)
	}
	queued, err := s.deps.Queue.Enqueue(t.Context(), queue.EnqueueParams{RepositoryID: "repo-1", Priority: queue.PriorityNormal, Trigger: queue.TriggerSchedule})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if code != http.StatusAccepted || jobIDOf(t, body) != queued.JobID {
		t.Fatalf("run now = %d %s, want 202 with the queued job %s", code, body, queued.JobID)
	}
	var priority, trigger string
	if err := s.deps.DB.QueryRow(`SELECT priority, "trigger" FROM jobs WHERE id = ?`, queued.JobID).Scan(&priority, &trigger); err != nil {
		t.Fatal(err)
	}
	if priority != "high" || trigger != "run_now" {
		t.Errorf("job = %s %s, want the safety-net job upgraded to high run_now", priority, trigger)
	}
}

func TestRunNowOfARunningRepositoryReturnsTheRunningJobAndFlagsAFollowUp(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	_, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	job, err := s.deps.Queue.Claim(t.Context())
	if err != nil || job.ID != jobIDOf(t, body) {
		t.Fatalf("claim = %+v, %v", job, err)
	}

	code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if code != http.StatusAccepted || jobIDOf(t, body) != job.ID {
		t.Fatalf("run now during a run = %d %s, want 202 with the running job", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT rerun_requested FROM jobs WHERE id = '`+job.ID+`'`); n != 1 {
		t.Errorf("rerun_requested = %d, want 1", n)
	}
	if n := jobCount(t, s); n != 1 {
		t.Errorf("jobs = %d, want 1", n)
	}
}

func TestPausingDropsTheQueuedJobRefusesRunNowAndEnablingAllowsItAgain(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-2/run", "")

	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false}`)
	if code != http.StatusOK || !strings.Contains(body, `"enabled":false`) || !strings.Contains(body, `"fullName":"acme/repo-1"`) {
		t.Fatalf("pause = %d %s, want 200 with enabled false", code, body)
	}
	if repositoryEnabled(t, s, "repo-1") {
		t.Fatal("repo-1 is still enabled after the pause")
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM jobs WHERE repository_id = 'repo-1'`); n != 0 {
		t.Errorf("repo-1 jobs = %d, want its queued job dropped", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM jobs WHERE repository_id = 'repo-2'`); n != 1 {
		t.Errorf("repo-2 jobs = %d, want its job kept", n)
	}

	code, body = s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if code != http.StatusConflict || errorCodeOf(body) != "REPOSITORY_PAUSED" {
		t.Fatalf("run now of a paused repository = %d %s, want 409 REPOSITORY_PAUSED", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM jobs WHERE repository_id = 'repo-1'`); n != 0 {
		t.Errorf("a refused run now left %d jobs", n)
	}

	code, body = s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":true}`)
	if code != http.StatusOK || !strings.Contains(body, `"enabled":true`) {
		t.Fatalf("enable = %d %s, want 200 with enabled true", code, body)
	}
	if code, body = s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", ""); code != http.StatusAccepted {
		t.Fatalf("run now after enabling = %d %s, want 202", code, body)
	}
}

func TestPausingARepositoryAbortsItsRunningJobAtTheNextHeartbeat(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	job, err := s.deps.Queue.Claim(t.Context())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if res, err := s.deps.Queue.Heartbeat(t.Context(), job.ID, job.LeaseOwner); err != nil || res.Abort {
		t.Fatalf("heartbeat before the pause = %+v, %v, want no abort", res, err)
	}

	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false}`); code != http.StatusOK {
		t.Fatalf("pause = %d %s", code, body)
	}
	if res, err := s.deps.Queue.Heartbeat(t.Context(), job.ID, job.LeaseOwner); err != nil || !res.Abort {
		t.Fatalf("heartbeat after the pause = %+v, %v, want an abort", res, err)
	}
}

func TestRepositoryRoutesAnswer404ForARepositoryTheCallerCannotUse(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET removed_at = '2026-11-14T11:00:00.000Z' WHERE id = 'repo-2'`); err != nil {
		t.Fatal(err)
	}
	s.send(t, admin, http.MethodPost, "/api/v1/repositories/repo-1/run", "")
	if _, err := s.deps.DB.Exec(`UPDATE installations SET status = 'removed'`); err != nil {
		t.Fatal(err)
	}

	for name, id := range map[string]string{"unknown": "no-such-repository", "removed from the installation": "repo-2", "of a removed installation": "repo-1"} {
		if code, body := s.send(t, admin, http.MethodPost, "/api/v1/repositories/"+id+"/run", ""); code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
			t.Errorf("run now of a repository %s = %d %s, want 404 NOT_FOUND", name, code, body)
		}
		if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/"+id, `{"enabled":false}`); code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
			t.Errorf("pause of a repository %s = %d %s, want 404 NOT_FOUND", name, code, body)
		}
	}
	if !repositoryEnabled(t, s, "repo-1") {
		t.Error("a refused pause changed repo-1")
	}
}

func TestRepositoryPatchRefusesWhatItDoesNotApply(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	for name, body := range map[string]string{
		"an unknown field":               `{"enabled":false,"fullName":"x/y"}`,
		"no field":                       `{}`,
		"a null enabled":                 `{"enabled":null}`,
		"a non-boolean enabled":          `{"enabled":"no"}`,
		"an empty ticket project id":     `{"ticketProjectId":""}`,
		"a non-string ticket project id": `{"ticketProjectId":7}`,
		"a body that is no object":       `[true]`,
		"no body":                        ``,
	} {
		code, got := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", body)
		if code != http.StatusBadRequest || errorCodeOf(got) != "VALIDATION_ERROR" {
			t.Errorf("PATCH with %s = %d %s, want 400 VALIDATION_ERROR", name, code, got)
		}
	}
	if !repositoryEnabled(t, s, "repo-1") {
		t.Error("a refused PATCH paused repo-1")
	}
}

// seedTicketProject stores a ticket project (and its integration) and returns its id.
func seedTicketProject(t *testing.T, s *rolesServer, name string) string {
	t.Helper()
	base := "https://kaneo.example.com"
	integ, err := s.deps.Store.Integrations().Create(t.Context(), store.CreateIntegrationInput{Kind: "kaneo", Name: "Kaneo " + name, BaseURL: &base, Secret: []byte(`{"api_key":"k"}`)})
	if err != nil {
		t.Fatalf("create integration: %v", err)
	}
	tp, err := s.deps.Store.TicketProjects().Create(t.Context(), store.CreateTicketProjectInput{
		IntegrationID: integ.ID, ExternalProjectID: name, Name: name, CreateConfig: `{}`, StatusMapping: `{}`,
	})
	if err != nil {
		t.Fatalf("create ticket project: %v", err)
	}
	return tp.ID
}

type listedRepository struct {
	ID                         string  `json:"id"`
	FullName                   string  `json:"fullName"`
	TicketProjectID            *string `json:"ticketProjectId"`
	EffectiveTicketProjectID   *string `json:"effectiveTicketProjectId"`
	EffectiveTicketProjectName *string `json:"effectiveTicketProjectName"`
}

func listRepositories(t *testing.T, s *rolesServer, c *http.Client, query string) (items []listedRepository, total int) {
	t.Helper()
	code, body := s.send(t, c, http.MethodGet, "/api/v1/repositories"+query, "")
	if code != http.StatusOK {
		t.Fatalf("GET /repositories%s = %d %s", query, code, body)
	}
	var out struct {
		Items []listedRepository `json:"items"`
		Total int                `json:"total"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out.Items, out.Total
}

func TestTheRepositoryOverrideIsSetClearedAndShownWithTheEffectiveProject(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	dev, ops := seedTicketProject(t, s, "DEV"), seedTicketProject(t, s, "OPS")

	items, total := listRepositories(t, s, admin, "")
	if total != 2 || len(items) != 2 || items[0].FullName != "acme/repo-1" {
		t.Fatalf("list = %+v (%d), want the two repositories by name", items, total)
	}
	for _, r := range items {
		if r.TicketProjectID != nil || r.EffectiveTicketProjectID != nil || r.EffectiveTicketProjectName != nil {
			t.Errorf("%s = %+v, want no ticket project while neither an override nor a default is set", r.FullName, r)
		}
	}

	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"`+dev+`"}`); code != http.StatusOK {
		t.Fatalf("set default = %d %s", code, body)
	}
	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-2", `{"ticketProjectId":"`+ops+`"}`)
	if code != http.StatusOK {
		t.Fatalf("set override = %d %s", code, body)
	}
	var patched listedRepository
	if err := json.Unmarshal([]byte(body), &patched); err != nil || patched.TicketProjectID == nil || *patched.TicketProjectID != ops || *patched.EffectiveTicketProjectName != "OPS" {
		t.Fatalf("PATCH response = %s (%v), want the override OPS", body, err)
	}

	items, _ = listRepositories(t, s, admin, "")
	byName := map[string]listedRepository{}
	for _, r := range items {
		byName[r.FullName] = r
	}
	if r := byName["acme/repo-1"]; r.TicketProjectID != nil || r.EffectiveTicketProjectID == nil || *r.EffectiveTicketProjectID != dev || *r.EffectiveTicketProjectName != "DEV" {
		t.Errorf("repo-1 = %+v, want no override and the default DEV in effect", r)
	}
	if r := byName["acme/repo-2"]; r.TicketProjectID == nil || *r.TicketProjectID != ops || *r.EffectiveTicketProjectID != ops {
		t.Errorf("repo-2 = %+v, want the override OPS in effect", r)
	}

	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-2", `{"ticketProjectId":null}`); code != http.StatusOK {
		t.Fatalf("clear override = %d %s", code, body)
	}
	items, _ = listRepositories(t, s, admin, "?search=REPO-2")
	if len(items) != 1 || items[0].TicketProjectID != nil || *items[0].EffectiveTicketProjectID != dev {
		t.Fatalf("after clearing = %+v, want repo-2 back on the default", items)
	}
}

func TestAnUnknownTicketProjectIsRefusedAndChangesNothing(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)

	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false,"ticketProjectId":"no-such-project"}`)
	if code != http.StatusUnprocessableEntity || errorCodeOf(body) != "VALIDATION_ERROR" {
		t.Fatalf("PATCH = %d %s, want 422 VALIDATION_ERROR", code, body)
	}
	if !repositoryEnabled(t, s, "repo-1") {
		t.Error("the refused PATCH still paused the repository")
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM repositories WHERE ticket_project_id IS NOT NULL`); n != 0 {
		t.Errorf("%d repositories have an override", n)
	}

	code, body = s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"no-such-project"}`)
	if code != http.StatusUnprocessableEntity || errorCodeOf(body) != "VALIDATION_ERROR" {
		t.Fatalf("PATCH installation = %d %s, want 422", code, body)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM installation_settings WHERE default_ticket_project_id IS NOT NULL`); n != 0 {
		t.Errorf("the refused PATCH stored a default")
	}
}

func TestPauseAndOverrideInOneRequestApplyTogether(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	dev := seedTicketProject(t, s, "DEV")

	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false,"ticketProjectId":"`+dev+`"}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", code, body)
	}
	if repositoryEnabled(t, s, "repo-1") {
		t.Error("repo-1 is still enabled")
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM repositories WHERE id = 'repo-1' AND ticket_project_id IS NOT NULL`); n != 1 {
		t.Error("the override was not stored")
	}
}

func TestTheInstallationDefaultTicketProjectIsReadSetAndCleared(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	dev := seedTicketProject(t, s, "DEV")

	type installation struct {
		ID                     string  `json:"id"`
		AccountLogin           string  `json:"accountLogin"`
		Status                 string  `json:"status"`
		RepositoryCount        int     `json:"repositoryCount"`
		DefaultTicketProjectID *string `json:"defaultTicketProjectId"`
	}
	read := func() installation {
		code, body := s.send(t, admin, http.MethodGet, "/api/v1/installations", "")
		var out struct {
			Items []installation `json:"items"`
		}
		if err := json.Unmarshal([]byte(body), &out); code != http.StatusOK || err != nil || len(out.Items) != 1 {
			t.Fatalf("GET /installations = %d %s (%v)", code, body, err)
		}
		return out.Items[0]
	}
	if i := read(); i.ID != "inst-1" || i.AccountLogin != "acme" || i.Status != "active" || i.RepositoryCount != 2 || i.DefaultTicketProjectID != nil {
		t.Fatalf("installation = %+v, want inst-1 with two repositories and no default", i)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM installation_settings`); n != 0 {
		t.Fatalf("settings rows = %d, want none before the first write", n)
	}

	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"`+dev+`"}`)
	if code != http.StatusOK {
		t.Fatalf("set = %d %s", code, body)
	}
	if i := read(); i.DefaultTicketProjectID == nil || *i.DefaultTicketProjectID != dev {
		t.Fatalf("installation = %+v, want the default stored", i)
	}
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":null}`); code != http.StatusOK {
		t.Fatalf("clear = %d %s", code, body)
	}
	if i := read(); i.DefaultTicketProjectID != nil {
		t.Fatalf("installation = %+v, want the default cleared", i)
	}

	for name, req := range map[string]struct{ path, body string }{
		"no key":      {"/api/v1/installations/inst-1", `{}`},
		"a number":    {"/api/v1/installations/inst-1", `{"defaultTicketProjectId":3}`},
		"empty":       {"/api/v1/installations/inst-1", `{"defaultTicketProjectId":""}`},
		"other field": {"/api/v1/installations/inst-1", `{"defaultTicketProjectId":null,"status":"removed"}`},
	} {
		if code, body := s.send(t, admin, http.MethodPatch, req.path, req.body); code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("PATCH with %s = %d %s, want 400 VALIDATION_ERROR", name, code, body)
		}
	}
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/no-such", `{"defaultTicketProjectId":"`+dev+`"}`); code != http.StatusNotFound || errorCodeOf(body) != "NOT_FOUND" {
		t.Errorf("PATCH of an unknown installation = %d %s, want 404", code, body)
	}
	if _, err := s.deps.DB.Exec(`UPDATE installations SET status = 'removed'`); err != nil {
		t.Fatal(err)
	}
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":"`+dev+`"}`); code != http.StatusNotFound {
		t.Errorf("PATCH of a removed installation = %d %s, want 404", code, body)
	}
}

func TestTheRepositoryListFiltersAndPages(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	admin := s.signIn(t)
	if _, err := s.deps.DB.Exec(`UPDATE repositories SET enabled = 0 WHERE id = 'repo-2'`); err != nil {
		t.Fatal(err)
	}

	if items, total := listRepositories(t, s, admin, "?enabled=false"); total != 1 || len(items) != 1 || items[0].ID != "repo-2" {
		t.Errorf("enabled=false = %+v (%d)", items, total)
	}
	if items, total := listRepositories(t, s, admin, "?enabled=true"); total != 1 || items[0].ID != "repo-1" {
		t.Errorf("enabled=true = %+v (%d)", items, total)
	}
	if items, total := listRepositories(t, s, admin, "?page=2&pageSize=1"); total != 2 || len(items) != 1 || items[0].ID != "repo-2" {
		t.Errorf("page 2 = %+v (%d), want the second repository of two", items, total)
	}
	if items, total := listRepositories(t, s, admin, "?configState=present"); total != 0 || len(items) != 0 {
		t.Errorf("configState=present = %+v (%d), want none", items, total)
	}
	for _, q := range []string{"?enabled=maybe", "?configState=odd", "?page=0", "?pageSize=101", "?pageSize=x"} {
		if code, body := s.send(t, admin, http.MethodGet, "/api/v1/repositories"+q, ""); code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("GET /repositories%s = %d %s, want 400 VALIDATION_ERROR", q, code, body)
		}
	}
}

func TestRepositoryActionsAreRefusedToAViewerAndToAnAnonymousCaller(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/repositories/repo-1/run", ""},
		{http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false}`},
		{http.MethodPatch, "/api/v1/repositories/repo-1", `{"ticketProjectId":null}`},
		{http.MethodPost, "/api/v1/repositories/repo-1/enable-renovate", ""},
		{http.MethodGet, "/api/v1/installations", ""},
		{http.MethodPatch, "/api/v1/installations/inst-1", `{"defaultTicketProjectId":null}`},
	} {
		if code, body := s.send(t, viewer, req.method, req.path, req.body); code != http.StatusForbidden || errorCodeOf(body) != "FORBIDDEN" {
			t.Errorf("viewer %s %s = %d %s, want 403 FORBIDDEN", req.method, req.path, code, body)
		}
		if code, body := s.send(t, noRedirects(t), req.method, req.path, req.body); code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d %s, want 401", req.method, req.path, code, body)
		}
	}
	if n := jobCount(t, s); n != 0 {
		t.Errorf("refused calls left %d jobs", n)
	}
	if !repositoryEnabled(t, s, "repo-1") {
		t.Error("a refused PATCH paused repo-1")
	}
}
