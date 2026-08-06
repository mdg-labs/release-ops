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
	t.Setenv("APP_PUBLIC_URL", "")
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
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

func TestLoadAppPublicURLOptional(t *testing.T) {
	validEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SecureCookies() {
		t.Error("SecureCookies() = true with empty APP_PUBLIC_URL")
	}

	t.Setenv("APP_PUBLIC_URL", "https://release-ops.example.com")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() with APP_PUBLIC_URL error = %v", err)
	}
	if cfg.AppPublicURL != "https://release-ops.example.com" {
		t.Errorf("AppPublicURL = %q", cfg.AppPublicURL)
	}
	if !cfg.SecureCookies() {
		t.Error("SecureCookies() = false for https APP_PUBLIC_URL")
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
