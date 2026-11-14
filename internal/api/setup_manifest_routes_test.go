package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
)

const (
	manifestAppURL    = "https://release-ops.example.com"
	manifestGitHubURL = "https://github.com"
	manifestWebhook   = "https://hooks.example.com/github"
)

var manifestSecrets = []string{"fixture-client-secret-value", "fixture-webhook-secret-value", "fixture-private-key-body"}

type fakeGitHub struct {
	srv   *httptest.Server
	calls atomic.Int32
}

// newFakeGitHub serves the conversion endpoint with respond; the default answers 201 with
// the documented-schema fixture.
func newFakeGitHub(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) *fakeGitHub {
	t.Helper()
	fixture, err := os.ReadFile("../github/testdata/manifest_conversion.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	f := &fakeGitHub{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if respond != nil {
			respond(w, r)
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/conversions") {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

type manifestFlow struct {
	srv    *httptest.Server
	deps   *ServerDeps
	github *fakeGitHub
	client *http.Client
}

func newManifestFlow(t *testing.T, gh *fakeGitHub, webhookURL string, timeout time.Duration) *manifestFlow {
	t.Helper()

	deps := newRoutesTestDeps(t)
	deps.AppURL = manifestAppURL
	deps.WebhookURL = webhookURL
	deps.GitHubBaseURL = manifestGitHubURL
	deps.GitHub = github.NewClient(gh.srv.URL, timeout)
	if err := setup.EnsureToken(context.Background(), deps.DB, setupRoutesToken, manifestAppURL, t.Logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	// The callback answers with redirects the browser follows; the tests read them instead.
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &manifestFlow{srv: srv, deps: deps, github: gh, client: client}
}

func (f *manifestFlow) do(t *testing.T, method, path, body string) (int, string) {
	t.Helper()
	code, out, _ := doSetupRequest(t, f.client, method, f.srv.URL+"/api/v1/setup"+path, body)
	return code, out
}

func (f *manifestFlow) openSession(t *testing.T) {
	t.Helper()
	if code, body := f.do(t, http.MethodPost, "/session", `{"token":"`+setupRoutesToken+`"}`); code != http.StatusNoContent {
		t.Fatalf("redeem = %d %q", code, body)
	}
}

type manifestReply struct {
	URL      string `json:"url"`
	Manifest struct {
		Name           string `json:"name"`
		URL            string `json:"url"`
		RedirectURL    string `json:"redirect_url"`
		CallbackURLs   []string
		SetupURL       string            `json:"setup_url"`
		Public         *bool             `json:"public"`
		Permissions    map[string]string `json:"default_permissions"`
		Events         []string          `json:"default_events"`
		HookAttributes struct {
			URL    string `json:"url"`
			Active bool   `json:"active"`
		} `json:"hook_attributes"`
	} `json:"manifest"`
}

func (f *manifestFlow) startManifest(t *testing.T, body string) (manifestReply, string) {
	t.Helper()
	code, out := f.do(t, http.MethodPost, "/manifest", body)
	if code != http.StatusOK {
		t.Fatalf("POST /setup/manifest = %d %q, want 200", code, out)
	}
	var reply manifestReply
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatalf("decode manifest reply %q: %v", out, err)
	}
	state := strings.SplitN(reply.URL, "state=", 2)
	if len(state) != 2 || state[1] == "" {
		t.Fatalf("registration URL %q carries no state", reply.URL)
	}
	return reply, state[1]
}

func (f *manifestFlow) githubAppRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.deps.DB.QueryRow(`SELECT COUNT(*) FROM github_app`).Scan(&n); err != nil {
		t.Fatalf("count github_app: %v", err)
	}
	return n
}

// callback GETs /setup/callback the way GitHub's redirect does and returns the status and the
// Location of the answer.
func (f *manifestFlow) callback(t *testing.T, code, state string) (int, string) {
	t.Helper()
	q := url.Values{}
	if code != "" {
		q.Set("code", code)
	}
	if state != "" {
		q.Set("state", state)
	}
	status, _, header := doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/callback?"+q.Encode(), "")
	return status, header.Get("Location")
}

func setupErrorRedirect(code string) string {
	return "/setup?error=" + code
}

func TestSetupManifestFlowCreatesTheApp(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	f := newManifestFlow(t, newFakeGitHub(t, nil), manifestWebhook, 0)
	f.openSession(t)

	reply, state := f.startManifest(t, `{"owner":{"type":"organization","login":"acme"},"name":"Acme Releases"}`)
	if want := manifestGitHubURL + "/organizations/acme/settings/apps/new?state="; !strings.HasPrefix(reply.URL, want) {
		t.Errorf("registration URL = %q, want prefix %q", reply.URL, want)
	}
	m := reply.Manifest
	if m.Name != "Acme Releases" || m.URL != manifestAppURL ||
		m.RedirectURL != manifestAppURL+"/api/go/api/v1/setup/callback" ||
		m.SetupURL != manifestAppURL+"/api/go/api/v1/setup/installed" ||
		m.HookAttributes.URL != manifestWebhook || !m.HookAttributes.Active {
		t.Errorf("manifest = %+v", m)
	}
	if m.Public == nil || *m.Public {
		t.Errorf("manifest public = %v, want an explicit false", m.Public)
	}
	if m.Permissions["merge_queues"] != "write" || m.Permissions["members"] != "read" || m.Permissions["contents"] != "write" {
		t.Errorf("manifest permissions = %v", m.Permissions)
	}
	if len(m.Events) != 5 {
		t.Errorf("manifest events = %v", m.Events)
	}

	code, location := f.callback(t, "abc123", state)
	if want := "https://github.com/apps/release-ops-acme/installations/new"; code != http.StatusFound || location != want {
		t.Fatalf("GET /setup/callback = %d to %q, want 302 to %q", code, location, want)
	}

	var appID int64
	var slug, owner, ownerType, keyID, privateKey, clientSecret, webhookSecret string
	var webhookActive int
	err := f.deps.DB.QueryRow(`SELECT app_id, slug, owner_login, owner_type, webhook_active, key_id,
		encrypted_private_key, encrypted_client_secret, encrypted_webhook_secret FROM github_app`).
		Scan(&appID, &slug, &owner, &ownerType, &webhookActive, &keyID, &privateKey, &clientSecret, &webhookSecret)
	if err != nil {
		t.Fatalf("read github_app: %v", err)
	}
	cipher, err := crypto.NewCipherFromHex(routesTestKeyHex)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	if appID != 1234567 || slug != "release-ops-acme" || owner != "acme" || ownerType != "Organization" || webhookActive != 1 || keyID != cipher.KeyID() {
		t.Errorf("stored App = %d %s %s %s %d %s", appID, slug, owner, ownerType, webhookActive, keyID)
	}
	for _, stored := range []string{privateKey, clientSecret, webhookSecret} {
		if plain, err := cipher.Decrypt(stored); err != nil || len(plain) == 0 {
			t.Errorf("stored secret does not decrypt: %v", err)
		}
		for _, s := range manifestSecrets {
			if strings.Contains(stored, s) {
				t.Errorf("a secret is stored in plaintext: %q", stored)
			}
		}
	}

	var tokens int
	if err := f.deps.DB.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'setup' AND used_at IS NULL`).Scan(&tokens); err != nil || tokens != 0 {
		t.Errorf("unused setup tokens after the App is stored = %d, %v", tokens, err)
	}

	// Setup is over: every setup route answers 404, and a second callback with a
	// valid-looking code and state changes nothing and never reaches GitHub.
	calls := f.github.calls.Load()
	for _, r := range []struct{ method, path, body string }{
		{http.MethodGet, "/session", ""},
		{http.MethodPost, "/session", `{"token":"` + setupRoutesToken + `"}`},
		{http.MethodPost, "/manifest", `{"owner":{"type":"user"}}`},
		{http.MethodGet, "/callback?code=abc123&state=" + url.QueryEscape(state), ""},
	} {
		if code, body := f.do(t, r.method, r.path, r.body); code != http.StatusNotFound {
			t.Errorf("%s /setup%s after setup = %d %q, want 404", r.method, r.path, code, body)
		}
	}
	if f.github.calls.Load() != calls {
		t.Error("a callback after setup reached GitHub")
	}
	if f.githubAppRows(t) != 1 {
		t.Errorf("github_app rows = %d, want 1", f.githubAppRows(t))
	}

	// With no installation yet, the status points the operator at the App's installation page.
	code, body := f.do(t, http.MethodGet, "/status", "")
	if code != http.StatusOK || !strings.Contains(body, `"setupRequired":false`) ||
		!strings.Contains(body, `"installUrl":"https://github.com/apps/release-ops-acme/installations/new"`) {
		t.Errorf("status after setup = %d %q", code, body)
	}

	// GitHub's setup_url redirect arrives after the App is stored, so it stays public. The
	// installation it names is already recorded here, so the redirect is answered without GitHub.
	if _, err := f.deps.DB.Exec(`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		VALUES ('inst-1', 1, 1, 'acme', 'Organization', 'active', 'all', '2026-11-14T12:00:00.000Z', '2026-11-14T12:00:00.000Z')`); err != nil {
		t.Fatalf("seed installation: %v", err)
	}
	if code, _, header := doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/installed?installation_id=1&setup_action=install", ""); code != http.StatusFound || header.Get("Location") != "/login" {
		t.Errorf("GET /setup/installed after setup = %d to %q, want 302 to /login", code, header.Get("Location"))
	}

	for _, s := range manifestSecrets {
		if strings.Contains(logs.String(), s) || strings.Contains(body, s) {
			t.Errorf("secret %q appears in the logs or a response", s)
		}
	}
}

func TestSetupManifestUsesConfiguredURLsOnly(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, nil), "", 0)
	f.openSession(t)

	req, err := http.NewRequest(http.MethodPost, f.srv.URL+"/api/v1/setup/manifest", strings.NewReader(`{"owner":{"type":"user"}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("Origin", "https://attacker.example")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var reply manifestReply
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("manifest with a spoofed forwarded host = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(reply)
	if strings.Contains(string(raw), "attacker.example") {
		t.Errorf("manifest carries the request host: %s", raw)
	}
	m := reply.Manifest
	if m.Name != "Release Ops" || m.HookAttributes.Active || m.HookAttributes.URL != manifestAppURL {
		t.Errorf("manifest without WEBHOOK_URL = name %q hook %+v, want default name and an inactive hook at APP_URL", m.Name, m.HookAttributes)
	}
	if want := manifestGitHubURL + "/settings/apps/new?state="; !strings.HasPrefix(reply.URL, want) {
		t.Errorf("personal registration URL = %q, want prefix %q", reply.URL, want)
	}
}

func TestSetupManifestRejectsBadInput(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, nil), "", 0)
	f.openSession(t)

	for name, body := range map[string]string{
		"path traversal in org": `{"owner":{"type":"organization","login":"../evil"}}`,
		"slash in org":          `{"owner":{"type":"organization","login":"a/b"}}`,
		"query in org":          `{"owner":{"type":"organization","login":"a?x=1"}}`,
		"leading hyphen":        `{"owner":{"type":"organization","login":"-a"}}`,
		"double hyphen":         `{"owner":{"type":"organization","login":"a--b"}}`,
		"too long":              `{"owner":{"type":"organization","login":"` + strings.Repeat("a", 40) + `"}}`,
		"org without login":     `{"owner":{"type":"organization"}}`,
		"unknown owner type":    `{"owner":{"type":"team","login":"acme"}}`,
		"name too long":         `{"owner":{"type":"user"},"name":"` + strings.Repeat("n", 35) + `"}`,
		"not json":              `nope`,
	} {
		if code, out := f.do(t, http.MethodPost, "/manifest", body); code != http.StatusBadRequest {
			t.Errorf("%s: = %d %q, want 400", name, code, out)
		}
	}
}

func TestSetupManifestRoutesNeedASetupSession(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, nil), "", 0)
	if code, body := f.do(t, http.MethodPost, "/manifest", `{"owner":{"type":"user"}}`); code != http.StatusUnauthorized {
		t.Errorf("POST /setup/manifest without a session = %d %q, want 401", code, body)
	}
	// The callback is GitHub's top-level redirect, so without a session it sends the
	// browser back to /setup instead of answering 401 JSON.
	if code, location := f.callback(t, "abc123", "whatever"); code != http.StatusFound || location != setupErrorRedirect("session_expired") {
		t.Errorf("GET /setup/callback without a session = %d to %q, want 302 to the session_expired error", code, location)
	}
	if f.github.calls.Load() != 0 {
		t.Error("a request without a setup session reached GitHub")
	}
}

func TestSetupCallbackRefusesAMissingOrWrongState(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, nil), "", 0)
	f.openSession(t)

	// No attempt started yet: nothing to match.
	if code, location := f.callback(t, "abc123", "anything"); code != http.StatusFound || location != setupErrorRedirect("invalid_state") {
		t.Errorf("callback before a manifest = %d to %q, want 302 to the invalid_state error", code, location)
	}

	_, state := f.startManifest(t, `{"owner":{"type":"user"}}`)
	for name, badState := range map[string]string{
		"missing":   "",
		"wrong":     "not-" + state,
		"truncated": state[:len(state)-1],
		"huge":      strings.Repeat("s", 3000),
	} {
		if code, location := f.callback(t, "abc123", badState); code != http.StatusFound || location != setupErrorRedirect("invalid_state") {
			t.Errorf("%s state = %d to %q, want 302 to the invalid_state error", name, code, location)
		}
	}
	if f.github.calls.Load() != 0 {
		t.Error("a request with a bad state reached GitHub")
	}
	if f.githubAppRows(t) != 0 {
		t.Error("a request with a bad state stored an App")
	}

	// A forged attempt does not cancel the real one.
	if code, location := f.callback(t, "abc123", state); code != http.StatusFound || !strings.HasPrefix(location, manifestGitHubURL+"/apps/") {
		t.Errorf("the real callback after forged ones = %d to %q, want 302 to the installation page", code, location)
	}
}

func TestSetupCallbackReplayIsRefused(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}), "", 0)
	f.openSession(t)
	_, state := f.startManifest(t, `{"owner":{"type":"user"}}`)

	if code, location := f.callback(t, "abc123", state); code != http.StatusFound || location != setupErrorRedirect("conversion_rejected") {
		t.Fatalf("first callback = %d to %q, want 302 to the conversion_rejected error", code, location)
	}
	if code, location := f.callback(t, "abc123", state); code != http.StatusFound || location != setupErrorRedirect("invalid_state") {
		t.Errorf("replayed callback = %d to %q, want 302 to the invalid_state error", code, location)
	}
	if f.github.calls.Load() != 1 {
		t.Errorf("GitHub calls = %d, want 1: the replay must not reach GitHub", f.github.calls.Load())
	}
}

func TestSetupCallbackConversionFailureStoresNothing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		respond func(w http.ResponseWriter, r *http.Request)
		timeout time.Duration
		wantErr string
	}{
		{"github 404", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, 0, "conversion_rejected"},
		{"github 422", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnprocessableEntity) }, 0, "conversion_rejected"},
		{"github 500", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, 0, "conversion_failed"},
		{"github garbage", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":1}`))
		}, 0, "conversion_failed"},
		{"github timeout", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, 100 * time.Millisecond, "conversion_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newManifestFlow(t, newFakeGitHub(t, tt.respond), "", tt.timeout)
			f.openSession(t)
			_, state := f.startManifest(t, `{"owner":{"type":"user"}}`)

			code, location := f.callback(t, "abc123", state)
			if code != http.StatusFound || location != setupErrorRedirect(tt.wantErr) {
				t.Fatalf("callback = %d to %q, want 302 to the %s error", code, location, tt.wantErr)
			}
			if f.githubAppRows(t) != 0 {
				t.Error("a failed conversion left a github_app row")
			}
			// The instance is still in setup mode, and the admin can start again.
			if code, body := f.do(t, http.MethodGet, "/status", ""); code != http.StatusOK || !strings.Contains(body, `"setupRequired":true`) {
				t.Errorf("status after a failed conversion = %d %q, want setup still required", code, body)
			}
			if code, body := f.do(t, http.MethodPost, "/manifest", `{"owner":{"type":"user"}}`); code != http.StatusOK {
				t.Errorf("a new attempt after a failed conversion = %d %q, want 200", code, body)
			}
		})
	}
}

func TestSetupManifestFlowRoutesAreRateLimitedEach(t *testing.T) {
	t.Parallel()

	f := newManifestFlow(t, newFakeGitHub(t, nil), "", 0)
	f.openSession(t)

	for i := range 10 {
		if code, body := f.do(t, http.MethodPost, "/manifest", `{"owner":{"type":"user"}}`); code != http.StatusOK {
			t.Fatalf("manifest %d = %d %q", i+1, code, body)
		}
	}
	code, body, header := doSetupRequest(t, f.client, http.MethodPost, f.srv.URL+"/api/v1/setup/manifest", `{"owner":{"type":"user"}}`)
	if code != http.StatusTooManyRequests || header.Get("Retry-After") == "" {
		t.Fatalf("manifest 11 = %d %q, want 429 with Retry-After", code, body)
	}

	// The callback has its own budget.
	for i := range 10 {
		if code, location := f.callback(t, "abc123", "wrong"); code != http.StatusFound {
			t.Fatalf("callback %d = %d to %q, want 302", i+1, code, location)
		}
	}
	if code, _, header := doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/callback?code=abc123&state=wrong", ""); code != http.StatusTooManyRequests || header.Get("Retry-After") == "" {
		t.Fatalf("callback 11 = %d, want 429 with Retry-After", code)
	}

	// So does the public installed route, which needs no session; with no App stored it answers 404.
	for i := range 10 {
		if code, _, _ := doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/installed?installation_id=1&setup_action=install", ""); code != http.StatusNotFound {
			t.Fatalf("installed %d = %d, want 404", i+1, code)
		}
	}
	if code, _, header := doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/installed", ""); code != http.StatusTooManyRequests || header.Get("Retry-After") == "" {
		t.Fatalf("installed 11 = %d, want 429 with Retry-After", code)
	}
}
