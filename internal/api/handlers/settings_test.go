package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
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

type mockSettingsRepo struct {
	settings *store.AppSettings
	getErr   error
	updateFn func(ctx context.Context, pollIntervalMinutes int64) (*store.AppSettings, error)
}

func (m *mockSettingsRepo) EnsureDefault(context.Context) error {
	return nil
}

func (m *mockSettingsRepo) Get(_ context.Context) (*store.AppSettings, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.settings, nil
}

func (m *mockSettingsRepo) UpdatePollInterval(ctx context.Context, pollIntervalMinutes int64) (*store.AppSettings, error) {
	if m.updateFn != nil {
		return m.updateFn(ctx, pollIntervalMinutes)
	}
	updated := *m.settings
	updated.PollIntervalMinutes = pollIntervalMinutes
	updated.UpdatedAt = "2026-08-07T12:00:00.000Z"
	m.settings = &updated
	return m.settings, nil
}

func newSettingsTestRouter(t *testing.T, repo store.SettingsRepository) (http.Handler, *scs.SessionManager) {
	t.Helper()

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Cookie.Name = auth.SessionCookieName
	h := &handlers.SettingsHandlers{Settings: repo}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Group(func(protected chi.Router) {
		protected.Use(apimw.RequireSession(sm))
		protected.Get("/api/v1/settings", h.Get)
		protected.Patch("/api/v1/settings", h.Patch)
	})
	return r, sm
}

func seedSession(t *testing.T, sm *scs.SessionManager) *http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/seed", nil)
	rec := httptest.NewRecorder()
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), auth.SessionUserIDKey, "user-123")
		sm.Put(r.Context(), auth.SessionUserEmailKey, "admin@example.com")
	})).ServeHTTP(rec, req)

	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.SessionCookieName {
			return c
		}
	}
	t.Fatal("expected session cookie")
	return nil
}

func TestGetSettingsReturnsPollIntervalMinutes(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                  1,
			PollIntervalMinutes: 360,
			UpdatedAt:           "2026-08-07T10:00:00.000Z",
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		PollIntervalMinutes int64 `json:"pollIntervalMinutes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PollIntervalMinutes != 360 {
		t.Fatalf("pollIntervalMinutes = %d, want 360", resp.PollIntervalMinutes)
	}
}

func TestGetSettingsRequiresSession(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
	}
	router, _ := newSettingsTestRouter(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPatchSettingsUpdatesValue(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                  1,
			PollIntervalMinutes: 360,
			UpdatedAt:           "2026-08-07T10:00:00.000Z",
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"pollIntervalMinutes":120}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		PollIntervalMinutes int64 `json:"pollIntervalMinutes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.PollIntervalMinutes != 120 {
		t.Fatalf("pollIntervalMinutes = %d, want 120", resp.PollIntervalMinutes)
	}
	if repo.settings.PollIntervalMinutes != 120 {
		t.Fatalf("stored pollIntervalMinutes = %d, want 120", repo.settings.PollIntervalMinutes)
	}
}

func TestPatchSettingsRejectsValueBelowMinimum(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"pollIntervalMinutes":4}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", bytes.NewReader([]byte(body)))
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
	if repo.settings.PollIntervalMinutes != 360 {
		t.Fatalf("settings should not be updated, got %d", repo.settings.PollIntervalMinutes)
	}
}

func TestPatchSettingsAcceptsMinimumValue(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"pollIntervalMinutes":5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestPatchSettingsRequiresSession(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1, PollIntervalMinutes: 360},
	}
	router, _ := newSettingsTestRouter(t, repo)

	body := `{"pollIntervalMinutes":60}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPatchSettingsSetsUpdatedAt(t *testing.T) {
	t.Parallel()

	const originalUpdatedAt = "2026-08-07T10:00:00.000Z"
	const newUpdatedAt = "2026-08-07T12:00:00.000Z"

	var updatedSettings *store.AppSettings
	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                  1,
			PollIntervalMinutes: 360,
			UpdatedAt:           originalUpdatedAt,
		},
		updateFn: func(_ context.Context, pollIntervalMinutes int64) (*store.AppSettings, error) {
			updatedSettings = &store.AppSettings{
				ID:                  1,
				PollIntervalMinutes: pollIntervalMinutes,
				UpdatedAt:           newUpdatedAt,
			}
			return updatedSettings, nil
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"pollIntervalMinutes":90}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updatedSettings == nil {
		t.Fatal("expected UpdatePollInterval to be called")
	}
	if updatedSettings.UpdatedAt != newUpdatedAt {
		t.Fatalf("updatedAt = %q, want %q", updatedSettings.UpdatedAt, newUpdatedAt)
	}
	if updatedSettings.UpdatedAt == originalUpdatedAt {
		t.Fatalf("updatedAt should change on patch, still %q", originalUpdatedAt)
	}
}
