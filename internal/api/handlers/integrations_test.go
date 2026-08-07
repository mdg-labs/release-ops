package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type mockIntegrationRepo struct {
	items       map[string]*store.Integration
	secrets     map[string][]byte
	references  map[string]int64
	createInput *store.CreateIntegrationInput
	updateInput *store.UpdateIntegrationInput
	createErr   error
	listErr     error
	getErr      error
	updateErr   error
	deleteErr   error
	countErr    error
	decryptErr  error
}

func (m *mockIntegrationRepo) Create(_ context.Context, input store.CreateIntegrationInput) (*store.Integration, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createInput = &input
	if m.items == nil {
		m.items = make(map[string]*store.Integration)
	}
	if m.secrets == nil {
		m.secrets = make(map[string][]byte)
	}
	id := fmt.Sprintf("int-%d", len(m.items)+1)
	item := &store.Integration{
		ID:        id,
		Kind:      input.Kind,
		Name:      input.Name,
		BaseURL:   input.BaseURL,
		HasSecret: len(input.Secret) > 0,
		CreatedAt: "2026-08-07T10:00:00.000Z",
		UpdatedAt: "2026-08-07T10:00:00.000Z",
	}
	m.items[id] = item
	m.secrets[id] = append([]byte(nil), input.Secret...)
	return item, nil
}

func (m *mockIntegrationRepo) Get(_ context.Context, id string) (*store.Integration, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (m *mockIntegrationRepo) List(_ context.Context) ([]store.Integration, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]store.Integration, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockIntegrationRepo) ListByKind(_ context.Context, kind string) ([]store.Integration, error) {
	all, err := m.List(context.Background())
	if err != nil {
		return nil, err
	}
	var out []store.Integration
	for _, item := range all {
		if item.Kind == kind {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *mockIntegrationRepo) Update(_ context.Context, id string, input store.UpdateIntegrationInput) (*store.Integration, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	m.updateInput = &input
	item.Name = input.Name
	item.BaseURL = input.BaseURL
	if input.Secret != nil {
		m.secrets[id] = append([]byte(nil), input.Secret...)
		item.HasSecret = len(input.Secret) > 0
	}
	item.UpdatedAt = "2026-08-07T12:00:00.000Z"
	return item, nil
}

func (m *mockIntegrationRepo) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.items, id)
	delete(m.secrets, id)
	return nil
}

func (m *mockIntegrationRepo) CountReferences(_ context.Context, id string) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.references[id], nil
}

func (m *mockIntegrationRepo) DecryptPayload(_ context.Context, id string) ([]byte, error) {
	if m.decryptErr != nil {
		return nil, m.decryptErr
	}
	secret, ok := m.secrets[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return secret, nil
}

type mockIntegrationTester struct {
	err error
	got struct {
		kind    string
		baseURL *string
		secret  []byte
	}
}

func (m *mockIntegrationTester) TestConnection(_ context.Context, kind string, baseURL *string, secret []byte) error {
	m.got.kind = kind
	m.got.baseURL = baseURL
	m.got.secret = append([]byte(nil), secret...)
	return m.err
}

func newIntegrationsTestRouter(t *testing.T, repo store.IntegrationRepository, tester handlers.IntegrationTester) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName
	h := &handlers.IntegrationHandlers{
		Integrations: repo,
		Tester:       tester,
	}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/integrations", h.List)
		protected.Post("/api/v1/integrations", h.Create)
		protected.Patch("/api/v1/integrations/{id}", h.Patch)
		protected.Delete("/api/v1/integrations/{id}", h.Delete)
		protected.Post("/api/v1/integrations/{id}/test", h.TestConnection)
	})
	return r, sm
}

func integrationResponseShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	for _, key := range []string{"id", "kind", "name", "baseUrl", "hasSecret", "createdAt", "updatedAt"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("response missing %q: %s", key, body)
		}
	}
	for _, forbidden := range []string{"secret", "encrypted_payload", "encryptedPayload"} {
		if _, ok := resp[forbidden]; ok {
			t.Fatalf("response leaks %q: %s", forbidden, body)
		}
	}
	return resp
}

func TestListIntegrationsWithoutSecrets(t *testing.T) {
	t.Parallel()

	baseURL := "https://gitlab.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "github",
				Name:      "GitHub PAT",
				BaseURL:   nil,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
			"int-2": {
				ID:        "int-2",
				Kind:      "gitlab",
				Name:      "GitLab",
				BaseURL:   &baseURL,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"token":"ghp_secret"}`),
			"int-2": []byte(`{"token":"glpat_secret"}`),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ghp_secret") || strings.Contains(rec.Body.String(), "glpat_secret") {
		t.Fatalf("list response leaks secret: %s", rec.Body.String())
	}

	var items []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	for _, item := range items {
		if hasSecret, ok := item["hasSecret"].(bool); !ok || !hasSecret {
			t.Fatalf("item missing hasSecret=true: %+v", item)
		}
	}
}

func TestCreateIntegrationPassesSecretToStore(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	body := `{"kind":"github","name":"GitHub PAT","secret":"ghp_test_token"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil {
		t.Fatal("expected Create to be called")
	}
	if string(repo.createInput.Secret) != "ghp_test_token" {
		t.Fatalf("secret = %q, want ghp_test_token", repo.createInput.Secret)
	}

	resp := integrationResponseShape(t, rec.Body.Bytes())
	if resp["hasSecret"] != true {
		t.Fatalf("hasSecret = %v, want true", resp["hasSecret"])
	}
}

func TestPatchIntegrationWithoutSecretKeepsExistingPayload(t *testing.T) {
	t.Parallel()

	baseURL := "https://gitlab.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "gitlab",
				Name:      "GitLab",
				BaseURL:   &baseURL,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"token":"keep-me"}`),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	newBaseURL := "https://gitlab.new.example"
	body := fmt.Sprintf(`{"name":"GitLab Updated","baseUrl":%q}`, newBaseURL)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil {
		t.Fatal("expected Update to be called")
	}
	if repo.updateInput.Secret != nil {
		t.Fatal("expected Update without secret replacement")
	}
	if repo.updateInput.Name != "GitLab Updated" {
		t.Fatalf("name = %q, want GitLab Updated", repo.updateInput.Name)
	}
	if repo.updateInput.BaseURL == nil || *repo.updateInput.BaseURL != newBaseURL {
		t.Fatalf("baseURL = %v, want %q", repo.updateInput.BaseURL, newBaseURL)
	}
	if string(repo.secrets["int-1"]) != `{"token":"keep-me"}` {
		t.Fatalf("secret changed without patch secret: %q", repo.secrets["int-1"])
	}
}

func TestPatchIntegrationReplacesSecretWhenProvided(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "github",
				Name:      "GitHub",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte("old-token"),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"GitHub","secret":"new-token"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil || repo.updateInput.Secret == nil {
		t.Fatal("expected Update with secret replacement")
	}
	if string(repo.secrets["int-1"]) != "new-token" {
		t.Fatalf("secret = %q, want new-token", repo.secrets["int-1"])
	}
}

func TestDeleteIntegrationConflictWhenReferenced(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "github",
				Name:      "GitHub",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		references: map[string]int64{
			"int-1": 2,
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/int-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"CONFLICT"`) {
		t.Fatalf("body = %s, want CONFLICT", rec.Body.String())
	}
	if _, ok := repo.items["int-1"]; !ok {
		t.Fatal("integration should not be deleted on conflict")
	}
}

func TestDeleteIntegrationSucceedsWhenUnreferenced(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "github",
				Name:      "GitHub",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		references: map[string]int64{
			"int-1": 0,
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/int-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, ok := repo.items["int-1"]; ok {
		t.Fatal("integration should be deleted")
	}
}

func TestTestConnectionSuccessAndError(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "github",
				Name:      "GitHub",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte("ghp_test"),
		},
	}
	tester := &mockIntegrationTester{}
	router, sm := newIntegrationsTestRouter(t, repo, tester)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/int-1/test", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("success status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var successResp struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&successResp); err != nil {
		t.Fatalf("decode success response: %v", err)
	}
	if !successResp.Success {
		t.Fatalf("success = false, want true")
	}
	if tester.got.kind != "github" || string(tester.got.secret) != "ghp_test" {
		t.Fatalf("tester got = %+v, want github/ghp_test", tester.got)
	}

	tester.err = errors.New("invalid credentials")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/integrations/int-1/test", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("error status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var errorResp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&errorResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errorResp.Success {
		t.Fatal("success = true, want false on connection failure")
	}
	if errorResp.Message == "" {
		t.Fatal("expected error message")
	}
}

func TestCreateIntegrationAcceptsAllKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind    string
		baseURL string
	}{
		{kind: "github"},
		{kind: "gitlab", baseURL: "https://gitlab.example"},
		{kind: "gitea", baseURL: "https://gitea.example"},
		{kind: "forgejo", baseURL: "https://forgejo.example"},
		{kind: "codeberg"},
		{kind: "phasical", baseURL: "https://api.phasical.example"},
		{kind: "jira", baseURL: "https://jira.example"},
		{kind: "linear"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			repo := &mockIntegrationRepo{}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			var body string
			if tc.baseURL != "" {
				body = fmt.Sprintf(
					`{"kind":%q,"name":%q,"baseUrl":%q,"secret":"token"}`,
					tc.kind, tc.kind+" integration", tc.baseURL,
				)
			} else {
				body = fmt.Sprintf(`{"kind":%q,"name":%q,"secret":"token"}`, tc.kind, tc.kind+" integration")
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(body))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
			}
			if repo.createInput == nil || repo.createInput.Kind != tc.kind {
				t.Fatalf("createInput = %+v, want kind %q", repo.createInput, tc.kind)
			}
		})
	}
}

func TestCreateIntegrationRejectsInvalidKind(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	body := `{"kind":"bitbucket","name":"Bad","secret":"token"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", bytes.NewReader([]byte(body)))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	respBody, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(respBody), `"code":"VALIDATION_ERROR"`) {
		t.Fatalf("body = %s, want VALIDATION_ERROR", respBody)
	}
}

func TestIntegrationsRequireSession(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{}
	router, _ := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
