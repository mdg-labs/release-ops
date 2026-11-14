package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
)

type installedFlow struct {
	srv    *httptest.Server
	deps   *ServerDeps
	github *githubtest.Fake
	client *http.Client
}

// newInstalledFlow serves the real router over a stored App and a fake GitHub that knows
// installation 1000 of that App. withApp false leaves the instance before setup.
func newInstalledFlow(t *testing.T, withApp bool) *installedFlow {
	t.Helper()

	fake := githubtest.New(t, time.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	deps := newRoutesTestDeps(t)
	deps.AppURL = manifestAppURL
	deps.GitHubBaseURL = manifestGitHubURL
	deps.GitHub = github.NewClient(fake.URL, 5*time.Second)
	if withApp {
		if err := deps.Store.GitHubApp().Create(context.Background(), store.NewGitHubApp{
			AppID: githubtest.AppID, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
			OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: string(fake.PEM), ClientSecret: "client-secret-value", WebhookSecret: "webhook-secret-value",
		}); err != nil {
			t.Fatalf("store app: %v", err)
		}
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &installedFlow{srv: srv, deps: deps, github: fake, client: client}
}

func (f *installedFlow) installed(t *testing.T, query string) (int, string, http.Header) {
	t.Helper()
	return doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/installed"+query, "")
}

func (f *installedFlow) installations(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.deps.DB.QueryRow(`SELECT COUNT(*) FROM installations`).Scan(&n); err != nil {
		t.Fatalf("count installations: %v", err)
	}
	return n
}

func TestInstalledRecordsTheInstallationGitHubConfirms(t *testing.T) {
	t.Parallel()

	f := newInstalledFlow(t, true)
	code, body, header := f.installed(t, "?installation_id=1000&setup_action=install")
	if code != http.StatusFound || header.Get("Location") != "/login" {
		t.Fatalf("GET /setup/installed = %d %q to %q, want 302 to /login", code, body, header.Get("Location"))
	}

	var ghID, accountID int64
	var login, accountType, status, selection, permissions string
	err := f.deps.DB.QueryRow(`SELECT github_installation_id, account_id, account_login, account_type, status, repository_selection, permissions FROM installations`).
		Scan(&ghID, &accountID, &login, &accountType, &status, &selection, &permissions)
	if err != nil {
		t.Fatalf("read installation: %v", err)
	}
	if ghID != 1000 || accountID != 4242 || login != "acme" || accountType != "Organization" || status != "active" || selection != "selected" ||
		!strings.Contains(permissions, `"contents":"write"`) {
		t.Errorf("stored installation = %d %d %s %s %s %s %s", ghID, accountID, login, accountType, status, selection, permissions)
	}

	reqs := f.github.Requests()
	if len(reqs) != 1 || reqs[0].Path != "/app/installations/1000" || !reqs[0].JWTValid {
		t.Errorf("requests to GitHub = %+v, want one App-JWT call for installation 1000", reqs)
	}

	// With the installation recorded the status no longer points at the installation page.
	code, body, _ = doSetupRequest(t, f.client, http.MethodGet, f.srv.URL+"/api/v1/setup/status", "")
	if code != http.StatusOK || strings.Contains(body, "installUrl") {
		t.Errorf("status after install = %d %q, want no installUrl", code, body)
	}

	// A repeat of the redirect is answered the same and changes nothing.
	if code, _, header := f.installed(t, "?installation_id=1000&setup_action=update"); code != http.StatusFound || header.Get("Location") != "/login" {
		t.Errorf("repeat = %d to %q, want 302 to /login", code, header.Get("Location"))
	}
	if n := f.installations(t); n != 1 {
		t.Errorf("installations = %d, want 1", n)
	}
}

func TestInstalledRefusesWhatGitHubDoesNotConfirm(t *testing.T) {
	t.Parallel()

	f := newInstalledFlow(t, true)
	f.github.AddInstallation(3000, githubtest.AppID+1) // an installation of another App

	for name, c := range map[string]struct {
		query string
		want  int
	}{
		"missing":       {"", http.StatusBadRequest},
		"not a number":  {"?installation_id=abc", http.StatusBadRequest},
		"negative":      {"?installation_id=-1", http.StatusBadRequest},
		"zero":          {"?installation_id=0", http.StatusBadRequest},
		"signed":        {"?installation_id=%2B1000", http.StatusBadRequest},
		"exponent":      {"?installation_id=1e3", http.StatusBadRequest},
		"too long":      {"?installation_id=99999999999999999999", http.StatusBadRequest},
		"path":          {"?installation_id=" + url.QueryEscape("1000/../x"), http.StatusBadRequest},
		"unknown":       {"?installation_id=999", http.StatusNotFound},
		"foreign app's": {"?installation_id=3000", http.StatusNotFound},
	} {
		code, body, header := f.installed(t, c.query)
		if code != c.want || header.Get("Location") != "" {
			t.Errorf("%s: GET /setup/installed%s = %d %q, want %d and no redirect", name, c.query, code, body, c.want)
		}
	}
	if n := f.installations(t); n != 0 {
		t.Errorf("installations = %d after refused requests, want 0", n)
	}
	for _, r := range f.github.Requests() {
		if strings.Contains(r.Path, "1000") && r.Path != "/app/installations/1000" {
			t.Errorf("a malformed id reached GitHub: %+v", r)
		}
	}
	if len(f.github.Requests()) != 2 {
		t.Errorf("requests to GitHub = %+v, want only the two well-formed ids", f.github.Requests())
	}
}

func TestInstalledWritesNothingWhenGitHubFails(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		f := newInstalledFlow(t, true)
		f.github.FailInstallations(status)
		code, body, header := f.installed(t, "?installation_id=1000")
		if code != http.StatusBadGateway || header.Get("Location") != "" || f.installations(t) != 0 {
			t.Errorf("GitHub %d: = %d %q, installations %d, want 502 and nothing written", status, code, body, f.installations(t))
		}
		if strings.Contains(body, "BEGIN") {
			t.Errorf("GitHub %d: body carries key material: %s", status, body)
		}
	}
}

func TestInstalledRefusesASecondInstallation(t *testing.T) {
	t.Parallel()

	f := newInstalledFlow(t, true)
	f.github.AddInstallation(2000, githubtest.AppID)
	if code, _, _ := f.installed(t, "?installation_id=1000"); code != http.StatusFound {
		t.Fatalf("first installation = %d, want 302", code)
	}
	calls := len(f.github.Requests())

	code, _, header := f.installed(t, "?installation_id=2000")
	if code != http.StatusFound || header.Get("Location") != "/login" {
		t.Errorf("second installation = %d to %q, want 302 to /login", code, header.Get("Location"))
	}
	var id int64
	if err := f.deps.DB.QueryRow(`SELECT github_installation_id FROM installations`).Scan(&id); err != nil || id != 1000 || f.installations(t) != 1 {
		t.Errorf("installations after the second: %d, %v, want only 1000", id, err)
	}
	if len(f.github.Requests()) != calls {
		t.Error("the second installation was looked up at GitHub")
	}
}

func TestInstalledBeforeSetupIs404(t *testing.T) {
	t.Parallel()

	f := newInstalledFlow(t, false)
	if code, _, _ := f.installed(t, "?installation_id=1000"); code != http.StatusNotFound {
		t.Errorf("installed before an App exists = %d, want 404", code)
	}
	if len(f.github.Requests()) != 0 || f.installations(t) != 0 {
		t.Error("installed before an App exists reached GitHub or wrote a row")
	}
}
