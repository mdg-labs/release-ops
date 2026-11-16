package config

import (
	"strings"
	"testing"
)

func TestLoadInternalListenerNeedsAddrAndSecretTogether(t *testing.T) {
	secret := strings.Repeat("w", minWorkerSecretLen)

	for _, tc := range []struct {
		name, addr, secret, wantErr string
	}{
		{"neither is no internal listener", "", "", ""},
		{"both", ":9090", secret, ""},
		{"address without secret", ":9090", "", "WORKER_SECRET is required"},
		{"secret without address", "", secret, "INTERNAL_ADDR is required"},
		{"short secret", ":9090", "short", "at least 32 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			validEnv(t)
			t.Setenv("INTERNAL_ADDR", tc.addr)
			t.Setenv("WORKER_SECRET", tc.secret)

			cfg, err := Load()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Load() = %v", err)
				}
				if cfg.InternalAddr != tc.addr || cfg.WorkerSecret != tc.secret {
					t.Errorf("InternalAddr %q WorkerSecret set %v", cfg.InternalAddr, cfg.WorkerSecret != "")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() = %v, want %q", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), tc.secret) && tc.secret != "" {
				t.Errorf("error echoes the secret: %v", err)
			}
		})
	}
}

// The worker holds no app secret: it starts with none of the app's variables set.
func TestLoadWorkerNeedsOnlyTheInternalURLAndTheSecret(t *testing.T) {
	for _, name := range []string{"SESSION_SECRET", "APP_ENCRYPTION_KEY", "APP_DB_PATH", "INTERNAL_ADDR", "METRICS_ADDR", "GITHUB_BASE_URL"} {
		t.Setenv(name, "")
	}
	t.Setenv("INTERNAL_URL", "http://release-ops:9090")
	t.Setenv("WORKER_SECRET", strings.Repeat("w", minWorkerSecretLen))

	cfg, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker() = %v", err)
	}
	if cfg.InternalURL != "http://release-ops:9090" || cfg.WorkerSecret == "" {
		t.Fatalf("LoadWorker() = %+v", cfg)
	}
}

func TestLoadWorkerRefusesBadValues(t *testing.T) {
	good := strings.Repeat("w", minWorkerSecretLen)
	for _, tc := range []struct{ name, url, secret, wantErr string }{
		{"no url", "", good, "INTERNAL_URL is required"},
		{"not a url", "release-ops:9090", good, "INTERNAL_URL must be"},
		{"wrong scheme", "ftp://release-ops:9090", good, "INTERNAL_URL must be"},
		{"no secret", "http://release-ops:9090", "", "WORKER_SECRET is required"},
		{"short secret", "http://release-ops:9090", "short", "at least 32 bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("INTERNAL_URL", tc.url)
			t.Setenv("WORKER_SECRET", tc.secret)
			if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadWorker() = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadWorkerMetricsAddr(t *testing.T) {
	t.Setenv("INTERNAL_URL", "http://release-ops:9090")
	t.Setenv("WORKER_SECRET", strings.Repeat("w", minWorkerSecretLen))

	t.Setenv("METRICS_ADDR", "")
	if cfg, err := LoadWorker(); err != nil || cfg.MetricsAddr != "127.0.0.1:9100" {
		t.Fatalf("LoadWorker() = %+v, %v; want the loopback default", cfg, err)
	}
	t.Setenv("METRICS_ADDR", ":9100")
	if cfg, err := LoadWorker(); err != nil || cfg.MetricsAddr != ":9100" {
		t.Fatalf("LoadWorker() = %+v, %v; want :9100", cfg, err)
	}
	t.Setenv("METRICS_ADDR", "9100")
	if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
		t.Fatalf("LoadWorker() = %v, want METRICS_ADDR refused", err)
	}
}

func TestLoadWorkerGitHubBaseURL(t *testing.T) {
	t.Setenv("INTERNAL_URL", "http://release-ops:9090")
	t.Setenv("WORKER_SECRET", strings.Repeat("w", minWorkerSecretLen))
	t.Setenv("METRICS_ADDR", "")

	t.Setenv("GITHUB_BASE_URL", "")
	if cfg, err := LoadWorker(); err != nil || cfg.GitHubBaseURL != "https://github.com" {
		t.Fatalf("LoadWorker() = %+v, %v; want the github.com default", cfg, err)
	}
	t.Setenv("GITHUB_BASE_URL", "https://ghe.example.com/")
	if cfg, err := LoadWorker(); err != nil || cfg.GitHubBaseURL != "https://ghe.example.com" {
		t.Fatalf("LoadWorker() = %+v, %v; want the trimmed enterprise URL", cfg, err)
	}
	t.Setenv("GITHUB_BASE_URL", "ghe.example.com")
	if _, err := LoadWorker(); err == nil || !strings.Contains(err.Error(), "GITHUB_BASE_URL") {
		t.Fatalf("LoadWorker() = %v, want GITHUB_BASE_URL refused", err)
	}
}
