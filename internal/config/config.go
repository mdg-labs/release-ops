package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultAppDBPath      = "/data/app.db"
	defaultGoInternalPort = 8080
	defaultPort           = 3000
	defaultGoAPIURL       = "http://127.0.0.1:8080"
	minSessionSecretLen   = 32
	encryptionKeyHexLen   = 64
)

// Config holds environment-backed server configuration.
type Config struct {
	SessionSecret          string
	AppEncryptionKey       string
	AppDBPath              string
	GoInternalPort         int
	Port                   int
	GoAPIURL               string
	AppPublicURL           string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
}

// Load reads and validates configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		SessionSecret:          strings.TrimSpace(os.Getenv("SESSION_SECRET")),
		AppEncryptionKey:       strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY")),
		AppDBPath:              envOrDefault("APP_DB_PATH", defaultAppDBPath),
		GoAPIURL:               envOrDefault("GO_API_URL", defaultGoAPIURL),
		AppPublicURL:           strings.TrimSpace(os.Getenv("APP_PUBLIC_URL")),
		BootstrapAdminEmail:    strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		BootstrapAdminPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
	}

	var err error
	if cfg.GoInternalPort, err = envIntOrDefault("GO_INTERNAL_PORT", defaultGoInternalPort); err != nil {
		return nil, fmt.Errorf("GO_INTERNAL_PORT: %w", err)
	}
	if cfg.Port, err = envIntOrDefault("PORT", defaultPort); err != nil {
		return nil, fmt.Errorf("PORT: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// GoListenAddr returns the loopback address for the Go HTTP server.
func (c *Config) GoListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.GoInternalPort)
}

// SecureCookies reports whether session cookies should use the Secure flag.
func (c *Config) SecureCookies() bool {
	if c.AppPublicURL == "" {
		return false
	}
	u, err := url.Parse(c.AppPublicURL)
	if err != nil {
		return false
	}
	return u.Scheme == "https"
}

// BootstrapAdminConfigured reports whether one-time admin bootstrap env is set.
func (c *Config) BootstrapAdminConfigured() bool {
	return c.BootstrapAdminEmail != "" || c.BootstrapAdminPassword != ""
}

func (c *Config) validate() error {
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}
	if len(c.SessionSecret) < minSessionSecretLen {
		return fmt.Errorf("SESSION_SECRET must be at least %d bytes", minSessionSecretLen)
	}
	if c.AppEncryptionKey == "" {
		return fmt.Errorf("APP_ENCRYPTION_KEY is required")
	}
	if len(c.AppEncryptionKey) != encryptionKeyHexLen || !isHex(c.AppEncryptionKey) {
		return fmt.Errorf("APP_ENCRYPTION_KEY must be %d hex characters", encryptionKeyHexLen)
	}
	if c.BootstrapAdminEmail != "" && c.BootstrapAdminPassword == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD is required when BOOTSTRAP_ADMIN_EMAIL is set")
	}
	if c.BootstrapAdminPassword != "" && c.BootstrapAdminEmail == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_EMAIL is required when BOOTSTRAP_ADMIN_PASSWORD is set")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be positive, got %d", n)
	}
	return n, nil
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
