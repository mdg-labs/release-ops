package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/store"
)

type mockRepoRepo struct {
	items       map[string]*store.MonitoredRepo
	createInput *store.CreateMonitoredRepoInput
	updateInput *store.UpdateMonitoredRepoInput
	createErr   error
	listErr     error
	getErr      error
	updateErr   error
	deleteErr   error
}

func (m *mockRepoRepo) Create(_ context.Context, input store.CreateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createInput = &input
	if m.items == nil {
		m.items = make(map[string]*store.MonitoredRepo)
	}

	for _, item := range m.items {
		if item.SourceKind == input.SourceKind && item.ProjectPath == input.ProjectPath {
			return nil, fmt.Errorf("UNIQUE constraint failed: monitored_repos.source_kind, monitored_repos.project_path")
		}
	}

	id := fmt.Sprintf("repo-%d", len(m.items)+1)
	notificationTargetIDs := append([]string(nil), input.NotificationTargetIDs...)
	item := &store.MonitoredRepo{
		ID:                    id,
		SourceKind:            input.SourceKind,
		ProjectPath:           input.ProjectPath,
		Enabled:               input.Enabled,
		SourceIntegrationID:   input.SourceIntegrationID,
		TicketProjectID:       input.TicketProjectID,
		NotificationTargetIDs: notificationTargetIDs,
		CreatedAt:             "2026-08-07T10:00:00.000Z",
		UpdatedAt:             "2026-08-07T10:00:00.000Z",
	}
	m.items[id] = item
	return item, nil
}

func (m *mockRepoRepo) Get(_ context.Context, id string) (*store.MonitoredRepo, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (m *mockRepoRepo) List(_ context.Context) ([]store.MonitoredRepo, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]store.MonitoredRepo, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockRepoRepo) ListEnabled(_ context.Context) ([]store.MonitoredRepo, error) {
	all, err := m.List(context.Background())
	if err != nil {
		return nil, err
	}
	var out []store.MonitoredRepo
	for _, item := range all {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *mockRepoRepo) Update(_ context.Context, id string, input store.UpdateMonitoredRepoInput) (*store.MonitoredRepo, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}

	for otherID, other := range m.items {
		if otherID != id && other.SourceKind == input.SourceKind && other.ProjectPath == input.ProjectPath {
			return nil, fmt.Errorf("UNIQUE constraint failed: monitored_repos.source_kind, monitored_repos.project_path")
		}
	}

	m.updateInput = &input
	item.SourceKind = input.SourceKind
	item.ProjectPath = input.ProjectPath
	item.Enabled = input.Enabled
	item.SourceIntegrationID = input.SourceIntegrationID
	item.TicketProjectID = input.TicketProjectID
	item.NotificationTargetIDs = append([]string(nil), input.NotificationTargetIDs...)
	item.UpdatedAt = "2026-08-07T12:00:00.000Z"
	return item, nil
}

func (m *mockRepoRepo) SetEnabled(_ context.Context, id string, enabled bool) (*store.MonitoredRepo, error) {
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	item.Enabled = enabled
	item.UpdatedAt = "2026-08-07T12:00:00.000Z"
	return item, nil
}

func (m *mockRepoRepo) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.items, id)
	return nil
}

func newReposTestRouter(t *testing.T, repo store.MonitoredRepoRepository) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName
	h := &handlers.RepoHandlers{Repos: repo}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/repos", h.List)
		protected.Post("/api/v1/repos", h.Create)
		protected.Patch("/api/v1/repos/{id}", h.Patch)
		protected.Delete("/api/v1/repos/{id}", h.Delete)
	})
	return r, sm
}

func repoResponseShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	for _, key := range []string{
		"id", "sourceKind", "projectPath", "enabled", "sourceIntegrationId",
		"ticketProjectId", "notificationTargetIds", "openTicketExternalId", "openTicketTag",
		"lastKnownTag", "lastPolledAt", "lastError", "createdAt", "updatedAt",
	} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("response missing %q: %s", key, body)
		}
	}
	return resp
}

func TestListRepos(t *testing.T) {
	t.Parallel()

	openTicketID := "TICKET-1"
	openTicketTag := "v1.0.0"
	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:                   "repo-1",
				SourceKind:           "github",
				ProjectPath:          "org/repo",
				Enabled:              true,
				TicketProjectID:      "tp-1",
				NotificationTargetIDs: []string{"nt-1"},
				OpenTicketExternalID: &openTicketID,
				OpenTicketTag:        &openTicketTag,
				CreatedAt:            "2026-08-07T10:00:00.000Z",
				UpdatedAt:            "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var items []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0]["openTicketExternalId"] != "TICKET-1" {
		t.Fatalf("openTicketExternalId = %v, want TICKET-1", items[0]["openTicketExternalId"])
	}
}

func TestCreateRepoPassesFieldsToStore(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"FreshRSS/FreshRSS",
		"enabled":true,
		"ticketProjectId":"tp-1",
		"notificationTargetIds":["nt-1","nt-2"]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil {
		t.Fatal("expected Create to be called")
	}
	if repo.createInput.TicketProjectID != "tp-1" {
		t.Fatalf("ticketProjectId = %q, want tp-1", repo.createInput.TicketProjectID)
	}
	if len(repo.createInput.NotificationTargetIDs) != 2 {
		t.Fatalf("notificationTargetIds = %v, want 2 items", repo.createInput.NotificationTargetIDs)
	}
	if !repo.createInput.Enabled {
		t.Fatal("enabled = false, want true")
	}

	resp := repoResponseShape(t, rec.Body.Bytes())
	if _, ok := resp["openTicketExternalId"]; !ok {
		t.Fatal("response missing openTicketExternalId")
	}
}

func TestCreateRepoDefaultsEnabledToTrue(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/repo",
		"ticketProjectId":"tp-1"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil || !repo.createInput.Enabled {
		t.Fatalf("createInput enabled = %+v, want true", repo.createInput)
	}
}

func TestCreateRepoRejectsMissingTicketProjectID(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"sourceKind":"github","projectPath":"org/repo","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"VALIDATION_ERROR"`) {
		t.Fatalf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
}

func TestCreateRepoRequiresSourceIntegrationForSelfHostedKinds(t *testing.T) {
	t.Parallel()

	selfHostedKinds := []string{"gitlab", "gitea", "forgejo"}
	for _, kind := range selfHostedKinds {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			repo := &mockRepoRepo{}
			router, sm := newReposTestRouter(t, repo)
			cookie := seedSession(t, sm)

			body := fmt.Sprintf(`{
				"sourceKind":%q,
				"projectPath":"org/repo",
				"enabled":true,
				"ticketProjectId":"tp-1"
			}`, kind)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "sourceIntegrationId is required") {
				t.Fatalf("body = %s, want sourceIntegrationId required message", rec.Body.String())
			}
		})
	}
}

func TestCreateRepoAcceptsSourceIntegrationForSelfHostedKinds(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"gitlab",
		"projectPath":"namespace/project",
		"enabled":true,
		"sourceIntegrationId":"int-1",
		"ticketProjectId":"tp-1"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil || repo.createInput.SourceIntegrationID == nil || *repo.createInput.SourceIntegrationID != "int-1" {
		t.Fatalf("createInput sourceIntegrationId = %+v, want int-1", repo.createInput)
	}
}

func TestCreateRepoConflictOnDuplicateSourceKindProjectPath(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:              "repo-1",
				SourceKind:      "github",
				ProjectPath:     "org/repo",
				Enabled:         true,
				TicketProjectID: "tp-1",
				CreatedAt:       "2026-08-07T10:00:00.000Z",
				UpdatedAt:       "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/repo",
		"enabled":true,
		"ticketProjectId":"tp-2"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"CONFLICT"`) {
		t.Fatalf("body = %s, want CONFLICT", rec.Body.String())
	}
}

func TestPatchRepoUpdatesFieldsAndNotificationTargets(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:                    "repo-1",
				SourceKind:            "github",
				ProjectPath:           "org/old",
				Enabled:               true,
				TicketProjectID:       "tp-1",
				NotificationTargetIDs: []string{"nt-1"},
				CreatedAt:             "2026-08-07T10:00:00.000Z",
				UpdatedAt:             "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/new",
		"enabled":false,
		"ticketProjectId":"tp-2",
		"notificationTargetIds":["nt-2","nt-3"]
	}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/repos/repo-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil {
		t.Fatal("expected Update to be called")
	}
	if repo.updateInput.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if len(repo.updateInput.NotificationTargetIDs) != 2 {
		t.Fatalf("notificationTargetIds = %v, want 2 items", repo.updateInput.NotificationTargetIDs)
	}

	resp := repoResponseShape(t, rec.Body.Bytes())
	if resp["enabled"] != false {
		t.Fatalf("enabled = %v, want false", resp["enabled"])
	}
}

func TestPatchRepoIgnoresReadOnlyOpenTicketFields(t *testing.T) {
	t.Parallel()

	openTicketID := "TICKET-KEEP"
	openTicketTag := "v1.0.0"
	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:                   "repo-1",
				SourceKind:           "github",
				ProjectPath:          "org/repo",
				Enabled:              true,
				TicketProjectID:      "tp-1",
				OpenTicketExternalID: &openTicketID,
				OpenTicketTag:        &openTicketTag,
				CreatedAt:            "2026-08-07T10:00:00.000Z",
				UpdatedAt:            "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/repo",
		"enabled":true,
		"ticketProjectId":"tp-1",
		"openTicketExternalId":"TICKET-OVERRIDE",
		"openTicketTag":"v9.9.9"
	}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/repos/repo-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := repoResponseShape(t, rec.Body.Bytes())
	if resp["openTicketExternalId"] != "TICKET-KEEP" {
		t.Fatalf("openTicketExternalId = %v, want TICKET-KEEP", resp["openTicketExternalId"])
	}
	if resp["openTicketTag"] != "v1.0.0" {
		t.Fatalf("openTicketTag = %v, want v1.0.0", resp["openTicketTag"])
	}
}

func TestPatchRepoNotFound(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/repo",
		"enabled":true,
		"ticketProjectId":"tp-1"
	}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/repos/missing", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestPatchRepoConflictOnDuplicateSourceKindProjectPath(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:              "repo-1",
				SourceKind:      "github",
				ProjectPath:     "org/one",
				Enabled:         true,
				TicketProjectID: "tp-1",
				CreatedAt:       "2026-08-07T10:00:00.000Z",
				UpdatedAt:       "2026-08-07T10:00:00.000Z",
			},
			"repo-2": {
				ID:              "repo-2",
				SourceKind:      "github",
				ProjectPath:     "org/two",
				Enabled:         true,
				TicketProjectID: "tp-1",
				CreatedAt:       "2026-08-07T10:00:00.000Z",
				UpdatedAt:       "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"github",
		"projectPath":"org/two",
		"enabled":true,
		"ticketProjectId":"tp-1"
	}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/repos/repo-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestDeleteRepoSucceeds(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{
		items: map[string]*store.MonitoredRepo{
			"repo-1": {
				ID:              "repo-1",
				SourceKind:      "github",
				ProjectPath:     "org/repo",
				Enabled:         true,
				TicketProjectID: "tp-1",
				CreatedAt:       "2026-08-07T10:00:00.000Z",
				UpdatedAt:       "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/repo-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, ok := repo.items["repo-1"]; ok {
		t.Fatal("repo should be deleted")
	}
}

func TestDeleteRepoNotFound(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/repos/missing", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestReposRequireSession(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, _ := newReposTestRouter(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/repos", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCreateRepoRejectsInvalidSourceKind(t *testing.T) {
	t.Parallel()

	repo := &mockRepoRepo{}
	router, sm := newReposTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{
		"sourceKind":"bitbucket",
		"projectPath":"org/repo",
		"enabled":true,
		"ticketProjectId":"tp-1"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/repos", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
