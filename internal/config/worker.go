package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mdg-labs/release-ops/internal/logging"
)

// WorkerConfig is the configuration of `release-ops worker`. It holds the app's internal URL
// and the worker secret and nothing else: the worker reads no app secret and no database
// path (specs.html § 9).
type WorkerConfig struct {
	// InternalURL is where the app's internal listener is reachable (INTERNAL_URL).
	InternalURL  string
	WorkerSecret string
	// MetricsAddr is where the worker's /metrics listener binds (METRICS_ADDR); empty means
	// no listener.
	MetricsAddr string
	// GitHubBaseURL is the GitHub web URL without a trailing slash (GITHUB_BASE_URL); Renovate's
	// API endpoint and the global config fetch derive from it.
	GitHubBaseURL string
}

// LoadWorker reads and validates the worker's configuration from the process environment.
func LoadWorker() (*WorkerConfig, error) {
	cfg := &WorkerConfig{
		InternalURL:  strings.TrimSpace(os.Getenv("INTERNAL_URL")),
		WorkerSecret: strings.TrimSpace(os.Getenv("WORKER_SECRET")),
		MetricsAddr:  envOrDefault("METRICS_ADDR", defaultMetricsAddr),

		GitHubBaseURL: strings.TrimRight(envOrDefault("GITHUB_BASE_URL", defaultGitHubURL), "/"),
	}
	logging.Register(cfg.WorkerSecret)
	if err := validateURL("GITHUB_BASE_URL", cfg.GitHubBaseURL, true); err != nil {
		return nil, err
	}
	if err := validateListenAddr(cfg.MetricsAddr); err != nil {
		return nil, fmt.Errorf("METRICS_ADDR: %w", err)
	}
	if cfg.InternalURL == "" {
		return nil, fmt.Errorf("INTERNAL_URL is required")
	}
	u, err := url.Parse(cfg.InternalURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("INTERNAL_URL must be an http or https URL with a host")
	}
	if cfg.WorkerSecret == "" {
		return nil, fmt.Errorf("WORKER_SECRET is required")
	}
	if err := validateWorkerSecret(cfg.WorkerSecret); err != nil {
		return nil, err
	}
	return cfg, nil
}
