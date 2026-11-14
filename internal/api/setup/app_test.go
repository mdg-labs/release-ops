package setup_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/setup"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

type fakeConverter struct {
	calls int
}

func (f *fakeConverter) ConvertManifest(context.Context, string) (*github.ManifestConversion, error) {
	f.calls++
	return &github.ManifestConversion{
		ID: 7, Slug: "ro", HTMLURL: "https://github.com/apps/ro",
		Owner:    github.Owner{Login: "acme", Type: "Organization"},
		ClientID: "cid", ClientSecret: "cs", WebhookSecret: "ws", PEM: "pem",
	}, nil
}

func TestBuildManifest(t *testing.T) {
	t.Parallel()

	m := setup.BuildManifest("Acme", appURL, "")
	if m.Public || m.URL != appURL || m.RedirectURL != appURL+"/api/go/api/v1/setup/callback" || m.SetupURL != appURL+"/api/go/api/v1/setup/installed" ||
		len(m.CallbackURLs) != 1 || m.CallbackURLs[0] != appURL+"/api/go/api/v1/auth/github/callback" {
		t.Errorf("manifest = %+v", m)
	}
	if m.HookAttributes.Active || m.HookAttributes.URL != appURL {
		t.Errorf("hook without WEBHOOK_URL = %+v, want inactive at APP_URL", m.HookAttributes)
	}
	if h := setup.BuildManifest("Acme", appURL, "https://hooks.example.com/x").HookAttributes; !h.Active || h.URL != "https://hooks.example.com/x" {
		t.Errorf("hook with WEBHOOK_URL = %+v, want active at WEBHOOK_URL", h)
	}
	raw, err := json.Marshal(m)
	if err != nil || !strings.Contains(string(raw), `"public":false`) {
		t.Errorf("marshalled manifest = %s, %v; want an explicit public:false", raw, err)
	}
	for name, access := range map[string]string{
		"administration": "read", "checks": "write", "statuses": "write", "contents": "write",
		"vulnerability_alerts": "read", "issues": "write", "metadata": "read", "pull_requests": "write",
		"workflows": "write", "merge_queues": "write", "members": "read",
	} {
		if m.DefaultPermissions[name] != access {
			t.Errorf("permission %s = %q, want %q", name, m.DefaultPermissions[name], access)
		}
	}
	if len(m.DefaultPermissions) != 11 {
		t.Errorf("permissions = %v, want exactly the eleven of the spec", m.DefaultPermissions)
	}
}

func TestValidLogin(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"a", "acme", "Acme-Corp", "a1-b2", strings.Repeat("a", 39)} {
		if !setup.ValidLogin(ok) {
			t.Errorf("ValidLogin(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "a--b", "a_b", "a.b", "a b", "a/b", "../x", "a?b", "a#b", "é", strings.Repeat("a", 40)} {
		if setup.ValidLogin(bad) {
			t.Errorf("ValidLogin(%q) = true, want false", bad)
		}
	}
}

func TestNormalizeAppName(t *testing.T) {
	t.Parallel()

	if got, ok := setup.NormalizeAppName("  "); !ok || got != setup.DefaultAppName {
		t.Errorf("blank name = %q, %v, want the default", got, ok)
	}
	if got, ok := setup.NormalizeAppName(" Acme "); !ok || got != "Acme" {
		t.Errorf("padded name = %q, %v", got, ok)
	}
	if _, ok := setup.NormalizeAppName(strings.Repeat("é", 35)); ok {
		t.Error("a 35-character name was accepted")
	}
	if _, ok := setup.NormalizeAppName(strings.Repeat("é", 34)); !ok {
		t.Error("a 34-character name was refused")
	}
	if _, ok := setup.NormalizeAppName("a\nb"); ok {
		t.Error("a name with a control character was accepted")
	}
}

func TestRegistrationURL(t *testing.T) {
	t.Parallel()

	if got, want := setup.RegistrationURL("https://github.com", "user", "", "s+/="), "https://github.com/settings/apps/new?state=s%2B%2F%3D"; got != want {
		t.Errorf("user URL = %q, want %q", got, want)
	}
	if got, want := setup.RegistrationURL("https://ghe.example.com", "organization", "acme", "s"), "https://ghe.example.com/organizations/acme/settings/apps/new?state=s"; got != want {
		t.Errorf("org URL = %q, want %q", got, want)
	}
}

// The state of a manifest attempt lives one hour at most, and a mismatching callback does
// not spend the attempt.
func TestCallbackStateLifetimeAndMismatch(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.EnsureToken(context.Background(), db.Writer, configToken, appURL, t.Logf); err != nil {
		t.Fatal(err)
	}
	clk := &clock{now: time.Now()}
	conv := &fakeConverter{}
	sm := auth.NewSessionManager(db.Writer, strings.Repeat("s", 32), false)
	h := &setup.Handlers{
		Sessions: sm, Queries: storedb.New(db.Writer), Now: clk.Now, TTL: 3 * time.Hour,
		Apps: store.New(db, cipher).GitHubApp(), Converter: conv,
		AppURL: appURL, GitHubBaseURL: "https://github.com",
	}
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Post("/setup/session", h.CreateSession)
	r.With(h.RequireSession).Post("/setup/manifest", h.Manifest)
	r.With(h.RequireCallbackSession).Get("/setup/callback", h.Callback)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	client := newClient(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	callback := func(code, state string) (int, string) {
		t.Helper()
		resp, err := client.Get(srv.URL + "/setup/callback?code=" + code + "&state=" + url.QueryEscape(state))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode, resp.Header.Get("Location")
	}
	post := func(path, body string) (int, string) {
		t.Helper()
		resp, err := client.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(out)
	}
	startAttempt := func() string {
		t.Helper()
		code, body := post("/setup/manifest", `{"owner":{"type":"user"}}`)
		if code != http.StatusOK {
			t.Fatalf("manifest = %d %q", code, body)
		}
		var reply struct{ URL string }
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatal(err)
		}
		return strings.SplitN(reply.URL, "state=", 2)[1]
	}

	if code, _ := post("/setup/session", `{"token":"`+configToken+`"}`); code != http.StatusNoContent {
		t.Fatalf("redeem = %d", code)
	}

	state := startAttempt()
	if code, location := callback("abc", "nope"); code != http.StatusFound || location != "/setup?error=invalid_state" {
		t.Fatalf("mismatching state = %d to %q, want 302 to the invalid_state error", code, location)
	}
	if conv.calls != 0 {
		t.Fatal("a mismatching state reached the converter")
	}

	clk.advance(setup.StateTTL + time.Second)
	if code, location := callback("abc", state); code != http.StatusFound || location != "/setup?error=invalid_state" {
		t.Errorf("state after an hour = %d to %q, want 302 to the invalid_state error", code, location)
	}
	if conv.calls != 0 {
		t.Fatal("an expired state reached the converter")
	}

	// A fresh attempt replaces the stale one and works.
	state = startAttempt()
	if code, location := callback("abc", state); code != http.StatusFound || location != "https://github.com/apps/ro/installations/new" {
		t.Errorf("fresh attempt = %d to %q, want 302 to the installation page", code, location)
	}
	if conv.calls != 1 {
		t.Errorf("converter calls = %d, want 1", conv.calls)
	}
}

// A callback after the setup session has ended sends the browser back to /setup instead of
// answering 401 JSON, and never reaches the converter.
func TestCallbackWithAnExpiredSetupSessionRedirectsToSetup(t *testing.T) {
	t.Parallel()

	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := setup.EnsureToken(context.Background(), db.Writer, configToken, appURL, t.Logf); err != nil {
		t.Fatal(err)
	}
	clk := &clock{now: time.Now()}
	conv := &fakeConverter{}
	sm := auth.NewSessionManager(db.Writer, strings.Repeat("s", 32), false)
	h := &setup.Handlers{
		Sessions: sm, Queries: storedb.New(db.Writer), Now: clk.Now,
		Apps: store.New(db, cipher).GitHubApp(), Converter: conv,
		AppURL: appURL, GitHubBaseURL: "https://github.com",
	}
	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Post("/setup/session", h.CreateSession)
	r.With(h.RequireSession).Post("/setup/manifest", h.Manifest)
	r.With(h.RequireCallbackSession).Get("/setup/callback", h.Callback)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	client := newClient(t)
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	callback := func() (int, string) {
		t.Helper()
		resp, err := client.Get(srv.URL + "/setup/callback?code=abc&state=whatever")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode, resp.Header.Get("Location")
	}

	resp, err := client.Post(srv.URL+"/setup/session", "application/json", strings.NewReader(`{"token":"`+configToken+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("redeem = %d", resp.StatusCode)
	}

	clk.advance(setup.SessionTTL + time.Second)
	if code, location := callback(); code != http.StatusFound || location != "/setup?error=session_expired" {
		t.Errorf("callback after the setup session ended = %d to %q, want 302 to the session_expired error", code, location)
	}
	// The session was destroyed, so the next request has none and gets the same answer.
	if code, location := callback(); code != http.StatusFound || location != "/setup?error=session_expired" {
		t.Errorf("callback without a setup session = %d to %q, want 302 to the session_expired error", code, location)
	}
	// Another setup route keeps answering 401.
	resp, err = client.Post(srv.URL+"/setup/manifest", "application/json", strings.NewReader(`{"owner":{"type":"user"}}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("POST /setup/manifest without a setup session = %d, want 401", resp.StatusCode)
	}
	if conv.calls != 0 {
		t.Error("a callback without a setup session reached the converter")
	}
}

func TestStatusOffersTheInstallPageOnlyUntilTheAppIsInstalled(t *testing.T) {
	t.Parallel()

	e := newEnv(t, configToken)
	get := func() string {
		t.Helper()
		resp, err := e.client.Get(e.srv.URL + "/setup/status")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}

	if body := get(); strings.Contains(body, "installUrl") {
		t.Errorf("status during setup = %q, want no installUrl", body)
	}
	insertApp(t, e.db)
	if body := get(); !strings.Contains(body, `"setupRequired":false`) || !strings.Contains(body, `"installUrl":"/apps/release-ops/installations/new"`) {
		t.Errorf("status with an App and no installation = %q, want its installUrl", body)
	}
	if _, err := e.db.Exec(`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		VALUES ('i1', 1, 1, 'acme', 'Organization', 'active', 'all', 't', 't')`); err != nil {
		t.Fatal(err)
	}
	if body := get(); strings.Contains(body, "installUrl") {
		t.Errorf("status with an installation = %q, want no installUrl", body)
	}
}
