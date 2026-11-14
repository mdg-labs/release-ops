package handlers_test

import (
	"context"
	"encoding/json"
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
	tokenFn  func(ctx context.Context, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes int64) (*store.AppSettings, error)
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

func (m *mockSettingsRepo) UpdateTokenExpiry(ctx context.Context, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes int64) (*store.AppSettings, error) {
	if m.tokenFn != nil {
		return m.tokenFn(ctx, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes)
	}
	updated := *m.settings
	updated.InviteTokenExpiryHours = inviteTokenExpiryHours
	updated.PasswordResetTokenExpiryMinutes = passwordResetTokenExpiryMinutes
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

func TestGetSettingsReturnsTokenWindows(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
			UpdatedAt:                       "2026-08-07T10:00:00.000Z",
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
		InviteTokenExpiryHours          int64 `json:"inviteTokenExpiryHours"`
		PasswordResetTokenExpiryMinutes int64 `json:"passwordResetTokenExpiryMinutes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.InviteTokenExpiryHours != 168 {
		t.Fatalf("inviteTokenExpiryHours = %d, want 168", resp.InviteTokenExpiryHours)
	}
	if resp.PasswordResetTokenExpiryMinutes != 60 {
		t.Fatalf("passwordResetTokenExpiryMinutes = %d, want 60", resp.PasswordResetTokenExpiryMinutes)
	}
}

func TestGetSettingsRequiresSession(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1},
	}
	router, _ := newSettingsTestRouter(t, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestPatchSettingsRequiresSession(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{ID: 1},
	}
	router, _ := newSettingsTestRouter(t, repo)

	body := `{"inviteTokenExpiryHours":60}`
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
			ID:        1,
			UpdatedAt: originalUpdatedAt,
		},
		tokenFn: func(_ context.Context, inviteTokenExpiryHours, passwordResetTokenExpiryMinutes int64) (*store.AppSettings, error) {
			updatedSettings = &store.AppSettings{
				ID:                              1,
				InviteTokenExpiryHours:          inviteTokenExpiryHours,
				PasswordResetTokenExpiryMinutes: passwordResetTokenExpiryMinutes,
				UpdatedAt:                       newUpdatedAt,
			}
			return updatedSettings, nil
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"inviteTokenExpiryHours":90}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updatedSettings == nil {
		t.Fatal("expected UpdateTokenExpiry to be called")
	}
	if updatedSettings.UpdatedAt != newUpdatedAt {
		t.Fatalf("updatedAt = %q, want %q", updatedSettings.UpdatedAt, newUpdatedAt)
	}
	if updatedSettings.UpdatedAt == originalUpdatedAt {
		t.Fatalf("updatedAt should change on patch, still %q", originalUpdatedAt)
	}
}

// A client that still sends the removed pollIntervalMinutes field gets the unchanged
// settings back, and the response no longer carries the field.
func TestPatchSettingsIgnoresRemovedPollInterval(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
		tokenFn: func(context.Context, int64, int64) (*store.AppSettings, error) {
			t.Fatal("token expiry updated by a pollIntervalMinutes-only patch")
			return nil, nil
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(`{"pollIntervalMinutes":120}`))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["pollIntervalMinutes"]; ok {
		t.Fatalf("response = %v, want no pollIntervalMinutes field", resp)
	}
	if resp["inviteTokenExpiryHours"] != float64(168) {
		t.Fatalf("inviteTokenExpiryHours = %v, want 168 (unchanged)", resp["inviteTokenExpiryHours"])
	}
}

func TestPatchSettingsUpdatesTokenExpiry(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"inviteTokenExpiryHours":48,"passwordResetTokenExpiryMinutes":30}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		InviteTokenExpiryHours          int64 `json:"inviteTokenExpiryHours"`
		PasswordResetTokenExpiryMinutes int64 `json:"passwordResetTokenExpiryMinutes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.InviteTokenExpiryHours != 48 {
		t.Fatalf("inviteTokenExpiryHours = %d, want 48", resp.InviteTokenExpiryHours)
	}
	if resp.PasswordResetTokenExpiryMinutes != 30 {
		t.Fatalf("passwordResetTokenExpiryMinutes = %d, want 30", resp.PasswordResetTokenExpiryMinutes)
	}
}

func TestPatchSettingsRejectsInviteExpiryBelowMinimum(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"inviteTokenExpiryHours":0}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if repo.settings.InviteTokenExpiryHours != 168 {
		t.Fatalf("inviteTokenExpiryHours should not be updated, got %d", repo.settings.InviteTokenExpiryHours)
	}
}

func TestPatchSettingsRejectsInviteExpiryAboveMaximum(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"inviteTokenExpiryHours":721}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPatchSettingsRejectsResetExpiryBelowMinimum(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"passwordResetTokenExpiryMinutes":4}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestPatchSettingsRejectsResetExpiryAboveMaximum(t *testing.T) {
	t.Parallel()

	repo := &mockSettingsRepo{
		settings: &store.AppSettings{
			ID:                              1,
			InviteTokenExpiryHours:          168,
			PasswordResetTokenExpiryMinutes: 60,
		},
	}
	router, sm := newSettingsTestRouter(t, repo)
	cookie := seedSession(t, sm)

	body := `{"passwordResetTokenExpiryMinutes":1441}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
