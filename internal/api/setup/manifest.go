package setup

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// DefaultAppName is the App name used when the operator enters none.
	DefaultAppName = "Release Ops"

	maxAppNameRunes = 34
	maxLoginLen     = 39
)

// Browser-facing paths GitHub redirects back to, served by the Go routes through the /api/go
// proxy.
const (
	callbackPath      = "/api/go/api/v1/setup/callback"
	installedPath     = "/api/go/api/v1/setup/installed"
	githubCallbackAPI = "/api/go/api/v1/auth/github/callback"
)

// Manifest is the GitHub App manifest the setup page submits to GitHub, as documented for
// registering a GitHub App from a manifest.
type Manifest struct {
	Name               string            `json:"name"`
	URL                string            `json:"url"`
	HookAttributes     HookAttributes    `json:"hook_attributes"`
	RedirectURL        string            `json:"redirect_url"`
	CallbackURLs       []string          `json:"callback_urls"`
	SetupURL           string            `json:"setup_url"`
	Public             bool              `json:"public"`
	DefaultPermissions map[string]string `json:"default_permissions"`
	DefaultEvents      []string          `json:"default_events"`
}

// HookAttributes is the manifest's webhook: GitHub requires a url even for an inactive one.
type HookAttributes struct {
	URL    string `json:"url"`
	Active bool   `json:"active"`
}

// manifestPermissions is the permission list of specs §3.3.
var manifestPermissions = map[string]string{
	"administration":       "read",
	"checks":               "write",
	"statuses":             "write",
	"contents":             "write",
	"vulnerability_alerts": "read",
	"issues":               "write",
	"metadata":             "read",
	"pull_requests":        "write",
	"workflows":            "write",
	"merge_queues":         "write",
	"members":              "read",
}

var manifestEvents = []string{"push", "pull_request", "issues", "installation", "installation_repositories"}

// BuildManifest builds the manifest from APP_URL and WEBHOOK_URL. The webhook is active only
// when webhookURL is set. Every URL comes from configuration, never from a request.
func BuildManifest(name, appURL, webhookURL string) Manifest {
	hook := HookAttributes{URL: appURL, Active: false}
	if webhookURL != "" {
		hook = HookAttributes{URL: webhookURL, Active: true}
	}
	permissions := make(map[string]string, len(manifestPermissions))
	for k, v := range manifestPermissions {
		permissions[k] = v
	}
	return Manifest{
		Name:               name,
		URL:                appURL,
		HookAttributes:     hook,
		RedirectURL:        appURL + callbackPath,
		CallbackURLs:       []string{appURL + githubCallbackAPI},
		SetupURL:           appURL + installedPath,
		Public:             false,
		DefaultPermissions: permissions,
		DefaultEvents:      append([]string(nil), manifestEvents...),
	}
}

// RegistrationURL is where the manifest form is posted: the personal or organization App
// registration page, carrying the state GitHub hands back with the code. login must already
// pass ValidLogin for an organization.
func RegistrationURL(githubBaseURL, ownerType, login, state string) string {
	path := "/settings/apps/new"
	if ownerType == ownerOrganization {
		path = "/organizations/" + url.PathEscape(login) + "/settings/apps/new"
	}
	return githubBaseURL + path + "?state=" + url.QueryEscape(state)
}

// InstallURL is the App's installation page.
func InstallURL(githubBaseURL, slug string) string {
	return githubBaseURL + "/apps/" + url.PathEscape(slug) + "/installations/new"
}

// ValidLogin reports whether login follows GitHub's account name syntax: up to 39 letters,
// digits and single hyphens, neither starting nor ending with a hyphen.
func ValidLogin(login string) bool {
	if login == "" || len(login) > maxLoginLen {
		return false
	}
	if strings.HasPrefix(login, "-") || strings.HasSuffix(login, "-") || strings.Contains(login, "--") {
		return false
	}
	for _, r := range login {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// NormalizeAppName trims name and applies the default; ok is false for a name GitHub would
// refuse (over 34 characters) or one with control characters.
func NormalizeAppName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultAppName, true
	}
	if utf8.RuneCountInString(name) > maxAppNameRunes {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}
