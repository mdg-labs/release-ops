package config

import (
	"strings"
	"testing"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SESSION_SECRET", strings.Repeat("a", minSessionSecretLen))
	t.Setenv("APP_ENCRYPTION_KEY", strings.Repeat("0", encryptionKeyHexLen))
	t.Setenv("APP_DB_PATH", "")
	t.Setenv("GO_INTERNAL_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("GO_API_URL", "")
	t.Setenv("APP_URL", "")
	t.Setenv("WEBHOOK_URL", "")
	t.Setenv("GITHUB_BASE_URL", "")
	t.Setenv("SETUP_TOKEN", "")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	t.Setenv("INTERNAL_ADDR", "")
	t.Setenv("WORKER_SECRET", "")
	t.Setenv("PRESET_REPOSITORIES", "")
	t.Setenv("GLOBAL_CONFIG_REPO", "")
}

func TestLoadDefaults(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.AppDBPath != defaultAppDBPath {
		t.Errorf("AppDBPath = %q, want %q", cfg.AppDBPath, defaultAppDBPath)
	}
	if cfg.GoInternalPort != defaultGoInternalPort {
		t.Errorf("GoInternalPort = %d, want %d", cfg.GoInternalPort, defaultGoInternalPort)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.GoAPIURL != defaultGoAPIURL {
		t.Errorf("GoAPIURL = %q, want %q", cfg.GoAPIURL, defaultGoAPIURL)
	}
	if cfg.GoListenAddr() != "127.0.0.1:8080" {
		t.Errorf("GoListenAddr() = %q, want 127.0.0.1:8080", cfg.GoListenAddr())
	}
}

func TestLoadMissingSessionSecret(t *testing.T) {
	validEnv(t)
	t.Setenv("SESSION_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing SESSION_SECRET")
	}
	if !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Errorf("error = %v, want SESSION_SECRET mention", err)
	}
}

func TestLoadMissingAppEncryptionKey(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENCRYPTION_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing APP_ENCRYPTION_KEY")
	}
	if !strings.Contains(err.Error(), "APP_ENCRYPTION_KEY") {
		t.Errorf("error = %v, want APP_ENCRYPTION_KEY mention", err)
	}
}

func TestLoadServerRequiresAppURL(t *testing.T) {
	validEnv(t)

	_, err := LoadServer()
	if err == nil || !strings.Contains(err.Error(), "APP_URL") {
		t.Fatalf("LoadServer() without APP_URL error = %v, want APP_URL mention", err)
	}

	t.Setenv("APP_URL", "https://release-ops.example.com/")
	cfg, err := LoadServer()
	if err != nil {
		t.Fatalf("LoadServer() with APP_URL error = %v", err)
	}
	if cfg.AppURL != "https://release-ops.example.com" {
		t.Errorf("AppURL = %q, want the value without its trailing slash", cfg.AppURL)
	}
	if !cfg.SecureCookies() {
		t.Error("SecureCookies() = false for https APP_URL")
	}

	t.Setenv("APP_URL", "http://localhost:3000")
	cfg, err = LoadServer()
	if err != nil {
		t.Fatalf("LoadServer() with http APP_URL error = %v", err)
	}
	if cfg.SecureCookies() {
		t.Error("SecureCookies() = true for http APP_URL")
	}
}

func TestLoadRejectsMalformedURLs(t *testing.T) {
	for _, name := range []string{"APP_URL", "WEBHOOK_URL", "GITHUB_BASE_URL"} {
		for _, value := range []string{"release-ops.example.com", "ftp://example.com", "https://"} {
			validEnv(t)
			t.Setenv(name, value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("Load() with %s=%q error = %v, want %s mention", name, value, err, name)
			}
		}
	}
}

func TestLoadWebhookAndGitHubURLs(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.WebhookURL != "" {
		t.Errorf("WebhookURL = %q, want empty when unset", cfg.WebhookURL)
	}
	if cfg.GitHubBaseURL != "https://github.com" {
		t.Errorf("GitHubBaseURL = %q, want the github.com default", cfg.GitHubBaseURL)
	}

	t.Setenv("WEBHOOK_URL", "https://hooks.example.com/hooks/github")
	t.Setenv("GITHUB_BASE_URL", "https://github.example.com/")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with URLs error = %v", err)
	}
	if cfg.WebhookURL != "https://hooks.example.com/hooks/github" {
		t.Errorf("WebhookURL = %q", cfg.WebhookURL)
	}
	if cfg.GitHubBaseURL != "https://github.example.com" {
		t.Errorf("GitHubBaseURL = %q, want the value without its trailing slash", cfg.GitHubBaseURL)
	}
}

func TestLoadSetupTokenMinimumStrength(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SetupToken != "" {
		t.Errorf("SetupToken = %q, want empty when unset", cfg.SetupToken)
	}

	t.Setenv("SETUP_TOKEN", strings.Repeat("a", minSetupTokenLen-1))
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SETUP_TOKEN") {
		t.Fatalf("Load() with a short SETUP_TOKEN error = %v, want SETUP_TOKEN mention", err)
	}

	t.Setenv("SETUP_TOKEN", strings.Repeat("a", minSetupTokenLen))
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with a long enough SETUP_TOKEN error = %v", err)
	}
	if cfg.SetupToken != strings.Repeat("a", minSetupTokenLen) {
		t.Error("SetupToken not read from the environment")
	}
}

func TestLoadBootstrapAdminOptionalPair(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BootstrapAdminConfigured() {
		t.Error("BootstrapAdminConfigured() = true with empty bootstrap env")
	}

	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "secret")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with bootstrap pair error = %v", err)
	}
	if !cfg.BootstrapAdminConfigured() {
		t.Error("BootstrapAdminConfigured() = false with both bootstrap vars set")
	}

	validEnv(t)
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error when only BOOTSTRAP_ADMIN_EMAIL is set")
	}

	validEnv(t)
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "secret")
	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error when only BOOTSTRAP_ADMIN_PASSWORD is set")
	}
}

func TestLoadCustomValues(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_DB_PATH", "/tmp/custom.db")
	t.Setenv("GO_INTERNAL_PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppDBPath != "/tmp/custom.db" {
		t.Errorf("AppDBPath = %q", cfg.AppDBPath)
	}
	if cfg.GoInternalPort != 9090 {
		t.Errorf("GoInternalPort = %d, want 9090", cfg.GoInternalPort)
	}
	if cfg.GoListenAddr() != "127.0.0.1:9090" {
		t.Errorf("GoListenAddr() = %q", cfg.GoListenAddr())
	}
}

func TestLoadRejectsEnvExamplePlaceholders(t *testing.T) {
	validEnv(t)
	t.Setenv("APP_ENCRYPTION_KEY", exampleAppEncryptionKey)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "APP_ENCRYPTION_KEY") {
		t.Fatalf("Load() error = %v, want placeholder APP_ENCRYPTION_KEY rejected", err)
	}

	validEnv(t)
	t.Setenv("SESSION_SECRET", exampleSessionSecret)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("Load() error = %v, want placeholder SESSION_SECRET rejected", err)
	}
}

func TestLoadPresetRepositories(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.PresetRepositories) != 0 || len(cfg.JobTokenRepositories()) != 0 {
		t.Errorf("unset presets = %v, want none", cfg.PresetRepositories)
	}

	t.Setenv("PRESET_REPOSITORIES", " acme/renovate-presets, ,Acme/RENOVATE-PRESETS,acme/other.repo_1 ")
	t.Setenv("GLOBAL_CONFIG_REPO", "acme/renovate-global")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := strings.Join(cfg.PresetRepositories, ","); got != "acme/renovate-presets,acme/other.repo_1" {
		t.Errorf("PresetRepositories = %q, want the trimmed list without blanks and repeats", got)
	}
	if got := strings.Join(cfg.JobTokenRepositories(), ","); got != "acme/renovate-presets,acme/other.repo_1,acme/renovate-global" {
		t.Errorf("JobTokenRepositories() = %q, want the presets and then the global config repository", got)
	}

	t.Setenv("GLOBAL_CONFIG_REPO", "ACME/renovate-presets")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := strings.Join(cfg.JobTokenRepositories(), ","); got != "acme/renovate-presets,acme/other.repo_1" {
		t.Errorf("JobTokenRepositories() = %q, want the global config repository listed once", got)
	}
}

func TestLoadRejectsMalformedRepositoryLists(t *testing.T) {
	for _, bad := range []string{"acme", "acme/", "/repo", "acme/repo/extra", "acme/re po", "https://github.com/acme/repo", "-acme/repo", "acme/.."} {
		validEnv(t)
		t.Setenv("PRESET_REPOSITORIES", "acme/ok,"+bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "PRESET_REPOSITORIES") {
			t.Errorf("PRESET_REPOSITORIES with %q: error = %v, want a PRESET_REPOSITORIES error", bad, err)
		}

		validEnv(t)
		t.Setenv("GLOBAL_CONFIG_REPO", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GLOBAL_CONFIG_REPO") {
			t.Errorf("GLOBAL_CONFIG_REPO %q: error = %v, want a GLOBAL_CONFIG_REPO error", bad, err)
		}
	}
}
