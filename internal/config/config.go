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
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
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

	// Retention windows, in days (specs.html § 4.6 and § 2.10). The delivery window must outlast
	// GitHub's redelivery window of three days, so a record is never deleted while GitHub can
	// still repeat the delivery it names.
	DefaultJobHistoryRetentionDays      = 30
	DefaultWebhookDeliveryRetentionDays = 7
	minWebhookDeliveryRetentionDays     = 4
	maxRetentionDays                    = 3650
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
	SetupToken string
	// BootstrapAdminGitHubLogin is the GitHub login of the pinned first admin
	// (BOOTSTRAP_ADMIN_GITHUB_LOGIN); empty means the App's owner is the first admin. It is
	// resolved to a numeric id at the first matching sign-in and ignored afterwards.
	BootstrapAdminGitHubLogin string
	// AdminTeam and ViewerTeam are ADMIN_TEAM and VIEWER_TEAM as org/team-slug, empty when
	// unset: the members of AdminTeam are admins, and with ViewerTeam set only its members
	// (and admins) may sign in as viewers (docs/specs.html § 3.7).
	AdminTeam  string
	ViewerTeam string
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
	// GlobalConfigRef is GLOBAL_CONFIG_REF, the branch, tag or commit of GlobalConfigRepo the
	// worker fetches the global Renovate config and tools from at job start.
	GlobalConfigRef string
	// JobHistoryRetention is how long a finished job and its history are kept
	// (JOB_HISTORY_RETENTION_DAYS), and WebhookDeliveryRetention how long a webhook delivery's
	// GUID is remembered (WEBHOOK_DELIVERY_RETENTION_DAYS).
	JobHistoryRetention, WebhookDeliveryRetention time.Duration
}

// Load reads and validates configuration from the process environment.
func Load() (*Config, error) {
	cfg := &Config{
		SessionSecret:             strings.TrimSpace(os.Getenv("SESSION_SECRET")),
		AppEncryptionKey:          strings.TrimSpace(os.Getenv("APP_ENCRYPTION_KEY")),
		AppDBPath:                 envOrDefault("APP_DB_PATH", defaultAppDBPath),
		GoAPIURL:                  envOrDefault("GO_API_URL", defaultGoAPIURL),
		AppURL:                    strings.TrimRight(strings.TrimSpace(os.Getenv("APP_URL")), "/"),
		WebhookURL:                strings.TrimSpace(os.Getenv("WEBHOOK_URL")),
		GitHubBaseURL:             strings.TrimRight(envOrDefault("GITHUB_BASE_URL", defaultGitHubURL), "/"),
		SetupToken:                strings.TrimSpace(os.Getenv("SETUP_TOKEN")),
		BootstrapAdminGitHubLogin: strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_GITHUB_LOGIN")),
		AdminTeam:                 strings.TrimSpace(os.Getenv("ADMIN_TEAM")),
		ViewerTeam:                strings.TrimSpace(os.Getenv("VIEWER_TEAM")),
		InternalAddr:              strings.TrimSpace(os.Getenv("INTERNAL_ADDR")),
		WorkerSecret:              strings.TrimSpace(os.Getenv("WORKER_SECRET")),
		GlobalConfigRepo:          strings.TrimSpace(os.Getenv("GLOBAL_CONFIG_REPO")),
		GlobalConfigRef:           strings.TrimSpace(os.Getenv("GLOBAL_CONFIG_REF")),
		MetricsAddr:               envOrDefault("METRICS_ADDR", defaultMetricsAddr),
	}

	// The secrets are registered before anything can fail, so an error that quotes one is
	// already covered.
	logging.Register(cfg.SessionSecret, cfg.AppEncryptionKey, cfg.SetupToken, cfg.WorkerSecret)

	var err error
	if err = validateListenAddr(cfg.MetricsAddr); err != nil {
		return nil, fmt.Errorf("METRICS_ADDR: %w", err)
	}
	if cfg.PresetRepositories, err = parseRepositoryList(os.Getenv("PRESET_REPOSITORIES")); err != nil {
		return nil, fmt.Errorf("PRESET_REPOSITORIES: %w", err)
	}
	if cfg.JobHistoryRetention, err = retentionDays("JOB_HISTORY_RETENTION_DAYS", DefaultJobHistoryRetentionDays, 1); err != nil {
		return nil, err
	}
	if cfg.WebhookDeliveryRetention, err = retentionDays("WEBHOOK_DELIVERY_RETENTION_DAYS", DefaultWebhookDeliveryRetentionDays, minWebhookDeliveryRetentionDays); err != nil {
		return nil, err
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

// PollingMode reports that no webhook is configured (WEBHOOK_URL is unset): GitHub delivers no
// events, so the instance polls instead and runs degraded, with minutes of latency.
func (c *Config) PollingMode() bool {
	return c.WebhookURL == ""
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
	if c.BootstrapAdminGitHubLogin != "" && !validUserLogin(c.BootstrapAdminGitHubLogin) {
		return fmt.Errorf("BOOTSTRAP_ADMIN_GITHUB_LOGIN must be a GitHub login: up to 39 letters, digits and single hyphens, with an Enterprise Managed User's _shortcode (mona-cat_octo) allowed")
	}
	for name, team := range map[string]string{"ADMIN_TEAM": c.AdminTeam, "VIEWER_TEAM": c.ViewerTeam} {
		if team != "" && !validTeam(team) {
			return fmt.Errorf("%s must be org/team-slug, e.g. acme/release-ops-admins", name)
		}
	}
	if c.GlobalConfigRepo != "" && !validRepository(c.GlobalConfigRepo) {
		return fmt.Errorf("GLOBAL_CONFIG_REPO must be owner/repo")
	}
	if c.GlobalConfigRepo != "" && c.GlobalConfigRef == "" {
		return fmt.Errorf("GLOBAL_CONFIG_REF is required when GLOBAL_CONFIG_REPO is set: a branch, tag or commit")
	}
	if c.GlobalConfigRef != "" && !refPattern.MatchString(c.GlobalConfigRef) {
		return fmt.Errorf("GLOBAL_CONFIG_REF must be a branch, tag or commit name")
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

// refPattern is a branch, tag or commit name as the worker puts it in a request: no space,
// control or git-reserved character. It starts with a letter, a digit or an underscore, so
// it can never look like an option, a hidden name or a path.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/+@-]{0,254}$`)

// userLoginPattern is a GitHub user's login: letters, digits and single hyphens, neither
// starting nor ending with a hyphen, followed for an Enterprise Managed User by an underscore
// and the short code of the enterprise (mona-cat_octo). It is the rule internal/github applies
// to the login a user signs in with; config does not import that package, because the worker
// must not depend on the database, and a test keeps the two in step.
var userLoginPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*(?:_[A-Za-z0-9]{1,39})?$`)

func validUserLogin(s string) bool {
	name, _, _ := strings.Cut(s, "_")
	return len(name) <= 39 && userLoginPattern.MatchString(s)
}

// teamPattern is org/team-slug: a GitHub account name and a team slug of letters, digits,
// dots, hyphens and underscores.
var teamPattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func validTeam(s string) bool {
	org, _, _ := strings.Cut(s, "/")
	return len(org) <= 39 && teamPattern.MatchString(s)
}

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

// retentionDays reads a retention window in whole days from the environment. A value that is not
// a whole number of days from floor to maxRetentionDays stops the server at startup, naming the
// variable and the range, so a typo cannot silently keep history for a day or for good.
func retentionDays(key string, fallback, floor int) (time.Duration, error) {
	days := fallback
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("%s must be a whole number of days from %d to %d, got %q", key, floor, maxRetentionDays, raw)
		}
		days = n
	}
	if days < floor || days > maxRetentionDays {
		return 0, fmt.Errorf("%s must be a whole number of days from %d to %d, got %d", key, floor, maxRetentionDays, days)
	}
	return time.Duration(days) * 24 * time.Hour, nil
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
