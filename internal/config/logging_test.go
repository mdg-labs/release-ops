package config

import (
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/logging"
)

func TestLoadKeepsItsSecretsOutOfTheLogs(t *testing.T) {
	validEnv(t)
	const (
		session = "opaque-session-secret-for-config-test-0001"
		worker  = "opaque-worker-secret-for-config-test-0001"
		setup   = "opaque-setup-token-for-config-test-0001-opaque-setup-token-0002-xxxxxxxxxx"
	)
	t.Setenv("SESSION_SECRET", session)
	t.Setenv("APP_ENCRYPTION_KEY", strings.Repeat("ab", 16)+strings.Repeat("cd", 16))
	t.Setenv("INTERNAL_ADDR", "127.0.0.1:9090")
	t.Setenv("WORKER_SECRET", worker)
	t.Setenv("SETUP_TOKEN", setup)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for name, secret := range map[string]string{
		"SESSION_SECRET": session, "APP_ENCRYPTION_KEY": cfg.AppEncryptionKey,
		"WORKER_SECRET": worker, "SETUP_TOKEN": setup,
	} {
		if got := logging.Redact("value " + secret); strings.Contains(got, secret) {
			t.Errorf("%s is not removed from log text: %q", name, got)
		}
	}
}

// A value that fails validation is registered all the same, so an error that quotes it is
// already covered.
func TestLoadRegistersASecretThatFailsValidation(t *testing.T) {
	validEnv(t)
	const short = "opaque-too-short-session"
	t.Setenv("SESSION_SECRET", short)
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a short session secret")
	}
	if got := logging.Redact("rejected " + short); strings.Contains(got, short) {
		t.Fatalf("the rejected secret is not removed: %q", got)
	}
}

func TestLoadWorkerKeepsItsSecretOutOfTheLogs(t *testing.T) {
	const worker = "opaque-worker-only-secret-for-test-0002"
	t.Setenv("INTERNAL_URL", "http://release-ops:9090")
	t.Setenv("WORKER_SECRET", worker)
	t.Setenv("METRICS_ADDR", "")
	t.Setenv("GITHUB_BASE_URL", "")
	if _, err := LoadWorker(); err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if got := logging.Redact("value " + worker); strings.Contains(got, worker) {
		t.Fatalf("WORKER_SECRET is not removed: %q", got)
	}
}
