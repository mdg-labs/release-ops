package api

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/setup"
)

const setupRoutesToken = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

func newSetupRoutesServer(t *testing.T) (*httptest.Server, *ServerDeps) {
	t.Helper()

	deps := newRoutesTestDeps(t)
	if err := setup.EnsureToken(context.Background(), deps.DB, setupRoutesToken, "https://release-ops.example.com", t.Logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	return srv, deps
}

func doSetupRequest(t *testing.T, client *http.Client, method, url, body string) (int, string, http.Header) {
	t.Helper()

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, url, err)
	}
	return resp.StatusCode, string(out), resp.Header
}

func TestSetupRoutesAreRegistered(t *testing.T) {
	t.Parallel()

	paths, err := ListRoutes(NewServerRouter(newRoutesTestDeps(t)).(chi.Router))
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	for _, want := range []string{"GET /api/v1/setup/status", "POST /api/v1/setup/session", "GET /api/v1/setup/session"} {
		if !slices.Contains(paths, want) {
			t.Errorf("registered paths are missing %s", want)
		}
	}
}

// The setup session opens the setup routes and nothing else: the protected API still answers
// 401 and the session endpoint still reports no user.
func TestSetupSessionDoesNotOpenTheProtectedAPI(t *testing.T) {
	t.Parallel()

	srv, _ := newSetupRoutesServer(t)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{Jar: jar}

	code, body, _ := doSetupRequest(t, client, http.MethodPost, srv.URL+"/api/v1/setup/session", `{"token":"`+setupRoutesToken+`"}`)
	if code != http.StatusNoContent || body != "" {
		t.Fatalf("redeem = %d %q, want 204 with no body", code, body)
	}
	if code, _, _ = doSetupRequest(t, client, http.MethodGet, srv.URL+"/api/v1/setup/session", ""); code != http.StatusOK {
		t.Fatalf("GET /setup/session with the setup session = %d, want 200", code)
	}

	for _, path := range []string{"/api/v1/status", "/api/v1/settings", "/api/v1/integrations", "/api/v1/users", "/api/v1/ticket-projects"} {
		if code, _, _ := doSetupRequest(t, client, http.MethodGet, srv.URL+path, ""); code != http.StatusUnauthorized {
			t.Errorf("GET %s with a setup session = %d, want 401", path, code)
		}
	}
	if code, body, _ := doSetupRequest(t, client, http.MethodPost, srv.URL+"/api/v1/auth/logout", ""); code != http.StatusUnauthorized {
		t.Errorf("POST logout with a setup session = %d %q, want 401", code, body)
	}
	_, body, _ = doSetupRequest(t, client, http.MethodGet, srv.URL+"/api/v1/auth/session", "")
	if !strings.Contains(body, `"user":null`) {
		t.Errorf("GET /auth/session with a setup session = %q, want no user", body)
	}
}

func TestSetupSessionRouteIsRateLimited(t *testing.T) {
	t.Parallel()

	srv, _ := newSetupRoutesServer(t)
	client := &http.Client{}
	url := srv.URL + "/api/v1/setup/session"

	for i := range 10 {
		code, body, _ := doSetupRequest(t, client, http.MethodPost, url, `{"token":"wrong"}`)
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %q, want 401", i+1, code, body)
		}
		if strings.Contains(body, setupRoutesToken) {
			t.Fatalf("attempt %d response contains the setup token", i+1)
		}
	}
	code, body, header := doSetupRequest(t, client, http.MethodPost, url, `{"token":"`+setupRoutesToken+`"}`)
	if code != http.StatusTooManyRequests || !strings.Contains(body, "RATE_LIMITED") || header.Get("Retry-After") == "" {
		t.Fatalf("attempt 11 = %d %q Retry-After %q, want 429 RATE_LIMITED with Retry-After", code, body, header.Get("Retry-After"))
	}
}

func TestSetupStatusRouteIsRateLimited(t *testing.T) {
	t.Parallel()

	srv, _ := newSetupRoutesServer(t)
	client := &http.Client{}
	url := srv.URL + "/api/v1/setup/status"

	for i := range 10 {
		code, body, _ := doSetupRequest(t, client, http.MethodGet, url, "")
		if code != http.StatusOK || !strings.Contains(body, `"setupRequired":true`) {
			t.Fatalf("request %d = %d %q, want 200 with setupRequired true", i+1, code, body)
		}
	}
	if code, _, header := doSetupRequest(t, client, http.MethodGet, url, ""); code != http.StatusTooManyRequests || header.Get("Retry-After") == "" {
		t.Fatalf("request 11 = %d, want 429 with Retry-After", code)
	}
}
