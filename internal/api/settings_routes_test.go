package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/buildinfo"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

func newSettingsAdmin(t *testing.T) (*rolesServer, *http.Client) {
	t.Helper()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	return s, s.signIn(t)
}

func getMap(t *testing.T, s *rolesServer, c *http.Client, path string) map[string]any {
	t.Helper()
	code, body := s.send(t, c, http.MethodGet, path, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want 200", path, code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("GET %s: %v in %s", path, err, body)
	}
	return out
}

func storedCap(t *testing.T, s *rolesServer) int {
	t.Helper()
	return rowCount(t, s.deps, `SELECT concurrency_cap FROM instance_settings WHERE id = 1`)
}

func TestSettingsReportTheCapTheModeAndTheConfiguredGlobalConfig(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	s.deps.Store.SetInstanceConfig(store.InstanceConfig{
		PresetRepositories: []string{"acme/presets"}, GlobalConfigRepo: "acme/renovate", GlobalConfigRef: "main",
	})

	got := getMap(t, s, admin, "/api/v1/settings")
	if got["concurrencyCap"] != float64(2) || got["mode"] != "polling" {
		t.Errorf("settings = %v, want the default cap 2 and polling mode (no WEBHOOK_URL)", got)
	}
	if presets, _ := got["presetRepositories"].([]any); len(presets) != 1 || presets[0] != "acme/presets" {
		t.Errorf("presetRepositories = %v", got["presetRepositories"])
	}
	want := map[string]any{"source": "repository", "repository": "acme/renovate", "ref": "main"}
	if gc, _ := got["globalConfig"].(map[string]any); len(gc) != 3 || gc["source"] != want["source"] || gc["repository"] != want["repository"] || gc["ref"] != want["ref"] {
		t.Errorf("globalConfig = %v, want %v", got["globalConfig"], want)
	}
	for _, removed := range []string{"inviteTokenExpiryHours", "passwordResetTokenExpiryMinutes", "smtpConfigured", "pollIntervalMinutes"} {
		if _, ok := got[removed]; ok {
			t.Errorf("settings still carries %s", removed)
		}
	}
}

func TestSettingsReportWebhookModeWhenAWebhookIsConfigured(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	s.deps.WebhookURL = "https://release-ops.example.com/hooks/github"
	httpSrv := httptest.NewServer(NewServerRouter(s.deps))
	t.Cleanup(httpSrv.Close)
	srv := &rolesServer{srv: httpSrv, deps: s.deps, fake: s.fake}
	admin := srv.signIn(t)
	if got := getMap(t, srv, admin, "/api/v1/settings"); got["mode"] != "webhook" {
		t.Errorf("mode = %v, want webhook", got["mode"])
	}
}

func TestPatchingTheConcurrencyCapStoresItAndKeepsTheBootstrapAdmin(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	const at = "2026-11-14T12:00:00.000Z"
	if _, err := s.deps.DB.Exec(`INSERT INTO users (id, created_at, updated_at) VALUES ('boot', ?, ?)`, at, at); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := s.deps.DB.Exec(`INSERT INTO instance_settings (id, bootstrap_admin_user_id, updated_at) VALUES (1, 'boot', ?)`, at); err != nil {
		t.Fatalf("seed instance settings: %v", err)
	}

	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{"concurrencyCap":5}`)
	if code != http.StatusOK || !strings.Contains(body, `"concurrencyCap":5`) {
		t.Fatalf("PATCH = %d %s, want 200 with the new cap", code, body)
	}
	if n := storedCap(t, s); n != 5 {
		t.Errorf("stored cap = %d, want 5", n)
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM instance_settings WHERE bootstrap_admin_user_id = 'boot'`); n != 1 {
		t.Errorf("the bootstrap admin of the row = %d rows, want it kept", n)
	}
	if got := getMap(t, s, admin, "/api/v1/settings"); got["concurrencyCap"] != float64(5) {
		t.Errorf("GET after PATCH = %v, want cap 5", got)
	}
}

func TestPatchWithoutAFieldChangesNothing(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{"concurrencyCap":3}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", code, body)
	}
	code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{}`)
	if code != http.StatusOK || !strings.Contains(body, `"concurrencyCap":3`) {
		t.Errorf("PATCH {} = %d %s, want 200 with the cap unchanged", code, body)
	}
}

func TestPatchRefusesAnInvalidCapAndKeepsTheStoredOne(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{"concurrencyCap":4}`); code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", code, body)
	}

	for _, bad := range []string{`0`, `-1`, `1.5`, `"3"`, `null`, `true`, `99999999999999999999`} {
		code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{"concurrencyCap":`+bad+`}`)
		if code != http.StatusUnprocessableEntity || errorCodeOf(body) != "VALIDATION_ERROR" {
			t.Errorf("PATCH concurrencyCap %s = %d %s, want 422 VALIDATION_ERROR", bad, code, body)
		}
	}
	if code, body := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `{"pollIntervalMinutes":10}`); code != http.StatusBadRequest || errorCodeOf(body) != "VALIDATION_ERROR" {
		t.Errorf("PATCH with the removed v1 field = %d %s, want 400 VALIDATION_ERROR", code, body)
	}
	if code, _ := s.send(t, admin, http.MethodPatch, "/api/v1/settings", `not json`); code != http.StatusBadRequest {
		t.Errorf("PATCH with a bad body = %d, want 400", code)
	}
	if n := storedCap(t, s); n != 4 {
		t.Errorf("stored cap = %d, want the earlier 4 kept", n)
	}
}

func TestAViewerCannotReadOrChangeTheSettingsOrSeeTheApp(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/settings", ""},
		{http.MethodPatch, "/api/v1/settings", `{"concurrencyCap":9}`},
		{http.MethodGet, "/api/v1/settings/global-config", ""},
		{http.MethodGet, "/api/v1/github-app", ""},
		{http.MethodGet, "/api/v1/installations", ""},
	} {
		code, body := s.send(t, viewer, route.method, route.path, route.body)
		if code != http.StatusForbidden || errorCodeOf(body) != "FORBIDDEN" {
			t.Errorf("viewer %s %s = %d %s, want 403 FORBIDDEN", route.method, route.path, code, body)
		}
	}
	if n := rowCount(t, s.deps, `SELECT COUNT(*) FROM instance_settings WHERE concurrency_cap = 9`); n != 0 {
		t.Error("a viewer's PATCH changed the concurrency cap")
	}
}

func TestSettingsRoutesNeedASession(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesOwnerID, Login: "acme-owner"})
	anonymous := noRedirects(t)
	for _, path := range []string{"/api/v1/settings", "/api/v1/settings/global-config", "/api/v1/github-app", "/api/v1/about"} {
		if code, body := s.send(t, anonymous, http.MethodGet, path, ""); code != http.StatusUnauthorized {
			t.Errorf("anonymous GET %s = %d %s, want 401", path, code, body)
		}
	}
}

// tokenShaped builds a GitHub-token-shaped string at run time, so no secret-shaped literal sits
// in the source.
func tokenShaped() string {
	return "ghp_" + strings.Repeat("a1B2", 9)
}

func captureGlobalConfig(t *testing.T, s *rolesServer, source, content string) {
	t.Helper()
	if _, err := s.deps.DB.Exec(`INSERT INTO installation_settings (installation_id, global_config_content, global_config_source, global_config_captured_at, updated_at)
		VALUES ('inst-1', ?, ?, '2026-11-14T12:00:00.000Z', '2026-11-14T12:00:00.000Z')`, content, source); err != nil {
		t.Fatalf("capture: %v", err)
	}
}

func TestGlobalConfigIsNullUntilAJobHasReturnedOne(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	s.deps.Store.SetInstanceConfig(store.InstanceConfig{GlobalConfigRepo: "acme/renovate", GlobalConfigRef: "main"})

	got := getMap(t, s, admin, "/api/v1/settings/global-config")
	if got["content"] != nil || got["capturedAt"] != nil {
		t.Errorf("global config before a capture = %v, want null content and capturedAt", got)
	}
	if got["source"] != "repository" || got["repository"] != "acme/renovate" || got["ref"] != "main" {
		t.Errorf("global config origin = %v, want the configured repository and ref", got)
	}
}

func TestGlobalConfigIsReturnedRawWithOnlyTokenShapedStringsRedacted(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	s.deps.Store.SetInstanceConfig(store.InstanceConfig{GlobalConfigRepo: "acme/renovate", GlobalConfigRef: "main"})
	token := tokenShaped()
	before := "module.exports = {\r\n  allowedCommands: ['^/opt/tools/run\\\\.sh$'],\r\n  token: '"
	after := "',\r\n\t// trailing comment, no final newline\r\n};"
	captureGlobalConfig(t, s, "repository", before+token+after)

	got := getMap(t, s, admin, "/api/v1/settings/global-config")
	want := before + "[REDACTED]" + after
	if got["content"] != want {
		t.Errorf("content = %q, want %q: the token replaced and every other byte unchanged", got["content"], want)
	}
	if strings.Contains(got["content"].(string), token) {
		t.Error("the token-shaped string reached the response")
	}
	if got["capturedAt"] != "2026-11-14T12:00:00.000Z" || got["source"] != "repository" {
		t.Errorf("global config = %v", got)
	}
	if rowCount(t, s.deps, `SELECT COUNT(*) FROM installation_settings WHERE global_config_content LIKE '%`+token+`%'`) != 1 {
		t.Error("the stored copy was changed; only the response is redacted")
	}
}

func TestAMountedGlobalConfigNamesNoRepository(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	s.deps.Store.SetInstanceConfig(store.InstanceConfig{GlobalConfigRepo: "acme/renovate", GlobalConfigRef: "main"})
	captureGlobalConfig(t, s, "mounted", "{}")

	got := getMap(t, s, admin, "/api/v1/settings/global-config")
	if got["source"] != "mounted" || got["repository"] != nil || got["ref"] != nil || got["content"] != "{}" {
		t.Errorf("global config = %v, want the mounted source with no repository or ref", got)
	}
}

func TestGitHubAppReportsMetadataAndPresenceFlagsAndNoSecret(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)

	got := getMap(t, s, admin, "/api/v1/github-app")
	app, _ := got["app"].(map[string]any)
	keys := make([]string, 0, len(app))
	for k := range app {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	wantKeys := []string{"appId", "clientId", "hasClientSecret", "hasPrivateKey", "hasWebhookSecret", "htmlUrl", "ownerLogin", "ownerType", "slug", "webhookActive"}
	if !slices.Equal(keys, wantKeys) {
		t.Errorf("app keys = %v, want exactly %v", keys, wantKeys)
	}
	if app["slug"] != "release-ops" || app["ownerLogin"] != "octocat" || app["hasPrivateKey"] != true || app["hasClientSecret"] != true || app["hasWebhookSecret"] != true || app["webhookActive"] != false {
		t.Errorf("app = %v", app)
	}
	inst, _ := got["installation"].(map[string]any)
	if inst["accountLogin"] != "acme" || inst["status"] != "active" {
		t.Errorf("installation = %v, want the live installation", got["installation"])
	}
}

func TestGitHubAppFlagsAMissingSecretAsAbsent(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	if _, err := s.deps.DB.Exec(`UPDATE github_app SET encrypted_client_secret = ''`); err != nil {
		t.Fatalf("blank secret: %v", err)
	}
	app, _ := getMap(t, s, admin, "/api/v1/github-app")["app"].(map[string]any)
	if app["hasClientSecret"] != false || app["hasPrivateKey"] != true {
		t.Errorf("app = %v, want only the client secret flagged absent", app)
	}
}

func TestInstallationListsTheMissingPermissions(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	required := map[string]string{
		"administration": "read", "checks": "write", "statuses": "write", "contents": "write", "vulnerability_alerts": "read",
		"issues": "write", "metadata": "read", "pull_requests": "write", "workflows": "write", "merge_queues": "write", "members": "read",
	}

	missingOf := func(permissions string) []string {
		t.Helper()
		if _, err := s.deps.DB.Exec(`UPDATE installations SET permissions = ?`, permissions); err != nil {
			t.Fatalf("set permissions: %v", err)
		}
		var out struct {
			Installation struct {
				Missing []string `json:"missingPermissions"`
			} `json:"installation"`
		}
		_, body := s.send(t, admin, http.MethodGet, "/api/v1/github-app", "")
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatalf("decode: %v in %s", err, body)
		}
		var list struct {
			Items []struct {
				Missing []string `json:"missingPermissions"`
			} `json:"items"`
		}
		_, body = s.send(t, admin, http.MethodGet, "/api/v1/installations", "")
		if err := json.Unmarshal([]byte(body), &list); err != nil || len(list.Items) != 1 {
			t.Fatalf("installations: %v in %s", err, body)
		}
		if !slices.Equal(out.Installation.Missing, list.Items[0].Missing) {
			t.Errorf("the App panel says %v missing and the installations list says %v", out.Installation.Missing, list.Items[0].Missing)
		}
		return out.Installation.Missing
	}

	full := map[string]string{}
	for k, v := range required {
		full[k] = v
	}
	toJSON := func(m map[string]string) string {
		b, _ := json.Marshal(m)
		return string(b)
	}

	if got := missingOf(toJSON(full)); len(got) != 0 {
		t.Errorf("missing with every permission granted = %v, want none", got)
	}
	full["contents"] = "read"
	delete(full, "members")
	full["metadata"] = "write"
	if got := missingOf(toJSON(full)); !slices.Equal(got, []string{"contents", "members"}) {
		t.Errorf("missing = %v, want [contents members]: a lower level and an absent permission count, a higher level does not", got)
	}
	if got := missingOf(`{}`); len(got) != len(required) || !slices.IsSorted(got) {
		t.Errorf("missing with nothing granted = %v, want all %d, sorted", got, len(required))
	}
}

func TestRoleMappingShowsTheTeamsAndReportsTheirProblems(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	mapping := func() map[string]any {
		m, _ := getMap(t, s, admin, "/api/v1/github-app")["roleMapping"].(map[string]any)
		return m
	}
	problemsOf := func() []string {
		var out []string
		for _, p := range mapping()["problems"].([]any) {
			p := p.(map[string]any)
			out = append(out, p["variable"].(string)+":"+p["code"].(string))
		}
		return out
	}

	if m := mapping(); m["adminTeam"] != nil || m["viewerTeam"] != nil || len(m["problems"].([]any)) != 0 {
		t.Errorf("role mapping with no teams = %v, want nulls and no problems", m)
	}

	s.deps.Store.SetInstanceConfig(store.InstanceConfig{AdminTeam: "Other-Org/admins", ViewerTeam: "ACME/readers"})
	if m := mapping(); m["adminTeam"] != "Other-Org/admins" || m["viewerTeam"] != "ACME/readers" ||
		!slices.Equal(problemsOf(), []string{"ADMIN_TEAM:team_of_another_organization"}) {
		t.Errorf("role mapping = %v, want only ADMIN_TEAM reported (a team of the installation's organization, in any case, is fine)", m)
	}

	if _, err := s.deps.DB.Exec(`UPDATE installations SET account_type = 'User'`); err != nil {
		t.Fatalf("personal account: %v", err)
	}
	if got := problemsOf(); !slices.Equal(got, []string{"ADMIN_TEAM:ignored_on_personal_account", "VIEWER_TEAM:ignored_on_personal_account"}) {
		t.Errorf("problems on a personal account = %v, want both teams reported as ignored", got)
	}
}

func TestGitHubAppAnswersWithNoInstallationBeforeOneIsRecorded(t *testing.T) {
	t.Parallel()
	s, admin := newSettingsAdmin(t)
	if _, err := s.deps.DB.Exec(`UPDATE installations SET status = 'removed'`); err != nil {
		t.Fatalf("remove installation: %v", err)
	}
	got := getMap(t, s, admin, "/api/v1/github-app")
	if got["installation"] != nil || got["app"] == nil {
		t.Errorf("github-app = %v, want the App and a null installation", got)
	}
}

func TestAboutIsOpenToAViewerAndNamesThePinnedRenovate(t *testing.T) {
	t.Parallel()
	s := newRolesServer(t, github.User{ID: rolesMemberID, Login: "acme-member"}, 1)
	viewer := s.signIn(t)

	got := getMap(t, s, viewer, "/api/v1/about")
	renovate, _ := got["renovate"].(map[string]any)
	if got["version"] != buildinfo.Current() ||
		renovate["version"] != buildinfo.RenovateVersion || renovate["license"] != "AGPL-3.0-only" ||
		renovate["sourceUrl"] != "https://github.com/renovatebot/renovate/tree/"+buildinfo.RenovateVersion {
		t.Errorf("about = %v", got)
	}
}
