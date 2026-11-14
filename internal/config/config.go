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
	minWorkerSecretLen    = 32
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
	// InternalAddr is where the internal listener for the worker binds (INTERNAL_ADDR); empty
	// means no internal listener. WorkerSecret authenticates the worker to it and is set
	// together with it.
	InternalAddr string
	WorkerSecret string
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
		InternalAddr:           strings.TrimSpace(os.Getenv("INTERNAL_ADDR")),
		WorkerSecret:           strings.TrimSpace(os.Getenv("WORKER_SECRET")),
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

// Placeholder values shipped in .env.example. They are public, so starting with them
// would sign sessions and encrypt stored credentials with a key anyone can read.
const (
	exampleSessionSecret    = "change-me-use-openssl-rand-base64-32-at-least-32-chars"
	exampleAppEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func (c *Config) validate() error {
	if c.SessionSecret == "" {
		return fmt.Errorf("SESSION_SECRET is required")
	}
	if c.SessionSecret == exampleSessionSecret {
		return fmt.Errorf("SESSION_SECRET is the .env.example placeholder; generate one with: openssl rand -base64 32")
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
	if strings.EqualFold(c.AppEncryptionKey, exampleAppEncryptionKey) {
		return fmt.Errorf("APP_ENCRYPTION_KEY is the .env.example placeholder; generate one with: openssl rand -hex 32")
	}
	if c.BootstrapAdminEmail != "" && c.BootstrapAdminPassword == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD is required when BOOTSTRAP_ADMIN_EMAIL is set")
	}
	if c.BootstrapAdminPassword != "" && c.BootstrapAdminEmail == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_EMAIL is required when BOOTSTRAP_ADMIN_PASSWORD is set")
	}
	return c.validateInternalListener()
}

// validateInternalListener requires INTERNAL_ADDR and WORKER_SECRET together: a listener
// without a secret would serve the worker routes to anyone on the network, and a secret
// without a listener has nothing to protect.
func (c *Config) validateInternalListener() error {
	if c.InternalAddr == "" && c.WorkerSecret == "" {
		return nil
	}
	if c.InternalAddr == "" {
		return fmt.Errorf("INTERNAL_ADDR is required when WORKER_SECRET is set")
	}
	if c.WorkerSecret == "" {
		return fmt.Errorf("WORKER_SECRET is required when INTERNAL_ADDR is set")
	}
	return validateWorkerSecret(c.WorkerSecret)
}

func validateWorkerSecret(secret string) error {
	if len(secret) < minWorkerSecretLen {
		return fmt.Errorf("WORKER_SECRET must be at least %d bytes; generate one with: openssl rand -base64 32", minWorkerSecretLen)
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
