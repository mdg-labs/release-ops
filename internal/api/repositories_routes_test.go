package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
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
		"a ticket project":         `{"enabled":false,"ticketProjectId":"some-project"}`,
		"a cleared ticket project": `{"enabled":false,"ticketProjectId":null}`,
		"a ticket project alone":   `{"ticketProjectId":null}`,
		"an unknown field":         `{"enabled":false,"fullName":"x/y"}`,
		"no enabled":               `{}`,
		"a null enabled":           `{"enabled":null}`,
		"a non-boolean enabled":    `{"enabled":"no"}`,
		"a body that is no object": `[true]`,
		"no body":                  ``,
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

func TestRepositoryActionsAreRefusedToAViewerAndToAnAnonymousCaller(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	for _, req := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/repositories/repo-1/run", ""},
		{http.MethodPatch, "/api/v1/repositories/repo-1", `{"enabled":false}`},
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
