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

func (m *mockIntegrationRepo) Credentials(ctx context.Context, id string) (*store.IntegrationCredentials, error) {
	item, err := m.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	secret, err := m.DecryptPayload(ctx, id)
	if err != nil {
		return nil, err
	}
	return &store.IntegrationCredentials{Kind: item.Kind, BaseURL: item.BaseURL, Payload: secret}, nil
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
	for _, key := range []string{"id", "kind", "name", "baseUrl", "hasSecret", "isDefault", "createdAt", "updatedAt"} {
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

	baseURL := "https://jira.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "linear",
				Name:      "Linear",
				BaseURL:   nil,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
			"int-2": {
				ID:        "int-2",
				Kind:      "jira",
				Name:      "Jira",
				BaseURL:   &baseURL,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"api_key":"lin_secret"}`),
			"int-2": []byte(`{"email":"a@b.example","api_token":"jira_secret"}`),
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
	if strings.Contains(rec.Body.String(), "lin_secret") || strings.Contains(rec.Body.String(), "jira_secret") {
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

	body := `{"kind":"linear","name":"Linear","secret":"{\"api_key\":\"lin_test_key\"}"}`
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
	if string(repo.createInput.Secret) != `{"api_key":"lin_test_key"}` {
		t.Fatalf("secret = %q, want the submitted token payload", repo.createInput.Secret)
	}

	resp := integrationResponseShape(t, rec.Body.Bytes())
	if resp["hasSecret"] != true {
		t.Fatalf("hasSecret = %v, want true", resp["hasSecret"])
	}
}

func TestPatchIntegrationWithoutSecretKeepsExistingPayload(t *testing.T) {
	t.Parallel()

	baseURL := "https://jira.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "jira",
				Name:      "Jira",
				BaseURL:   &baseURL,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"email":"a@b.example","api_token":"keep-me"}`),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	newBaseURL := "https://jira.example/"
	body := fmt.Sprintf(`{"name":"Jira Updated","baseUrl":%q}`, newBaseURL)
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
	if repo.updateInput.Name != "Jira Updated" {
		t.Fatalf("name = %q, want Jira Updated", repo.updateInput.Name)
	}
	if repo.updateInput.BaseURL == nil || *repo.updateInput.BaseURL != newBaseURL {
		t.Fatalf("baseURL = %v, want %q", repo.updateInput.BaseURL, newBaseURL)
	}
	if string(repo.secrets["int-1"]) != `{"email":"a@b.example","api_token":"keep-me"}` {
		t.Fatalf("secret changed without patch secret: %q", repo.secrets["int-1"])
	}
}

func TestPatchIntegrationRejectsBaseURLChangeWithoutSecret(t *testing.T) {
	t.Parallel()

	baseURL := "https://jira.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "jira",
				Name:      "Jira",
				BaseURL:   &baseURL,
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"email":"a@b.example","api_token":"keep-me"}`),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Jira","baseUrl":"https://attacker.example"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if repo.updateInput != nil {
		t.Fatal("expected no Update when baseUrl changes without a new secret")
	}
}

func TestPatchIntegrationReplacesSecretWhenProvided(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "linear",
				Name:      "Linear",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"api_key":"old-key"}`),
		},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Linear","secret":"{\"api_key\":\"new-key\"}"}`
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
	if string(repo.secrets["int-1"]) != `{"api_key":"new-key"}` {
		t.Fatalf("secret = %q, want the submitted token payload", repo.secrets["int-1"])
	}
}

func TestPatchIntegrationRejectsBrokenJiraSecret(t *testing.T) {
	t.Parallel()

	const stored = `{"email":"old@example.com","api_token":"old-token"}`
	cases := map[string]string{
		"empty email":       `{"email":"","api_token":"new-token"}`,
		"missing email":     `{"api_token":"new-token"}`,
		"blank email":       `{"email":"   ","api_token":"new-token"}`,
		"empty api_token":   `{"email":"a@b.example","api_token":""}`,
		"missing api_token": `{"email":"a@b.example"}`,
		"not JSON":          `hunter2`,
		"not an object":     `"hunter2"`,
	}

	for name, secret := range cases {
		secret := secret
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			baseURL := "https://jira.example"
			repo := &mockIntegrationRepo{
				items: map[string]*store.Integration{
					"int-1": {ID: "int-1", Kind: "jira", Name: "Jira", BaseURL: &baseURL, HasSecret: true},
				},
				secrets: map[string][]byte{"int-1": []byte(stored)},
			}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			payload, err := json.Marshal(map[string]string{"name": "Jira", "secret": secret})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(string(payload)))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if repo.updateInput != nil {
				t.Fatal("expected no Update for a broken Jira secret")
			}
			if string(repo.secrets["int-1"]) != stored {
				t.Fatalf("stored secret changed: %q", repo.secrets["int-1"])
			}
			if strings.Contains(rec.Body.String(), "hunter2") {
				t.Fatalf("response echoes the submitted secret: %s", rec.Body.String())
			}
		})
	}
}

func TestPatchIntegrationAcceptsValidJiraSecret(t *testing.T) {
	t.Parallel()

	baseURL := "https://jira.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {ID: "int-1", Kind: "jira", Name: "Jira", BaseURL: &baseURL, HasSecret: true},
		},
		secrets: map[string][]byte{"int-1": []byte(`{"email":"old@example.com","api_token":"old"}`)},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	const next = `{"email":"new@example.com","api_token":"new"}`
	payload, err := json.Marshal(map[string]string{"name": "Jira", "secret": next})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(string(payload)))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if string(repo.secrets["int-1"]) != next {
		t.Fatalf("secret = %q, want %q", repo.secrets["int-1"], next)
	}
}

func TestPatchJiraNameOnlyKeepsStoredSecret(t *testing.T) {
	t.Parallel()

	const stored = `{"email":"old@example.com","api_token":"old"}`
	baseURL := "https://jira.example"
	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {ID: "int-1", Kind: "jira", Name: "Jira", BaseURL: &baseURL, HasSecret: true},
		},
		secrets: map[string][]byte{"int-1": []byte(stored)},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(`{"name":"Renamed"}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil || repo.updateInput.Secret != nil {
		t.Fatalf("updateInput = %+v, want an update without a secret", repo.updateInput)
	}
	if string(repo.secrets["int-1"]) != stored {
		t.Fatalf("stored secret changed: %q", repo.secrets["int-1"])
	}
}

func TestCreateIntegrationRejectsBrokenJiraSecret(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty email":     `{"email":"","api_token":"tok"}`,
		"missing email":   `{"api_token":"tok"}`,
		"empty api_token": `{"email":"a@b.example","api_token":""}`,
		"not JSON":        `hunter2`,
	}

	for name, secret := range cases {
		secret := secret
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			repo := &mockIntegrationRepo{}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			payload, err := json.Marshal(map[string]string{
				"kind": "jira", "name": "Jira", "baseUrl": "https://jira.example", "secret": secret,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(string(payload)))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if repo.createInput != nil {
				t.Fatal("expected no Create for a broken Jira secret")
			}
			if strings.Contains(rec.Body.String(), "hunter2") {
				t.Fatalf("response echoes the submitted secret: %s", rec.Body.String())
			}
		})
	}
}

func TestDeleteIntegrationConflictWhenReferenced(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {
				ID:        "int-1",
				Kind:      "linear",
				Name:      "Linear",
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
				Kind:      "linear",
				Name:      "Linear",
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
				Kind:      "linear",
				Name:      "Linear",
				HasSecret: true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		secrets: map[string][]byte{
			"int-1": []byte(`{"api_key":"lin_test"}`),
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
	if tester.got.kind != "linear" || string(tester.got.secret) != `{"api_key":"lin_test"}` {
		t.Fatalf("tester got = %+v, want linear and its decrypted secret", tester.got)
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

func TestCreateIntegrationAcceptsTicketKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind    string
		baseURL string
		secret  string
	}{
		{kind: "kaneo", baseURL: "https://api.kaneo.example", secret: `{"api_key":"token"}`},
		{kind: "jira", baseURL: "https://jira.example", secret: `{"email":"a@b.example","api_token":"tok"}`},
		{kind: "linear", secret: `{"api_key":"token"}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			secret := tc.secret

			repo := &mockIntegrationRepo{}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			var body string
			if tc.baseURL != "" {
				body = fmt.Sprintf(
					`{"kind":%q,"name":%q,"baseUrl":%q,"secret":%q}`,
					tc.kind, tc.kind+" integration", tc.baseURL, secret,
				)
			} else {
				body = fmt.Sprintf(`{"kind":%q,"name":%q,"secret":%q}`, tc.kind, tc.kind+" integration", secret)
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

func postIntegration(t *testing.T, router http.Handler, cookie *http.Cookie, payload map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(string(body)))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateIntegrationRequiresSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind    string
		baseURL string
	}{
		{kind: "kaneo", baseURL: "https://api.kaneo.example"},
		{kind: "jira", baseURL: "https://jira.example"},
		{kind: "linear"},
	}

	for _, tc := range cases {
		for _, variant := range []string{"omitted", "empty"} {
			tc, variant := tc, variant
			t.Run(tc.kind+"/"+variant, func(t *testing.T) {
				t.Parallel()

				repo := &mockIntegrationRepo{}
				router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
				cookie := seedSession(t, sm)

				payload := map[string]string{"kind": tc.kind, "name": tc.kind + " integration"}
				if tc.baseURL != "" {
					payload["baseUrl"] = tc.baseURL
				}
				if variant == "empty" {
					payload["secret"] = ""
				}

				rec := postIntegration(t, router, cookie, payload)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
				}
				if repo.createInput != nil {
					t.Fatal("expected no Create without a secret")
				}
			})
		}
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

func TestCreateIntegrationRejectsDefaultFlag(t *testing.T) {
	t.Parallel()

	// Each secret is valid for its kind and each baseUrl is accepted, so the only
	// reason left to refuse the request is the isDefault flag.
	for _, tc := range []struct {
		kind    string
		baseURL string
		secret  string
	}{
		{kind: "kaneo", baseURL: "https://kaneo.example", secret: `{"api_key":"kaneo_test_key"}`},
		{kind: "jira", baseURL: "https://jira.example", secret: `{"email":"bot@example.com","api_token":"jira_test_token"}`},
		{kind: "linear", secret: `{"api_key":"lin_test_key"}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			repo := &mockIntegrationRepo{}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			payload := map[string]any{"kind": tc.kind, "name": "Ticket", "secret": tc.secret, "isDefault": true}
			if tc.baseURL != "" {
				payload["baseUrl"] = tc.baseURL
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations", strings.NewReader(string(body)))
			req.AddCookie(cookie)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if got, want := errorMessage(t, rec.Body.Bytes()), "isDefault is not supported"; got != want {
				t.Errorf("error message = %q, want %q", got, want)
			}
			if repo.createInput != nil {
				t.Fatalf("Create called despite rejected default flag: %+v", repo.createInput)
			}
		})
	}
}

func patchIntegration(t *testing.T, router http.Handler, cookie *http.Cookie, id string, payload map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/"+id, strings.NewReader(string(body)))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func errorMessage(t *testing.T, body []byte) string {
	t.Helper()

	var resp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode error body %s: %v", body, err)
	}
	return resp.Error.Message
}

var apiKeyKindCases = []struct {
	kind    string
	baseURL string
}{
	{kind: "kaneo", baseURL: "https://api.kaneo.example"},
	{kind: "linear"},
}

// malformedAPIKeySecrets cannot be read by ticket.ParseKaneoSecret or ParseLinearSecret.
var malformedAPIKeySecrets = map[string]string{
	"bare key":       "lin_hunter2",
	"JSON string":    `"lin_hunter2"`,
	"truncated":      `{"api_key":"lin_hunter2"`,
	"key not string": `{"api_key":12345}`,
	"empty api_key":  `{"api_key":""}`,
	"blank api_key":  `{"api_key":"   "}`,
	"wrong field":    `{"token":"lin_hunter2"}`,
	"null":           `null`,
	"empty object":   `{}`,
	"JSON array":     `["lin_hunter2"]`,
}

func TestCreateIntegrationRejectsMalformedAPIKeySecret(t *testing.T) {
	t.Parallel()

	for _, kc := range apiKeyKindCases {
		for name, secret := range malformedAPIKeySecrets {
			kc, secret := kc, secret
			t.Run(kc.kind+"/"+name, func(t *testing.T) {
				t.Parallel()

				repo := &mockIntegrationRepo{}
				router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
				cookie := seedSession(t, sm)

				payload := map[string]string{"kind": kc.kind, "name": "Tickets", "secret": secret}
				if kc.baseURL != "" {
					payload["baseUrl"] = kc.baseURL
				}
				rec := postIntegration(t, router, cookie, payload)

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
				}
				if repo.createInput != nil {
					t.Fatal("expected no Create for a malformed api_key secret")
				}
				if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "12345") {
					t.Fatalf("response echoes the submitted secret: %s", rec.Body.String())
				}
			})
		}
	}
}

func TestPatchIntegrationRejectsMalformedAPIKeySecret(t *testing.T) {
	t.Parallel()

	const stored = `{"api_key":"keep-me"}`
	for _, kc := range apiKeyKindCases {
		for name, secret := range malformedAPIKeySecrets {
			kc, secret := kc, secret
			t.Run(kc.kind+"/"+name, func(t *testing.T) {
				t.Parallel()

				var baseURL *string
				if kc.baseURL != "" {
					baseURL = &kc.baseURL
				}
				repo := &mockIntegrationRepo{
					items: map[string]*store.Integration{
						"int-1": {ID: "int-1", Kind: kc.kind, Name: "Tickets", BaseURL: baseURL, HasSecret: true},
					},
					secrets: map[string][]byte{"int-1": []byte(stored)},
				}
				router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
				cookie := seedSession(t, sm)

				rec := patchIntegration(t, router, cookie, "int-1", map[string]string{"name": "Tickets", "secret": secret})

				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
				}
				if repo.updateInput != nil {
					t.Fatal("expected no Update for a malformed api_key secret")
				}
				if string(repo.secrets["int-1"]) != stored {
					t.Fatalf("stored secret changed: %q", repo.secrets["int-1"])
				}
				if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), "12345") {
					t.Fatalf("response echoes the submitted secret: %s", rec.Body.String())
				}
			})
		}
	}
}

func TestPatchIntegrationReplacesAPIKeySecretWhenValid(t *testing.T) {
	t.Parallel()

	const fresh = `{"api_key":"fresh"}`
	for _, kc := range apiKeyKindCases {
		kc := kc
		t.Run(kc.kind, func(t *testing.T) {
			t.Parallel()

			var baseURL *string
			if kc.baseURL != "" {
				baseURL = &kc.baseURL
			}
			repo := &mockIntegrationRepo{
				items: map[string]*store.Integration{
					"int-1": {ID: "int-1", Kind: kc.kind, Name: "Tickets", BaseURL: baseURL, HasSecret: true},
				},
				secrets: map[string][]byte{"int-1": []byte(`{"api_key":"old"}`)},
			}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			rec := patchIntegration(t, router, cookie, "int-1", map[string]string{"name": "Tickets", "secret": fresh})

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if repo.updateInput == nil || string(repo.updateInput.Secret) != fresh {
				t.Fatalf("updateInput = %+v, want secret %q", repo.updateInput, fresh)
			}
		})
	}
}

func TestCreateIntegrationRejectsRemovedSourceKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{"github", "gitlab", "gitea", "forgejo", "codeberg"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()

			repo := &mockIntegrationRepo{}
			router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
			cookie := seedSession(t, sm)

			rec := postIntegration(t, router, cookie, map[string]string{
				"kind": kind, "name": "Source", "baseUrl": "https://forge.example", "secret": `{"token":"tok"}`,
			})

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(errorMessage(t, rec.Body.Bytes()), "kind must be one of: kaneo, jira, linear") {
				t.Fatalf("body = %s, want the ticket kinds listed", rec.Body.String())
			}
			if repo.createInput != nil {
				t.Fatalf("Create called for removed kind: %+v", repo.createInput)
			}
		})
	}
}

func TestPatchIntegrationRefusesLeftoverSourceKind(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {ID: "int-1", Kind: "github", Name: "GitHub", HasSecret: true},
		},
		secrets: map[string][]byte{"int-1": []byte(`{"token":"keep-me"}`)},
	}
	router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
	cookie := seedSession(t, sm)

	rec := patchIntegration(t, router, cookie, "int-1", map[string]string{"name": "Renamed"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if repo.updateInput != nil {
		t.Fatalf("Update called for a removed kind: %+v", repo.updateInput)
	}
}

func TestPatchIntegrationDefaultFlag(t *testing.T) {
	t.Parallel()

	newRepo := func() *mockIntegrationRepo {
		return &mockIntegrationRepo{
			items: map[string]*store.Integration{
				"int-1": {ID: "int-1", Kind: "linear", Name: "Linear", HasSecret: true},
			},
			secrets: map[string][]byte{"int-1": []byte(`{"api_key":"a"}`)},
		}
	}
	patch := func(t *testing.T, repo *mockIntegrationRepo, body string) *httptest.ResponseRecorder {
		t.Helper()

		router, sm := newIntegrationsTestRouter(t, repo, &mockIntegrationTester{})
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/integrations/int-1", strings.NewReader(body))
		req.AddCookie(seedSession(t, sm))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("true is rejected", func(t *testing.T) {
		t.Parallel()

		repo := newRepo()
		rec := patch(t, repo, `{"name":"Linear","isDefault":true}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
		if repo.updateInput != nil {
			t.Fatalf("Update called despite rejected default flag: %+v", repo.updateInput)
		}
	})

	t.Run("false is accepted and the response reports false", func(t *testing.T) {
		t.Parallel()

		repo := newRepo()
		rec := patch(t, repo, `{"name":"Linear","isDefault":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
		}
		if resp := integrationResponseShape(t, rec.Body.Bytes()); resp["isDefault"] != false {
			t.Fatalf("isDefault = %v, want false", resp["isDefault"])
		}
	})
}

func TestDeleteIntegrationConflictWhenStoreReportsInUse(t *testing.T) {
	t.Parallel()

	repo := &mockIntegrationRepo{
		items: map[string]*store.Integration{
			"int-1": {ID: "int-1", Kind: "github", Name: "GitHub", HasSecret: true},
		},
		deleteErr: store.ErrInUse,
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
}
