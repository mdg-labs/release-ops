package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

type mockNotificationRepo struct {
	items       map[string]*store.NotificationTarget
	urls        map[string]string
	createInput *store.CreateNotificationTargetInput
	updateInput *store.UpdateNotificationTargetInput
	createErr   error
	listErr     error
	getErr      error
	updateErr   error
	deleteErr   error
	decryptErr  error
}

func (m *mockNotificationRepo) Create(_ context.Context, input store.CreateNotificationTargetInput) (*store.NotificationTarget, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	m.createInput = &input
	if m.items == nil {
		m.items = make(map[string]*store.NotificationTarget)
	}
	if m.urls == nil {
		m.urls = make(map[string]string)
	}

	events := input.Events
	if len(events) == 0 {
		events = append([]string(nil), store.DefaultNotificationEvents...)
	}

	id := fmt.Sprintf("nt-%d", len(m.items)+1)
	item := &store.NotificationTarget{
		ID:        id,
		Name:      input.Name,
		HasSecret: input.ShoutrrrURL != "",
		Events:    append([]string(nil), events...),
		Enabled:   input.Enabled,
		CreatedAt: "2026-08-07T10:00:00.000Z",
		UpdatedAt: "2026-08-07T10:00:00.000Z",
	}
	m.items[id] = item
	m.urls[id] = input.ShoutrrrURL
	return item, nil
}

func (m *mockNotificationRepo) Get(_ context.Context, id string) (*store.NotificationTarget, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return item, nil
}

func (m *mockNotificationRepo) List(_ context.Context) ([]store.NotificationTarget, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]store.NotificationTarget, 0, len(m.items))
	for _, item := range m.items {
		out = append(out, *item)
	}
	return out, nil
}

func (m *mockNotificationRepo) ListEnabled(_ context.Context) ([]store.NotificationTarget, error) {
	all, err := m.List(context.Background())
	if err != nil {
		return nil, err
	}
	var out []store.NotificationTarget
	for _, item := range all {
		if item.Enabled {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *mockNotificationRepo) Update(_ context.Context, id string, input store.UpdateNotificationTargetInput) (*store.NotificationTarget, error) {
	if m.updateErr != nil {
		return nil, m.updateErr
	}
	item, ok := m.items[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	m.updateInput = &input
	item.Name = input.Name
	item.Events = append([]string(nil), input.Events...)
	item.Enabled = input.Enabled
	if input.ShoutrrrURL != nil {
		m.urls[id] = *input.ShoutrrrURL
		item.HasSecret = *input.ShoutrrrURL != ""
	}
	item.UpdatedAt = "2026-08-07T12:00:00.000Z"
	return item, nil
}

func (m *mockNotificationRepo) Delete(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.items, id)
	delete(m.urls, id)
	return nil
}

func (m *mockNotificationRepo) DecryptURL(_ context.Context, id string) (string, error) {
	if m.decryptErr != nil {
		return "", m.decryptErr
	}
	url, ok := m.urls[id]
	if !ok {
		return "", sql.ErrNoRows
	}
	return url, nil
}

type mockNotificationTester struct {
	err      error
	gotURL   string
	checkCtx func(context.Context)
}

func (m *mockNotificationTester) SendTest(ctx context.Context, shoutrrrURL string) error {
	if m.checkCtx != nil {
		m.checkCtx(ctx)
	}
	m.gotURL = shoutrrrURL
	return m.err
}

func newNotificationsTestRouter(t *testing.T, repo store.NotificationTargetRepository, tester handlers.NotificationTester) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName
	h := &handlers.NotificationHandlers{
		Notifications: repo,
		Tester:        tester,
	}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/notification-targets", h.List)
		protected.Post("/api/v1/notification-targets", h.Create)
		protected.Patch("/api/v1/notification-targets/{id}", h.Patch)
		protected.Delete("/api/v1/notification-targets/{id}", h.Delete)
		protected.Post("/api/v1/notification-targets/{id}/test", h.Test)
	})
	return r, sm
}

func notificationTargetResponseShape(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, body)
	}
	for _, key := range []string{"id", "name", "hasSecret", "events", "enabled", "createdAt", "updatedAt"} {
		if _, ok := resp[key]; !ok {
			t.Fatalf("response missing %q: %s", key, body)
		}
	}
	for _, forbidden := range []string{"shoutrrrUrl", "shoutrrr_url", "shoutrrrUrlEncrypted"} {
		if _, ok := resp[forbidden]; ok {
			t.Fatalf("response leaks %q: %s", forbidden, body)
		}
	}
	return resp
}

func TestListNotificationTargetsWithoutURL(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create", "error", "supersede"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		urls: map[string]string{
			"nt-1": "slack://token@channel",
		},
	}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-targets", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "slack://") {
		t.Fatalf("list response leaks shoutrrr URL: %s", rec.Body.String())
	}

	var items []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if hasSecret, ok := items[0]["hasSecret"].(bool); !ok || !hasSecret {
		t.Fatalf("item missing hasSecret=true: %+v", items[0])
	}
}

func TestCreateNotificationTargetPassesURLToStore(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Slack","shoutrrrUrl":"slack://token@channel"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil {
		t.Fatal("expected Create to be called")
	}
	if repo.createInput.ShoutrrrURL != "slack://token@channel" {
		t.Fatalf("shoutrrrURL = %q, want slack://token@channel", repo.createInput.ShoutrrrURL)
	}
	if strings.Contains(rec.Body.String(), "slack://") {
		t.Fatalf("create response leaks shoutrrr URL: %s", rec.Body.String())
	}

	resp := notificationTargetResponseShape(t, rec.Body.Bytes())
	if resp["hasSecret"] != true {
		t.Fatalf("hasSecret = %v, want true", resp["hasSecret"])
	}
}

func TestCreateNotificationTargetDefaultsEvents(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"ntfy","shoutrrrUrl":"ntfy://example/topic"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil || len(repo.createInput.Events) != 0 {
		t.Fatalf("createInput.Events = %v, want empty slice for store default", repo.createInput.Events)
	}

	resp := notificationTargetResponseShape(t, rec.Body.Bytes())
	events, ok := resp["events"].([]any)
	if !ok || len(events) != 3 {
		t.Fatalf("events = %v, want 3 default entries", resp["events"])
	}
}

func TestCreateNotificationTargetEnabledToggle(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Disabled","shoutrrrUrl":"generic://example","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if repo.createInput == nil || repo.createInput.Enabled {
		t.Fatal("expected enabled=false on create")
	}

	resp := notificationTargetResponseShape(t, rec.Body.Bytes())
	if resp["enabled"] != false {
		t.Fatalf("enabled = %v, want false", resp["enabled"])
	}
}

func TestCreateNotificationTargetRejectsInvalidEvents(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Bad","shoutrrrUrl":"generic://example","events":["baseline"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"VALIDATION_ERROR"`) {
		t.Fatalf("body = %s, want VALIDATION_ERROR", rec.Body.String())
	}
	if repo.createInput != nil {
		t.Fatal("Create should not be called for invalid events")
	}
}

func TestPatchNotificationTargetWithoutURLKeepsExisting(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create", "error", "supersede"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		urls: map[string]string{
			"nt-1": "slack://keep-me",
		},
	}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Slack Updated","events":["create"],"enabled":false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notification-targets/nt-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil {
		t.Fatal("expected Update to be called")
	}
	if repo.updateInput.ShoutrrrURL != nil {
		t.Fatal("expected Update without shoutrrrUrl replacement")
	}
	if repo.urls["nt-1"] != "slack://keep-me" {
		t.Fatalf("url = %q, want slack://keep-me", repo.urls["nt-1"])
	}
	if repo.updateInput.Enabled {
		t.Fatal("expected enabled=false on patch")
	}
}

func TestPatchNotificationTargetReplacesURLWhenProvided(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create", "error", "supersede"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		urls: map[string]string{
			"nt-1": "slack://old",
		},
	}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	body := `{"name":"Slack","shoutrrrUrl":"slack://new","events":["error"],"enabled":true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/notification-targets/nt-1", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if repo.updateInput == nil || repo.updateInput.ShoutrrrURL == nil {
		t.Fatal("expected Update with shoutrrrUrl replacement")
	}
	if *repo.updateInput.ShoutrrrURL != "slack://new" {
		t.Fatalf("shoutrrrUrl = %q, want slack://new", *repo.updateInput.ShoutrrrURL)
	}
	if strings.Contains(rec.Body.String(), "slack://") {
		t.Fatalf("patch response leaks shoutrrr URL: %s", rec.Body.String())
	}
}

func TestDeleteNotificationTargetSucceeds(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
	}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notification-targets/nt-1", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if _, ok := repo.items["nt-1"]; ok {
		t.Fatal("notification target should be deleted")
	}
}

func TestDeleteNotificationTargetNotFound(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, sm := newNotificationsTestRouter(t, repo, &mockNotificationTester{})
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/notification-targets/missing", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestTestNotificationSuccessAndError(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		urls: map[string]string{
			"nt-1": "slack://token@channel",
		},
	}
	tester := &mockNotificationTester{}
	router, sm := newNotificationsTestRouter(t, repo, tester)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets/nt-1/test", nil)
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
		t.Fatal("success = false, want true")
	}
	if tester.gotURL != "slack://token@channel" {
		t.Fatalf("tester URL = %q, want slack://token@channel", tester.gotURL)
	}

	tester.err = errors.New("delivery failed")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets/nt-1/test", nil)
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
		t.Fatal("success = true, want false on delivery failure")
	}
	if errorResp.Message == "" {
		t.Fatal("expected error message")
	}
}

func TestTestNotificationUsesDetachedContext(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{
		items: map[string]*store.NotificationTarget{
			"nt-1": {
				ID:        "nt-1",
				Name:      "Slack",
				HasSecret: true,
				Events:    []string{"create"},
				Enabled:   true,
				CreatedAt: "2026-08-07T10:00:00.000Z",
				UpdatedAt: "2026-08-07T10:00:00.000Z",
			},
		},
		urls: map[string]string{
			"nt-1": "slack://token@channel",
		},
	}
	tester := &mockNotificationTester{
		checkCtx: func(ctx context.Context) {
			select {
			case <-ctx.Done():
				t.Fatal("SendTest context was cancelled")
			default:
			}
		},
	}
	router, sm := newNotificationsTestRouter(t, repo, tester)
	cookie := seedSession(t, sm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-targets/nt-1/test", nil)
	req = req.WithContext(ctx)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNotificationTargetsRequireSession(t *testing.T) {
	t.Parallel()

	repo := &mockNotificationRepo{}
	router, _ := newNotificationsTestRouter(t, repo, &mockNotificationTester{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-targets", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
