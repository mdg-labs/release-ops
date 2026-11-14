package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultAppDBPath      = "/data/app.db"
	defaultGoInternalPort = 8080
	defaultPort           = 3000
	// defaultMetricsAddr is loopback: an operator opens the metrics listener to a scraper by
	// setting METRICS_ADDR, never by default.
	defaultMetricsAddr  = "127.0.0.1:9100"
	defaultGoAPIURL     = "http://127.0.0.1:8080"
	minSessionSecretLen = 32
	encryptionKeyHexLen = 64
	minWorkerSecretLen  = 32
	// minSetupTokenLen is the length of the token the server generates (32 random bytes as
	// hex), so an operator-supplied SETUP_TOKEN is never easier to guess than a generated one.
	minSetupTokenLen = 64
	defaultGitHubURL = "https://github.com"
)

// Config holds environment-backed server configuration.
type Config struct {
	SessionSecret    string
	AppEncryptionKey string
	AppDBPath        string
	GoInternalPort   int
	Port             int
	GoAPIURL         string
	// AppURL is the browser-facing URL of the instance without a trailing slash (APP_URL).
	AppURL string
	// WebhookURL is the public URL GitHub delivers webhooks to (WEBHOOK_URL); empty when unset.
	WebhookURL string
	// GitHubBaseURL is the GitHub web URL without a trailing slash (GITHUB_BASE_URL).
	GitHubBaseURL string
	// SetupToken is the operator-supplied first-start setup token (SETUP_TOKEN); empty means
	// the server generates one.
	SetupToken             string
	BootstrapAdminEmail    string
	BootstrapAdminPassword string
	// InternalAddr is where the internal listener for the worker binds (INTERNAL_ADDR); empty
	// means no internal listener. WorkerSecret authenticates the worker to it and is set
	// together with it.
	InternalAddr string
	WorkerSecret string
	// MetricsAddr is where the /metrics listener binds (METRICS_ADDR); empty means no listener.
	MetricsAddr string
	// PresetRepositories are the owner/repo entries of PRESET_REPOSITORIES, e.g. a shared
	// presets repository, and GlobalConfigRepo is GLOBAL_CONFIG_REPO. Each job's installation
	// token covers the job's repository and these, so Renovate can resolve presets from them.
	PresetRepositories []string
	GlobalConfigRepo   string
}

// Load reads and validates configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		SessionSecret:          strings.TrimSpace(os.Getenv("SESSION_SECRET")),
		AppEncryptionKey:       strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY")),
		AppDBPath:              envOrDefault("APP_DB_PATH", defaultAppDBPath),
		GoAPIURL:               envOrDefault("GO_API_URL", defaultGoAPIURL),
		AppURL:                 strings.TrimRight(strings.TrimSpace(os.Getenv("APP_URL")), "/"),
		WebhookURL:             strings.TrimSpace(os.Getenv("WEBHOOK_URL")),
		GitHubBaseURL:          strings.TrimRight(envOrDefault("GITHUB_BASE_URL", defaultGitHubURL), "/"),
		SetupToken:             strings.TrimSpace(os.Getenv("SETUP_TOKEN")),
		BootstrapAdminEmail:    strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_EMAIL")),
		BootstrapAdminPassword: os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
		InternalAddr:           strings.TrimSpace(os.Getenv("INTERNAL_ADDR")),
		WorkerSecret:           strings.TrimSpace(os.Getenv("WORKER_SECRET")),
		GlobalConfigRepo:       strings.TrimSpace(os.Getenv("GLOBAL_CONFIG_REPO")),
		MetricsAddr:            envOrDefault("METRICS_ADDR", defaultMetricsAddr),
	}

	var err error
	if err = validateListenAddr(cfg.MetricsAddr); err != nil {
		return nil, fmt.Errorf("METRICS_ADDR: %w", err)
	}
	if cfg.PresetRepositories, err = parseRepositoryList(os.Getenv("PRESET_REPOSITORIES")); err != nil {
		return nil, fmt.Errorf("PRESET_REPOSITORIES: %w", err)
	}
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

// LoadServer is Load for the server process: it also refuses to start without APP_URL,
// which fixes the setup and sign-in redirect targets and the Secure cookie flag.
func LoadServer() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if cfg.AppURL == "" {
		return nil, fmt.Errorf("APP_URL is required: the browser-facing URL of the instance, e.g. https://release-ops.example.com")
	}
	return cfg, nil
}

// GoListenAddr returns the loopback address for the Go HTTP server.
func (c *Config) GoListenAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.GoInternalPort)
}

// SecureCookies reports whether session cookies should use the Secure flag.
func (c *Config) SecureCookies() bool {
	u, err := url.Parse(c.AppURL)
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
	if err := validateURL("APP_URL", c.AppURL, false); err != nil {
		return err
	}
	if err := validateURL("WEBHOOK_URL", c.WebhookURL, false); err != nil {
		return err
	}
	if err := validateURL("GITHUB_BASE_URL", c.GitHubBaseURL, true); err != nil {
		return err
	}
	if c.SetupToken != "" && len(c.SetupToken) < minSetupTokenLen {
		return fmt.Errorf("SETUP_TOKEN must be at least %d characters; generate one with: openssl rand -hex 32", minSetupTokenLen)
	}
	if c.BootstrapAdminEmail != "" && c.BootstrapAdminPassword == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD is required when BOOTSTRAP_ADMIN_EMAIL is set")
	}
	if c.BootstrapAdminPassword != "" && c.BootstrapAdminEmail == "" {
		return fmt.Errorf("BOOTSTRAP_ADMIN_EMAIL is required when BOOTSTRAP_ADMIN_PASSWORD is set")
	}
	if c.GlobalConfigRepo != "" && !validRepository(c.GlobalConfigRepo) {
		return fmt.Errorf("GLOBAL_CONFIG_REPO must be owner/repo")
	}
	return c.validateInternalListener()
}

// JobTokenRepositories returns the repositories besides the job's own that a job's token
// covers: the preset repositories, then the global config repository when one is set,
// without repeats.
func (c *Config) JobTokenRepositories() []string {
	repos := slices.Clone(c.PresetRepositories)
	if c.GlobalConfigRepo != "" && !slices.ContainsFunc(repos, func(r string) bool {
		return strings.EqualFold(r, c.GlobalConfigRepo)
	}) {
		repos = append(repos, c.GlobalConfigRepo)
	}
	return repos
}

// repositoryPattern is owner/repo as GitHub names them: an account name and a repository
// name made of letters, digits, dots, hyphens and underscores.
var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

func validRepository(s string) bool {
	return repositoryPattern.MatchString(s) && !strings.HasSuffix(s, "/.") && !strings.HasSuffix(s, "/..")
}

// parseRepositoryList parses a comma-separated owner/repo list, ignoring blank entries and
// repeats. A malformed entry is an error, so a typo does not silently narrow a job's token.
func parseRepositoryList(raw string) ([]string, error) {
	var repos []string
	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !validRepository(entry) {
			return nil, fmt.Errorf("%q is not owner/repo", entry)
		}
		if !slices.ContainsFunc(repos, func(r string) bool { return strings.EqualFold(r, entry) }) {
			repos = append(repos, entry)
		}
	}
	return repos, nil
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

// validateURL accepts an empty value unless required, and otherwise an http or https URL
// with a host.
func validateURL(name, raw string, required bool) error {
	if raw == "" {
		if required {
			return fmt.Errorf("%s is required", name)
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an http or https URL with a host", name)
	}
	return nil
}

func validateWorkerSecret(secret string) error {
	if len(secret) < minWorkerSecretLen {
		return fmt.Errorf("WORKER_SECRET must be at least %d bytes; generate one with: openssl rand -base64 32", minWorkerSecretLen)
	}
	return nil
}

// validateListenAddr checks that addr is a host:port with a numeric port, e.g. ":9100".
func validateListenAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("must be host:port, e.g. 127.0.0.1:9100")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port must be a number from 1 to 65535")
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
