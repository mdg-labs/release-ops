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
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	validCreateConfig      = `{"issueType":"Task","priority":"Medium"}`
	validStatusMapping     = `{"open":["To Do","In Progress"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`
	validContentTemplates  = `{"title":"","description":"","supersedeComment":""}`
	customContentTemplates = `{"title":"Bump {{ .PullRequest.Title }}","description":"Notes","updateComment":"Updated","supersedeComment":"Superseded"}`
)

type mockTicketProjectRepo struct {
	items       map[string]*store.TicketProject
	createInput *store.CreateTicketProjectInput
	updateInput *store.UpdateTicketProjectInput
	updated     *store.TicketProject
	createErr   error
	listErr     error
	listByErr   error
	getErr      error
	updateErr   error
	deleteErr   error
}

func defaultFilter(filter string) string {
	if filter == "" {
		return `{"updateTypes":[],"labels":[],"skipAutomerged":true}`
	}
	return filter
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

	id := fmt.Sprintf("tp-%d", len(m.items)+1)
	item := &store.TicketProject{
		ID:                id,
		IntegrationID:     input.IntegrationID,
		ExternalProjectID: input.ExternalProjectID,
		Name:              input.Name,
		CreateConfig:      input.CreateConfig,
		StatusMapping:     input.StatusMapping,
		ContentTemplates:  input.ContentTemplates,
		Filter:            defaultFilter(input.Filter),
		CreatedAt:         "2026-08-07T10:00:00.000Z",
		UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
	if input.ContentTemplates != nil {
		templates, err := input.ContentTemplates(item.ContentTemplates)
		if err != nil {
			return nil, err
		}
		item.ContentTemplates = templates
	}
	m.updateInput = &input
	item.Name = input.Name
	item.CreateConfig = input.CreateConfig
	item.StatusMapping = input.StatusMapping
	if input.Filter != nil {
		item.Filter = defaultFilter(*input.Filter)
	}
	m.updated = item
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
		"createConfig", "statusMapping", "contentTemplates", "filter", "createdAt", "updatedAt",
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Jira DEV",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
			},
			"tp-2": {
				ID:                "tp-2",
				IntegrationID:     "int-2",
				ExternalProjectID: "OPS",
				Name:              "Jira OPS",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Jira DEV",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
			},
			"tp-2": {
				ID:                "tp-2",
				IntegrationID:     "int-2",
				ExternalProjectID: "OPS",
				Name:              "Jira OPS",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
		"contentTemplates":%s
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
	if repo.createInput.ContentTemplates != validContentTemplates {
		t.Fatalf("contentTemplates = %q, want %q", repo.createInput.ContentTemplates, validContentTemplates)
	}

	if _, present := ticketProjectResponseShape(t, rec.Body.Bytes())["onOpenTicketPolicy"]; present {
		t.Fatalf("response carries onOpenTicketPolicy, want the retired field gone: %s", rec.Body.String())
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

func TestCreateTicketProjectConflictOnDuplicateExternalProject(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Existing",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Old Name",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
		"statusMapping":%s
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
	if repo.updated.ContentTemplates != validContentTemplates {
		t.Fatalf("contentTemplates = %q, want unchanged %q", repo.updated.ContentTemplates, validContentTemplates)
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
		"statusMapping":%s
	}`, validCreateConfig, validStatusMapping)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/missing", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestDeleteTicketProjectConflictWhenStoreReportsInUse(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{
		items: map[string]*store.TicketProject{
			"tp-1": {
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Jira DEV",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
			},
		},
		deleteErr: store.ErrInUse,
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Jira DEV",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  validContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
			},
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Old Name",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  customContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := fmt.Sprintf(`{
		"name":"Updated Name",
		"createConfig":%s,
		"statusMapping":%s,
		"contentTemplates":{"title":"New title"}
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

	expected := `{"title":"New title","description":"Notes","updateComment":"Updated","supersedeComment":"Superseded"}`
	if repo.updated.ContentTemplates != expected {
		t.Fatalf("contentTemplates = %q, want %q", repo.updated.ContentTemplates, expected)
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
				ID:                "tp-1",
				IntegrationID:     "int-1",
				ExternalProjectID: "DEV",
				Name:              "Jira DEV",
				CreateConfig:      validCreateConfig,
				StatusMapping:     validStatusMapping,
				ContentTemplates:  customContentTemplates,
				CreatedAt:         "2026-08-07T10:00:00.000Z",
				UpdatedAt:         "2026-08-07T10:00:00.000Z",
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
	if ct["title"] != "Bump {{ .PullRequest.Title }}" || ct["updateComment"] != "Updated" {
		t.Fatalf("title = %v, want template string", ct["title"])
	}
}

func postTicketProject(t *testing.T, repo *mockTicketProjectRepo, extra string) *httptest.ResponseRecorder {
	t.Helper()
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)
	body := fmt.Sprintf(`{"integrationId":"int-1","externalProjectId":"DEV","name":"Jira DEV","createConfig":%s,"statusMapping":%s%s}`,
		validCreateConfig, validStatusMapping, extra)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ticket-projects", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func patchTicketProject(t *testing.T, repo *mockTicketProjectRepo, extra string) *httptest.ResponseRecorder {
	t.Helper()
	router, sm := newTicketProjectsTestRouter(t, repo)
	cookie := seedSession(t, sm)
	body := fmt.Sprintf(`{"name":"Renamed","createConfig":%s,"statusMapping":%s%s}`, validCreateConfig, validStatusMapping, extra)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/tp-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func repoWithProject(templates, filter string) *mockTicketProjectRepo {
	return &mockTicketProjectRepo{items: map[string]*store.TicketProject{"tp-1": {
		ID: "tp-1", IntegrationID: "int-1", ExternalProjectID: "DEV", Name: "Old", CreateConfig: validCreateConfig, StatusMapping: validStatusMapping,
		ContentTemplates: templates, Filter: filter,
	}}}
}

func TestCreateTicketProjectStoresAndReturnsTheUpdateCommentAndTheFilter(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	rec := postTicketProject(t, repo, `,"contentTemplates":`+customContentTemplates+`,"filter":{"updateTypes":["major","minor"],"labels":["deps"],"skipAutomerged":false}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if repo.createInput.ContentTemplates != customContentTemplates {
		t.Fatalf("contentTemplates = %q", repo.createInput.ContentTemplates)
	}
	if want := `{"updateTypes":["major","minor"],"labels":["deps"],"skipAutomerged":false}`; repo.createInput.Filter != want {
		t.Fatalf("filter = %q, want %q", repo.createInput.Filter, want)
	}
	resp := ticketProjectResponseShape(t, rec.Body.Bytes())
	if ct := resp["contentTemplates"].(map[string]any); ct["updateComment"] != "Updated" {
		t.Fatalf("contentTemplates = %v, want updateComment returned", ct)
	}
	filter := resp["filter"].(map[string]any)
	if filter["skipAutomerged"] != false || len(filter["updateTypes"].([]any)) != 2 {
		t.Fatalf("filter = %v", filter)
	}
}

func TestCreateTicketProjectWithoutAFilterOrAnUpdateCommentUsesTheDefaults(t *testing.T) {
	t.Parallel()

	repo := &mockTicketProjectRepo{}
	rec := postTicketProject(t, repo, `,"contentTemplates":`+validContentTemplates)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if repo.createInput.Filter != "" {
		t.Fatalf("filter = %q, want none passed so the store applies the default", repo.createInput.Filter)
	}
	resp := ticketProjectResponseShape(t, rec.Body.Bytes())
	filter := resp["filter"].(map[string]any)
	if filter["skipAutomerged"] != true || len(filter["updateTypes"].([]any)) != 0 || len(filter["labels"].([]any)) != 0 {
		t.Fatalf("filter = %v, want the default that skips automerged PRs only", filter)
	}
	if ct := resp["contentTemplates"].(map[string]any); ct["updateComment"] != "" {
		t.Fatalf("contentTemplates = %v, want an empty updateComment", ct)
	}
}

func TestCreateTicketProjectRefusesAFilterOutsideSection52(t *testing.T) {
	t.Parallel()

	for name, filter := range map[string]string{
		"an update type outside the five": `{"updateTypes":["other"]}`,
		"a misspelled update type":        `{"updateTypes":["majr"]}`,
		"labels that are not strings":     `{"labels":[1]}`,
		"a non-boolean skipAutomerged":    `{"skipAutomerged":"no"}`,
		"an unknown key":                  `{"authors":["a"]}`,
		"a filter that is no object":      `[]`,
	} {
		repo := &mockTicketProjectRepo{}
		rec := postTicketProject(t, repo, `,"contentTemplates":`+validContentTemplates+`,"filter":`+filter)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
			t.Errorf("%s: status = %d, want 422 VALIDATION_ERROR; body = %s", name, rec.Code, rec.Body.String())
		}
		if repo.createInput != nil {
			t.Errorf("%s: the project was created", name)
		}
	}
}

func TestTemplatesOfTheReleaseContractAreRefusedOnSave(t *testing.T) {
	t.Parallel()

	v1 := `{"title":"Release {{ .Release.Tag }}","description":"","updateComment":"","supersedeComment":""}`
	broken := `{"title":"{{ .PullRequest.Title ","description":"","updateComment":"","supersedeComment":""}`
	badUpdateComment := `{"title":"","description":"","updateComment":"{{ .Previous.Tag }}","supersedeComment":""}`
	for name, templates := range map[string]string{"a release variable": v1, "a syntax error": broken, "a release variable in updateComment": badUpdateComment} {
		repo := &mockTicketProjectRepo{}
		rec := postTicketProject(t, repo, `,"contentTemplates":`+templates)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "VALIDATION_ERROR") {
			t.Errorf("POST with %s: status = %d, want 422 VALIDATION_ERROR; body = %s", name, rec.Code, rec.Body.String())
		}
		if repo.createInput != nil {
			t.Errorf("POST with %s created the project", name)
		}

		existing := repoWithProject(validContentTemplates, "")
		rec = patchTicketProject(t, existing, `,"contentTemplates":`+templates)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("PATCH with %s: status = %d, want 422; body = %s", name, rec.Code, rec.Body.String())
		}
		if existing.updateInput != nil {
			t.Errorf("PATCH with %s updated the project", name)
		}
	}
	rec := postTicketProject(t, &mockTicketProjectRepo{}, `,"contentTemplates":`+v1)
	if !strings.Contains(rec.Body.String(), ".Release.Tag") || !strings.Contains(rec.Body.String(), ".Dependencies") {
		t.Errorf("the message should name the variable and its replacement: %s", rec.Body.String())
	}
}

func TestPatchTicketProjectSetsTheFilterAndKeepsItWhenOmitted(t *testing.T) {
	t.Parallel()

	stored := `{"updateTypes":["patch"],"labels":[],"skipAutomerged":true}`
	repo := repoWithProject(validContentTemplates, stored)
	if rec := patchTicketProject(t, repo, ``); rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if repo.updated.Filter != stored {
		t.Fatalf("filter = %q, want the stored one kept", repo.updated.Filter)
	}

	rec := patchTicketProject(t, repo, `,"filter":{"labels":["security"],"skipAutomerged":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if want := `{"updateTypes":[],"labels":["security"],"skipAutomerged":false}`; repo.updated.Filter != want {
		t.Fatalf("filter = %q, want %q", repo.updated.Filter, want)
	}

	repo = repoWithProject(validContentTemplates, stored)
	if rec := patchTicketProject(t, repo, `,"filter":{"updateTypes":["other"]}`); rec.Code != http.StatusUnprocessableEntity || repo.updateInput != nil {
		t.Fatalf("status = %d, updated = %v, want 422 and nothing stored", rec.Code, repo.updateInput != nil)
	}
}

func TestPatchTicketProjectAcceptsTheUpdateComment(t *testing.T) {
	t.Parallel()

	repo := repoWithProject(`{"title":"","description":"","supersedeComment":"S"}`, "")
	rec := patchTicketProject(t, repo, `,"contentTemplates":{"updateComment":"{{ range .Dependencies }}{{ .Name }}{{ end }}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	want := `{"title":"","description":"","updateComment":"{{ range .Dependencies }}{{ .Name }}{{ end }}","supersedeComment":"S"}`
	if repo.updated.ContentTemplates != want {
		t.Fatalf("contentTemplates = %q, want %q", repo.updated.ContentTemplates, want)
	}
}

// interleavingTicketProjects runs beforeUpdate once, just before the first Update reaches the
// store: whatever the handler has read by then is the state a concurrent request can change.
type interleavingTicketProjects struct {
	store.TicketProjectRepository
	beforeUpdate func()
}

func (r *interleavingTicketProjects) Update(ctx context.Context, id string, input store.UpdateTicketProjectInput) (*store.TicketProject, error) {
	if hook := r.beforeUpdate; hook != nil {
		r.beforeUpdate = nil
		hook()
	}
	return r.TicketProjectRepository.Update(ctx, id, input)
}

func TestConcurrentPatchesDoNotOverwriteFieldsTheyOmit(t *testing.T) {
	t.Parallel()

	const (
		storedTemplates = `{"title":"T","description":"D","updateComment":"","supersedeComment":"S"}`
		storedFilter    = `{"updateTypes":["patch"],"labels":[],"skipAutomerged":true}`
		newFilter       = `{"updateTypes":["major"],"labels":["security"],"skipAutomerged":false}`
	)

	for name, tc := range map[string]struct {
		requestA string // the request that completes last, without the field B changes
		requestB string
		check    func(t *testing.T, got *store.TicketProject)
	}{
		"filter": {
			requestA: `,"contentTemplates":{"description":"from A"}`,
			requestB: `,"filter":` + newFilter,
			check: func(t *testing.T, got *store.TicketProject) {
				if got.Filter != newFilter {
					t.Fatalf("filter = %q, want the one request B saved, %q", got.Filter, newFilter)
				}
				if !strings.Contains(got.ContentTemplates, `"description":"from A"`) {
					t.Fatalf("contentTemplates = %q, want request A's description", got.ContentTemplates)
				}
			},
		},
		"content templates": {
			requestA: `,"contentTemplates":{"description":"from A"}`,
			requestB: `,"contentTemplates":{"title":"from B"}`,
			check: func(t *testing.T, got *store.TicketProject) {
				want := `{"title":"from B","description":"from A","updateComment":"","supersedeComment":"S"}`
				if got.ContentTemplates != want {
					t.Fatalf("contentTemplates = %q, want both requests' keys, %q", got.ContentTemplates, want)
				}
				if got.Filter != storedFilter {
					t.Fatalf("filter = %q, want %q unchanged", got.Filter, storedFilter)
				}
			},
		},
		"templates and filter omitted": {
			requestA: ``,
			requestB: `,"filter":` + newFilter + `,"contentTemplates":{"title":"from B"}`,
			check: func(t *testing.T, got *store.TicketProject) {
				if got.Filter != newFilter || !strings.Contains(got.ContentTemplates, `"title":"from B"`) {
					t.Fatalf("filter = %q, contentTemplates = %q, want request B's values kept", got.Filter, got.ContentTemplates)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cipher, err := crypto.NewCipherFromHex("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatalf("NewCipherFromHex: %v", err)
			}
			st := store.New(storetest.OpenMigrated(t), cipher)
			ctx := context.Background()
			integration, err := st.Integrations().Create(ctx, store.CreateIntegrationInput{
				Kind: "kaneo", Name: "Kaneo", BaseURL: strPtr("https://api.kaneo.example"), Secret: []byte(`{"api_key":"test"}`),
			})
			if err != nil {
				t.Fatalf("Create integration: %v", err)
			}
			project, err := st.TicketProjects().Create(ctx, store.CreateTicketProjectInput{
				IntegrationID: integration.ID, ExternalProjectID: "DEV", Name: "Old",
				CreateConfig: validCreateConfig, StatusMapping: validStatusMapping,
				ContentTemplates: storedTemplates, Filter: storedFilter,
			})
			if err != nil {
				t.Fatalf("Create ticket project: %v", err)
			}

			repo := &interleavingTicketProjects{TicketProjectRepository: st.TicketProjects()}
			router, sm := newTicketProjectsTestRouter(t, repo)
			cookie := seedSession(t, sm)
			patch := func(extra string) int {
				body := fmt.Sprintf(`{"name":"Renamed","createConfig":%s,"statusMapping":%s%s}`, validCreateConfig, validStatusMapping, extra)
				req := httptest.NewRequest(http.MethodPatch, "/api/v1/ticket-projects/"+project.ID, strings.NewReader(body))
				req.AddCookie(cookie)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				return rec.Code
			}
			repo.beforeUpdate = func() {
				if code := patch(tc.requestB); code != http.StatusOK {
					t.Errorf("request B status = %d, want 200", code)
				}
			}

			if code := patch(tc.requestA); code != http.StatusOK {
				t.Fatalf("request A status = %d, want 200", code)
			}
			got, err := st.TicketProjects().Get(ctx, project.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			tc.check(t, got)
		})
	}
}

func strPtr(s string) *string { return &s }
