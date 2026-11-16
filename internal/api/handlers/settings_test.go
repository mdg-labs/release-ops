package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
)

// panicInstanceSettings and panicAppInfo fail the test when a handler reaches the store.
type panicInstanceSettings struct{ t *testing.T }

func (p panicInstanceSettings) ConcurrencyCap(context.Context) (int64, error) {
	p.t.Error("the handler read the concurrency cap without an admin principal")
	return 0, nil
}

func (p panicInstanceSettings) SetConcurrencyCap(context.Context, int64) error {
	p.t.Error("the handler wrote the concurrency cap without an admin principal")
	return nil
}

func (p panicInstanceSettings) GlobalConfig(context.Context) (store.CapturedGlobalConfig, error) {
	p.t.Error("the handler read the global config without an admin principal")
	return store.CapturedGlobalConfig{}, nil
}

type panicAppInfo struct{ t *testing.T }

func (p panicAppInfo) Info(context.Context) (store.GitHubAppInfo, error) {
	p.t.Error("the handler read the GitHub App without an admin principal")
	return store.GitHubAppInfo{}, nil
}

// The routes sit behind RequireAdmin; the settings and App handlers refuse a caller with no
// admin principal on their own, so a route moved out of the admin group does not expose them.
func TestSettingsAndAppHandlersEnforceTheAdminRoleThemselves(t *testing.T) {
	t.Parallel()

	settings := &handlers.SettingsHandlers{
		Settings: panicInstanceSettings{t},
		Config:   func() store.InstanceConfig { return store.InstanceConfig{} },
	}
	app := &handlers.GitHubAppHandlers{
		Info: panicAppInfo{t}, Installations: panicInstallations{t},
		Config: func() store.InstanceConfig { return store.InstanceConfig{} },
	}
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"GET /settings":               settings.Get,
		"PATCH /settings":             settings.Patch,
		"GET /settings/global-config": settings.GetGlobalConfig,
		"GET /github-app":             app.Get,
	} {
		rec := httptest.NewRecorder()
		call(rec, httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{"concurrencyCap":9}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a principal = %d %s, want 401", name, rec.Code, rec.Body.String())
		}
	}
}

// seedSession stores a session for a user and returns its cookie, for the handler tests whose
// routers sit behind RequireSession.
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
