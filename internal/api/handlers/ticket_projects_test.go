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

const (
	validCreateConfig       = `{"issueType":"Task","priority":"Medium"}`
	validStatusMapping      = `{"open":["To Do","In Progress"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`
	validContentTemplates   = `{"title":"","description":"","supersedeComment":""}`
	customContentTemplates  = `{"title":"Release: {{ .Release.Tag }}","description":"Notes","supersedeComment":"Superseded"}`
)

type mockTicketProjectRepo struct {
	items       map[string]*store.TicketProject
	references  map[string]int64
	createInput *store.CreateTicketProjectInput
	updateInput *store.UpdateTicketProjectInput
	createErr   error
	listErr     error
	listByErr   error
	getErr      error
	updateErr   error
	deleteErr   error
	countErr    error
}

func (m *mockTicketProjectRepo) Create(_ context.Context, input store.CreateTicketProjectInput) (*store.TicketProject, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createInput = &input
	if m.items == nil {
		m.items = make(map[string]*store.TicketProject)
	}

	for _, item := range m.items {
		if item.IntegrationID == input.IntegrationID && item.ExternalProjectID == input.ExternalProjectID {
			return nil, fmt.Errorf("UNIQUE constraint failed: ticket_projects.integration_id, ticket_projects.external_project_id")
		}
	}

	policy := input.OnOpenTicketPolicy
	if policy == "" {
		policy = "supersede"
	}

	id := fmt.Sprintf("tp-%d", len(m.items)+1)
	item := &store.TicketProject{
		ID:                 id,
		IntegrationID:      input.IntegrationID,
		ExternalProjectID:  input.ExternalProjectID,
		Name:               input.Name,
		CreateConfig:       input.CreateConfig,
		StatusMapping:      input.StatusMapping,
		ContentTemplates:   input.ContentTemplates,
		OnOpenTicketPolicy: policy,
		CreatedAt:          "2026-08-07T10:00:00.000Z",
		UpdatedAt:          "2026-08-07T10:00:00.000Z",
	}
	m.items[id] = item
	return item, nil
}

func (m *mockTicketProjectRepo) Get(_ context.Context, id string) (*store.TicketProject, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (m *mockTicketProjectRepo) List(_ context.Context) ([]store.TicketProject, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]store.TicketProject, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockTicketProjectRepo) ListByIntegration(_ context.Context, integrationID string) ([]store.TicketProject, error) {
	if m.listByErr != nil {
		return nil, m.listByErr
	}
	all, err := m.List(context.Background())
	if err != nil {
		return nil, err
	}
	var out []store.TicketProject
	for _, item := range all {
		if item.IntegrationID == integrationID {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *mockTicketProjectRepo) Update(_ context.Context, id string, input store.UpdateTicketProjectInput) (*store.TicketProject, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	m.updateInput = &input
	item.Name = input.Name
	item.CreateConfig = input.CreateConfig
	item.StatusMapping = input.StatusMapping
	item.ContentTemplates = input.ContentTemplates
	item.OnOpenTicketPolicy = input.OnOpenTicketPolicy
	item.UpdatedAt = "2026-08-07T12:00:00.000Z"
	return item, nil
}

func (m *mockTicketProjectRepo) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.items, id)
	return nil
}

func (m *mockTicketProjectRepo) CountMonitoredRepos(_ context.Context, id string) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.references[id], nil
}

func newTicketProjectsTestRouter(t *testing.T, repo store.TicketProjectRepository) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName
	h := &handlers.TicketProjectHandlers{TicketProjects: repo}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/ticket-projects", h.List)
		protected.Post("/api/v1/ticket-projects", h.Create)
		protected.Patch("/api/v1/ticket-projects/{id}", h.Patch)
		protected.Delete("/api/v1/ticket-projects/{id}", h.Delete)
	})
	return r, sm
}

func ticketProjectResponseShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	for _, key := range []string{
		"id", "integrationId", "externalProjectId", "name",
		"createConfig", "statusMapping", "contentTemplates", "onOpenTicketPolicy", "createdAt", "updatedAt",
	} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("response missing %q: %s", key, body)
		}
	}
	return resp
}

func TestListTicketProjects(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Jira DEV",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
			"tp-2": {
				ID:                 "tp-2",
				IntegrationID:      "int-2",
				ExternalProjectID:  "OPS",
				Name:               "Jira OPS",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "merge",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ticket-projects", nil)
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
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
}

func TestListTicketProjectsFilteredByIntegrationID(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Jira DEV",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
			"tp-2": {
				ID:                 "tp-2",
				IntegrationID:      "int-2",
				ExternalProjectID:  "OPS",
				Name:               "Jira OPS",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "merge",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ticket-projects?integrationId=int-1", nil)
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
	if items[0]["integrationId"] != "int-1" {
		t.Fatalf("integrationId = %v, want int-1", items[0]["integrationId"])
	}
}

func TestCreateTicketProjectPassesFieldsToStore(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Jira DEV",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":%s,
		"onOpenTicketPolicy":"skip_if_open"
	}`, validCreateConfig, validStatusMapping, validContentTemplates)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil {
		t.Fatal("expected Create to be called")
	}
	if repo.createInput.IntegrationID != "int-1" {
		t.Fatalf("integrationId = %q, want int-1", repo.createInput.IntegrationID)
	}
	if repo.createInput.ExternalProjectID != "DEV" {
		t.Fatalf("externalProjectId = %q, want DEV", repo.createInput.ExternalProjectID)
	}
	if repo.createInput.OnOpenTicketPolicy != "skip_if_open" {
		t.Fatalf("policy = %q, want skip_if_open", repo.createInput.OnOpenTicketPolicy)
	}
	if repo.createInput.ContentTemplates != validContentTemplates {
		t.Fatalf("contentTemplates = %q, want %q", repo.createInput.ContentTemplates, validContentTemplates)
	}

	resp := ticketProjectResponseShape(t, rec.Body.Bytes())
	if resp["onOpenTicketPolicy"] != "skip_if_open" {
		t.Fatalf("onOpenTicketPolicy = %v, want skip_if_open", resp["onOpenTicketPolicy"])
	}
}

func TestCreateTicketProjectDefaultsPolicyToSupersede(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Jira DEV",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":%s
	}`, validCreateConfig, validStatusMapping, validContentTemplates)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil || repo.createInput.OnOpenTicketPolicy != "supersede" {
		t.Fatalf("createInput policy = %+v, want supersede", repo.createInput)
	}
}

func TestCreateTicketProjectAcceptsAllPolicies(t *testing.T) {
	t.Parallel()

	policies := []string{"supersede", "merge", "skip_if_open"}
	for _, policy := range policies {
		policy := policy
		t.Run(policy, func(t *testing.T) {
			t.Parallel()

			repo := &mockTicketProjectRepo{}
			router, sm := newTicketProjectsTestRouter(t, repo)
			cookie := seedSession(t, sm)

			body := fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":%q,
				"name":"Policy Test",
				"createConfig":%s,
				"statusMapping":%s,
				"contentTemplates":%s,
				"onOpenTicketPolicy":%q
			}`, policy, validCreateConfig, validStatusMapping, validContentTemplates, policy)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
			}
		})
	}
}

func TestCreateTicketProjectRejectsInvalidJSONConfigs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "invalid createConfig syntax",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":{bad},
				"statusMapping":%s,
				"contentTemplates":%s
			}`, validStatusMapping, validContentTemplates),
		},
		{
			name: "createConfig not an object",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":"not-an-object",
				"statusMapping":%s,
				"contentTemplates":%s
			}`, validStatusMapping, validContentTemplates),
		},
		{
			name: "statusMapping not an object",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":%s,
				"statusMapping":"not-an-object",
				"contentTemplates":%s
			}`, validCreateConfig, validContentTemplates),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockTicketProjectRepo{}
			router, sm := newTicketProjectsTestRouter(t, repo)
			cookie := seedSession(t, sm)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(tc.body))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"VALIDATION_ERROR"`) {
				t.Fatalf("body = %s, want VALIDATION_ERROR", rec.Body.String())
			}
		})
	}
}

func TestCreateTicketProjectRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Jira DEV",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":%s,
		"onOpenTicketPolicy":"replace"
	}`, validCreateConfig, validStatusMapping, validContentTemplates)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateTicketProjectConflictOnDuplicateExternalProject(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Existing",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Duplicate",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":%s
	}`, validCreateConfig, validStatusMapping, validContentTemplates)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
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

func TestPatchTicketProjectUpdatesFields(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Old Name",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	newCreateConfig := `{"issueType":"Bug"}`
	newStatusMapping := `{"open":["Open"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`
	body := fmt.Sprintf(`{
		"name":"Updated Name",
		"createConfig":%s,
		"statusMapping":%s,
		"onOpenTicketPolicy":"merge"
	}`, newCreateConfig, newStatusMapping)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/tp-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil {
		t.Fatal("expected Update to be called")
	}
	if repo.updateInput.Name != "Updated Name" {
		t.Fatalf("name = %q, want Updated Name", repo.updateInput.Name)
	}
	if repo.updateInput.OnOpenTicketPolicy != "merge" {
		t.Fatalf("policy = %q, want merge", repo.updateInput.OnOpenTicketPolicy)
	}
	if repo.updateInput.ContentTemplates != validContentTemplates {
		t.Fatalf("contentTemplates = %q, want unchanged %q", repo.updateInput.ContentTemplates, validContentTemplates)
	}
}

func TestPatchTicketProjectNotFound(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"name":"Updated Name",
		"createConfig":%s,
		"statusMapping":%s,
		"onOpenTicketPolicy":"merge"
	}`, validCreateConfig, validStatusMapping)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/missing", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteTicketProjectConflictWhenReferenced(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Jira DEV",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
		references: map[string]int64{
			"tp-1": 1,
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ticket-projects/tp-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"CONFLICT"`) {
		t.Fatalf("body = %s, want CONFLICT", rec.Body.String())
	}
	if _, ok := repo.items["tp-1"]; !ok {
		t.Fatal("ticket project should not be deleted on conflict")
	}
}

func TestDeleteTicketProjectSucceedsWhenUnreferenced(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Jira DEV",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   validContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
		references: map[string]int64{
			"tp-1": 0,
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/ticket-projects/tp-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, ok := repo.items["tp-1"]; ok {
		t.Fatal("ticket project should be deleted")
	}
}

func TestTicketProjectsRequireSession(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, _ := newTicketProjectsTestRouter(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ticket-projects", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCreateTicketProjectAcceptsEmptyContentTemplateStrings(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Jira DEV",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":%s
	}`, validCreateConfig, validStatusMapping, validContentTemplates)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	resp := ticketProjectResponseShape(t, rec.Body.Bytes())
	ct, ok := resp["contentTemplates"].(map[string]any)
	if !ok {
		t.Fatalf("contentTemplates = %T, want map", resp["contentTemplates"])
	}
	if ct["title"] != "" || ct["description"] != "" || ct["supersedeComment"] != "" {
		t.Fatalf("contentTemplates = %v, want empty strings", ct)
	}
}

func TestCreateTicketProjectRejectsMissingContentTemplates(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"integrationId":"int-1",
		"externalProjectId":"DEV",
		"name":"Jira DEV",
		"createConfig":%s,
		"statusMapping":%s
	}`, validCreateConfig, validStatusMapping)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestCreateTicketProjectRejectsInvalidContentTemplates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "missing title key",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":%s,
				"statusMapping":%s,
				"contentTemplates":{"description":"","supersedeComment":""}
			}`, validCreateConfig, validStatusMapping),
		},
		{
			name: "title not a string",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":%s,
				"statusMapping":%s,
				"contentTemplates":{"title":1,"description":"","supersedeComment":""}
			}`, validCreateConfig, validStatusMapping),
		},
		{
			name: "contentTemplates not an object",
			body: fmt.Sprintf(`{
				"integrationId":"int-1",
				"externalProjectId":"DEV",
				"name":"Jira DEV",
				"createConfig":%s,
				"statusMapping":%s,
				"contentTemplates":"not-an-object"
			}`, validCreateConfig, validStatusMapping),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &mockTicketProjectRepo{}
			router, sm := newTicketProjectsTestRouter(t, repo)
			cookie := seedSession(t, sm)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(tc.body))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestPatchTicketProjectMergesPartialContentTemplates(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Old Name",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   customContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"name":"Updated Name",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":{"title":"New title"},
		"onOpenTicketPolicy":"merge"
	}`, validCreateConfig, validStatusMapping)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/tp-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil {
		t.Fatal("expected Update to be called")
	}

	expected := `{"title":"New title","description":"Notes","supersedeComment":"Superseded"}`
	if repo.updateInput.ContentTemplates != expected {
		t.Fatalf("contentTemplates = %q, want %q", repo.updateInput.ContentTemplates, expected)
	}

	resp := ticketProjectResponseShape(t, rec.Body.Bytes())
	ct, ok := resp["contentTemplates"].(map[string]any)
	if !ok {
		t.Fatalf("contentTemplates = %T, want map", resp["contentTemplates"])
	}
	if ct["title"] != "New title" {
		t.Fatalf("title = %v, want New title", ct["title"])
	}
	if ct["description"] != "Notes" {
		t.Fatalf("description = %v, want Notes", ct["description"])
	}
}

func TestListTicketProjectsIncludesContentTemplates(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                 "tp-1",
				IntegrationID:      "int-1",
				ExternalProjectID:  "DEV",
				Name:               "Jira DEV",
				CreateConfig:       validCreateConfig,
				StatusMapping:      validStatusMapping,
				ContentTemplates:   customContentTemplates,
				OnOpenTicketPolicy: "supersede",
				CreatedAt:          "2026-08-07T10:00:00.000Z",
				UpdatedAt:          "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ticket-projects", nil)
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
	ct, ok := items[0]["contentTemplates"].(map[string]any)
	if !ok {
		t.Fatalf("contentTemplates = %T, want map", items[0]["contentTemplates"])
	}
	if ct["title"] != "Release: {{ .Release.Tag }}" {
		t.Fatalf("title = %v, want template string", ct["title"])
	}
}
