package renovate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mdg-labs/release-ops/internal/logging"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/renovate/globalconfig"
	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

const (
	// DefaultDeadline is how long Renovate may run. The app hands a job a token with at least
	// 55 minutes left (github.JobTokenMinLife), so a run never outlives its token.
	DefaultDeadline = 45 * time.Minute
	// DefaultTokenMargin is kept between the end of a run and the token's expiry.
	DefaultTokenMargin = time.Minute
	// DefaultMinRun is the shortest run window worth starting Renovate for.
	DefaultMinRun = 2 * time.Minute
	// DefaultKillGrace is how long Renovate gets to stop on SIGTERM before it is killed.
	DefaultKillGrace = 10 * time.Second
	// DefaultCacheRoot is the worker's cache volume.
	DefaultCacheRoot = "/cache"
	// DefaultBinary is the pinned Renovate of the worker image.
	DefaultBinary = "renovate"
	// DefaultUser is the second Unix user of the worker image; it cannot read the worker secret.
	DefaultUser = "renovate"

	fetchTimeout = 2 * time.Minute
	// rawLogBytes is how much of the log's tail is kept before redaction and cutting.
	rawLogBytes = 4 * queue.MaxLogExcerptBytes
)

// Error codes of a failed attempt, stored as job_runs.error_code.
const (
	CodeInvalidJob         = "invalid_job"
	CodeTokenUnavailable   = "token_unavailable"
	CodeTokenRefused       = "token_refused"
	CodeTokenExpiring      = "token_expiring"
	CodeGlobalConfig       = "global_config"
	CodeUserUnavailable    = "user_unavailable"
	CodeSetup              = "setup_failed"
	CodeRenovateMissing    = "renovate_missing"
	CodeStartFailed        = "start_failed"
	CodeTimeout            = "timeout"
	CodeRenovateExit       = "renovate_exit"
	CodeRenovateTerminated = "renovate_terminated"
	CodeCacheScrub         = "cache_scrub"
	CodeRateLimited        = queue.ErrorCodeRateLimited
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]{1,100}$`)

// TokenSource gives a job's installation token; the worker's internal-API client does.
type TokenSource interface {
	Token(ctx context.Context, jobID, leaseOwner string) (queue.TokenResponse, error)
}

var _ TokenSource = (*worker.Client)(nil)

// User is the Unix user Renovate runs as.
type User struct {
	Name     string
	UID, GID uint32
	Home     string
}

// LookupUser resolves a user by name.
func LookupUser(name string) (User, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return User{}, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return User{}, fmt.Errorf("user %s: uid %q: %w", name, u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return User{}, fmt.Errorf("user %s: gid %q: %w", name, u.Gid, err)
	}
	return User{Name: name, UID: uint32(uid), GID: uint32(gid), Home: u.HomeDir}, nil
}

// Options configures the handler. Zero fields take their defaults.
type Options struct {
	// Tokens fetches the job's installation token.
	Tokens TokenSource
	// GitHubAPIURL is the GitHub REST API base URL; Renovate's endpoint and the global config
	// fetch both use it.
	GitHubAPIURL string
	// HTTP is the client of the global config fetch.
	HTTP *http.Client
	// User resolves the second Unix user Renovate is started as, once per job. A failure
	// fails the job: Renovate never runs as the worker's own user.
	User func() (User, error)
	// SameUser runs Renovate as the worker's own user instead of User. It exists for tests of
	// the handler on a machine without the second user; the worker command never sets it.
	SameUser bool
	// WorkerSecret is redacted from everything the handler posts or logs, besides the token.
	WorkerSecret string
	// Binary is the renovate executable, looked up on the worker's PATH.
	Binary string
	// CacheRoot holds one directory per slot; ConfigMount and ToolsMount are where an
	// operator mounts the global config and tools.
	CacheRoot, ConfigMount, ToolsMount string
	// Deadline, TokenMargin, MinRun and KillGrace bound a run: it ends Deadline after it
	// starts or TokenMargin before the token expires, whichever is sooner, and is not started
	// with less than MinRun in that window. KillGrace follows a SIGTERM before a SIGKILL.
	Deadline, TokenMargin, MinRun, KillGrace time.Duration
	// Now is the clock; tests replace it.
	Now    func() time.Time
	Logger *slog.Logger
}

// Handler runs Renovate for a claimed job.
type Handler struct {
	opts Options
	log  *slog.Logger
}

var _ worker.Handler = (*Handler)(nil)

// NewHandler returns the handler for opts.
func NewHandler(opts Options) (*Handler, error) {
	if opts.Tokens == nil {
		return nil, errors.New("renovate: a token source is required")
	}
	if opts.User == nil && !opts.SameUser {
		return nil, errors.New("renovate: the second Unix user is required")
	}
	if opts.GitHubAPIURL == "" {
		return nil, errors.New("renovate: the GitHub API URL is required")
	}
	opts.GitHubAPIURL = strings.TrimRight(opts.GitHubAPIURL, "/")
	if opts.Binary == "" {
		opts.Binary = DefaultBinary
	}
	if opts.CacheRoot == "" {
		opts.CacheRoot = DefaultCacheRoot
	}
	if opts.ConfigMount == "" {
		opts.ConfigMount = globalconfig.MountConfigDir
	}
	if opts.ToolsMount == "" {
		opts.ToolsMount = globalconfig.MountToolsDir
	}
	if opts.Deadline <= 0 {
		opts.Deadline = DefaultDeadline
	}
	if opts.TokenMargin <= 0 {
		opts.TokenMargin = DefaultTokenMargin
	}
	if opts.MinRun <= 0 {
		opts.MinRun = DefaultMinRun
	}
	if opts.KillGrace <= 0 {
		opts.KillGrace = DefaultKillGrace
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Handler{opts: opts, log: log}, nil
}

// failure is a failed attempt: a short code and a short, secret-free message.
type failure struct {
	code, msg string
	// retryAfter and rateLimit are set when GitHub's rate limit ended the attempt: the delay the
	// app should wait and what the attempt saw of the limit.
	retryAfter time.Duration
	rateLimit  *queue.RateLimitReport
}

func (f *failure) Error() string { return f.msg }

func fail(code, msg string) *failure { return &failure{code: code, msg: msg} }

// Run implements worker.Handler.
func (h *Handler) Run(ctx context.Context, job queue.ClaimedJob) worker.Result {
	var rec queue.RunRecord
	var seen *queue.RateLimitReport
	err := h.execute(ctx, job, &rec, &seen)
	if err == nil {
		return worker.Result{Run: &rec, RateLimit: seen}
	}
	var f *failure
	if !errors.As(err, &f) {
		// The attempt was stopped from outside: the runner gives the job back or drops it.
		return worker.Result{Err: err}
	}
	rec.ErrorCode, rec.ErrorMessage = f.code, f.msg
	if f.rateLimit == nil {
		f.rateLimit = seen
	}
	return worker.Result{Err: f, Run: &rec, RetryAfter: f.retryAfter, RateLimit: f.rateLimit}
}

// execute runs the job. seen is set to what Renovate's log shows of GitHub's rate limit, when it
// shows anything, however the attempt ends.
func (h *Handler) execute(ctx context.Context, job queue.ClaimedJob, rec *queue.RunRecord, seen **queue.RateLimitReport) error {
	if !repositoryPattern.MatchString(job.RepositoryFullName) || job.Slot < 0 || strings.ContainsAny(job.GitAuthor, "\x00\r\n") {
		return fail(CodeInvalidJob, "the job does not name a valid repository and slot")
	}

	runAs, err := h.runAs()
	if err != nil {
		return fail(CodeUserUnavailable, "the Renovate user is not available")
	}

	tok, err := h.opts.Tokens.Token(ctx, job.ID, job.LeaseOwner)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, worker.ErrLeaseLost) {
			return fail(CodeTokenRefused, "the app refused the installation token request")
		}
		var limited *worker.RateLimitedError
		if errors.As(err, &limited) {
			return rateLimitedFailure(nil, limited.RetryAfter)
		}
		return fail(CodeTokenUnavailable, "the installation token could not be obtained")
	}
	redactor := redact.New(tok.Token, h.opts.WorkerSecret)
	logging.RegisterFor(max(time.Until(tok.ExpiresAt), 0), tok.Token)

	now := h.opts.Now()
	runBy := now.Add(h.opts.Deadline)
	if tok.ExpiresAt.IsZero() {
		return fail(CodeTokenExpiring, "the installation token has no expiry")
	}
	if byToken := tok.ExpiresAt.Add(-h.opts.TokenMargin); byToken.Before(runBy) {
		runBy = byToken
	}
	if runBy.Sub(now) < h.opts.MinRun {
		return fail(CodeTokenExpiring, "the installation token expires too soon to run Renovate")
	}

	slotDir := filepath.Join(h.opts.CacheRoot, "slot-"+strconv.Itoa(job.Slot))
	cacheDir := filepath.Join(slotDir, "renovate")
	baseDir := filepath.Join(slotDir, "base")
	for _, dir := range []string{slotDir, cacheDir, baseDir} {
		if err := makeSharedDir(dir); err != nil {
			h.log.Error("renovate: cache directory", "job", job.ID, "err", redactor.String(err.Error()))
			return fail(CodeSetup, "the job's cache directory could not be prepared")
		}
	}
	// A job that died before its own scrub left its token behind; nothing may start over it.
	if err := scrubBaseDir(baseDir, tok.Token); err != nil {
		h.log.Error("renovate: cache scrub", "job", job.ID, "err", redactor.String(err.Error()))
		return fail(CodeCacheScrub, "the slot's working copies could not be cleared of credentials")
	}
	scratch, err := os.MkdirTemp(slotDir, "job-")
	if err == nil {
		err = makeSharedDir(scratch)
	}
	if err != nil {
		h.log.Error("renovate: scratch directory", "job", job.ID, "err", redactor.String(err.Error()))
		return fail(CodeSetup, "the job's scratch directory could not be prepared")
	}
	defer func() {
		if err := os.RemoveAll(scratch); err != nil {
			h.log.Warn("renovate: scratch directory not removed", "job", job.ID, "err", redactor.String(err.Error()))
		}
	}()

	cfg, err := h.globalConfig(ctx, job, tok.Token, scratch)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h.log.Warn("renovate: global config", "job", job.ID, "err", redactor.String(err.Error()))
		return fail(CodeGlobalConfig, globalConfigReason(err))
	}
	rec.GlobalConfig = h.captureGlobalConfig(job, cfg, redactor)
	cfgPath, removed, err := cfg.WithoutOwnedKeys(scratch)
	if err != nil {
		return fail(CodeSetup, "the global Renovate config could not be prepared")
	}
	for _, key := range removed {
		h.log.Warn("renovate: the global config sets a key Release Ops owns; it is ignored", "job", job.ID, "key", key)
	}
	cfgPath, err = withDefaults(cfgPath, scratch)
	if err != nil {
		h.log.Warn("renovate: global config defaults", "job", job.ID, "err", redactor.String(err.Error()))
		return fail(CodeGlobalConfig, globalConfigDefaultsReason(err))
	}

	bin, err := exec.LookPath(h.opts.Binary)
	if err != nil {
		return fail(CodeRenovateMissing, "the renovate executable was not found")
	}
	reportPath := filepath.Join(scratch, "report.json")
	home := runAs.Home
	cmd := &exec.Cmd{
		Path:        bin,
		Args:        []string{bin},
		Env:         h.environment(job, tok.Token, cfgPath, cfg.ToolsDir, baseDir, cacheDir, reportPath, home),
		Dir:         scratch,
		SysProcAttr: processAttr(runAs),
		WaitDelay:   2 * time.Second,
	}
	logs := newTailBuffer(rawLogBytes)
	cmd.Stdout, cmd.Stderr = logs, logs

	h.log.Info("renovate: starting", "job", job.ID, "repository", job.RepositoryFullName, "slot", job.Slot)
	outcome := h.runProcess(ctx, cmd, runBy, runAs)

	// Whatever ended the run, the token Renovate wrote into the kept working copies goes now.
	scrubErr := scrubBaseDir(baseDir, tok.Token)
	if scrubErr != nil {
		h.log.Error("renovate: cache scrub after the run", "job", job.ID, "err", redactor.String(scrubErr.Error()))
	}

	rawLog := logs.String()
	*seen = rateLimitFromLog(rawLog, hostOf(h.opts.GitHubAPIURL), h.opts.Now())
	rec.LogExcerpt = tailBytes(redactor.String(rawLog), queue.MaxLogExcerptBytes)
	rec.ReportStatus, rec.Report = readReport(reportPath, redactor)
	if outcome.exit != nil {
		code := *outcome.exit
		rec.ExitCode = &code
	}

	switch {
	case outcome.startErr != nil:
		h.log.Error("renovate: start failed", "job", job.ID, "err", redactor.String(outcome.startErr.Error()))
		return fail(CodeStartFailed, "renovate could not be started")
	case outcome.cancelled:
		return ctx.Err()
	case *seen != nil && (*seen).Limited:
		return rateLimitedFailure(*seen, retryAfterOf(*seen, h.opts.Now()))
	case outcome.timedOut:
		return fail(CodeTimeout, "renovate exceeded the job deadline and was stopped")
	case outcome.exit == nil:
		return fail(CodeRenovateTerminated, "renovate was terminated by a signal")
	case *outcome.exit != 0:
		return fail(CodeRenovateExit, fmt.Sprintf("renovate exited with status %d", *outcome.exit))
	case scrubErr != nil:
		return fail(CodeCacheScrub, "the slot's working copies could not be cleared of credentials")
	}
	return nil
}

// captureGlobalConfig is the config as the result carries it: the file's own text, never
// parsed, with the job's secrets and token-shaped strings replaced. A text that is still over
// the cap the app accepts is left out, so the app keeps the copy it has.
func (h *Handler) captureGlobalConfig(job queue.ClaimedJob, cfg *globalconfig.Config, redactor *redact.Redactor) *queue.GlobalConfigCapture {
	content := redactor.String(cfg.Raw)
	if len(content) > queue.MaxGlobalConfigBytes {
		h.log.Warn("renovate: the global config is too large to report", "job", job.ID)
		return nil
	}
	source := queue.GlobalConfigSourceRepository
	if cfg.Source == globalconfig.SourceMounted {
		source = queue.GlobalConfigSourceMounted
	}
	return &queue.GlobalConfigCapture{Source: source, Content: content}
}

func (h *Handler) runAs() (User, error) {
	if h.opts.SameUser {
		return User{Home: os.Getenv("HOME")}, nil
	}
	u, err := h.opts.User()
	if err != nil {
		return User{}, err
	}
	if u.Name == "" || u.UID == uint32(os.Getuid()) {
		return User{}, errors.New("the Renovate user is the worker's own user or has no name")
	}
	return u, nil
}

// processAttr puts the child in its own process group, so the whole tree can be signalled, and
// switches it to the second Unix user.
func processAttr(u User) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{Setpgid: true}
	if u.Name != "" {
		attr.Credential = credential(u)
	}
	return attr
}

// credential is the switch to u. The supplementary groups of the worker are cleared.
func credential(u User) *syscall.Credential {
	return &syscall.Credential{Uid: u.UID, Gid: u.GID}
}

// makeSharedDir creates dir for the worker and the Renovate user, which share the root group:
// group-writable and setgid, so what either creates stays in that group.
func makeSharedDir(dir string) error {
	if err := os.MkdirAll(dir, 0o775); err != nil {
		return err
	}
	return os.Chmod(dir, 0o775|os.ModeSetgid)
}

// globalConfig finds the global config: the mount when the operator mounted one, otherwise
// the repository the app named with the job, fetched with the job's token.
func (h *Handler) globalConfig(ctx context.Context, job queue.ClaimedJob, token, scratch string) (*globalconfig.Config, error) {
	cfg, err := globalconfig.Mounted(h.opts.ConfigMount, h.opts.ToolsMount)
	if !errors.Is(err, globalconfig.ErrNotMounted) {
		return cfg, err
	}
	if job.GlobalConfigRepo == "" {
		return nil, errNoGlobalConfig
	}
	dest := filepath.Join(scratch, "global")
	if err := os.Mkdir(dest, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	return globalconfig.Fetch(ctx, globalconfig.FetchOptions{
		HTTP: h.opts.HTTP, APIBase: h.opts.GitHubAPIURL, Token: token,
		Repo: job.GlobalConfigRepo, Ref: job.GlobalConfigRef, Dest: dest,
	})
}

var errNoGlobalConfig = errors.New("no global config is mounted and none is configured to fetch")

func globalConfigReason(err error) string {
	switch {
	case errors.Is(err, errNoGlobalConfig):
		return "no global Renovate config is mounted and none is configured to fetch"
	case errors.Is(err, globalconfig.ErrNoConfig):
		return "the config repository has no global Renovate config file"
	case errors.Is(err, globalconfig.ErrTooLarge):
		return "the global Renovate config or its tools exceed the size limit"
	default:
		return "the global Renovate config could not be fetched or read"
	}
}

// configDefaults are the settings Release Ops applies beneath the global config file
// (specs.html § 2.5): the file keeps any of them it sets. They cannot go in the child's
// environment, which Renovate reads over the file.
var configDefaults = map[string]any{
	"onboarding":     false,
	"requireConfig":  "required",
	"platformCommit": "enabled",
	"binarySource":   "install",
}

var errConfigNotJSON = errors.New("the global config is not parseable JSON")

func globalConfigDefaultsReason(err error) string {
	if errors.Is(err, errConfigNotJSON) {
		return "the global Renovate config is not valid JSON, so the Release Ops defaults cannot be applied beneath it"
	}
	return "the Release Ops defaults could not be applied beneath the global Renovate config"
}

// withDefaults returns the path of the config at path with configDefaults beneath it, written
// into dir. A JSON config gets each default it does not set itself at the top level. A
// JavaScript config is not evaluated by the worker: it is wrapped by a module in dir that
// loads it in Renovate's own process and merges the defaults beneath what it exports, which
// may be an object or a function. A config that already sets every default is returned as it is.
func withDefaults(path, dir string) (string, error) {
	if filepath.Ext(path) != ".json" {
		return wrapWithDefaults(path, dir)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&doc); err != nil {
		return "", errConfigNotJSON
	}
	added := false
	for key, value := range configDefaults {
		if _, ok := doc[key]; ok {
			continue
		}
		enc, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		doc[key] = enc
		added = true
	}
	if !added {
		return path, nil
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "config-with-defaults.json")
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return "", fmt.Errorf("write global config: %w", err)
	}
	return p, nil
}

// wrapWithDefaults writes the module that stands in for the JavaScript config at path.
func wrapWithDefaults(path, dir string) (string, error) {
	defaults, err := json.Marshal(configDefaults)
	if err != nil {
		return "", err
	}
	target, err := json.Marshal(path)
	if err != nil {
		return "", err
	}
	src := "const defaults = " + string(defaults) + ";\n" +
		"const loaded = require(" + string(target) + ");\n" +
		"const config = loaded.default ? loaded.default : loaded;\n" +
		"const merge = (c) => ({ ...defaults, ...c });\n" +
		"module.exports = typeof config === 'function'\n" +
		"  ? (...args) => {\n" +
		"      const out = config(...args);\n" +
		"      return out && typeof out.then === 'function' ? out.then(merge) : merge(out);\n" +
		"    }\n" +
		"  : merge(config);\n"
	p := filepath.Join(dir, "config-with-defaults.js")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		return "", fmt.Errorf("write global config: %w", err)
	}
	return p, nil
}

// environment is the child's whole environment: nothing of the worker's own is inherited, so
// neither the worker secret nor any other setting reaches Renovate. allowedCommands is not
// set here: it lives in the admin's global config file only.
func (h *Handler) environment(job queue.ClaimedJob, token, cfgPath, toolsDir, baseDir, cacheDir, reportPath, home string) []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	if toolsDir != "" {
		path += string(os.PathListSeparator) + toolsDir
	}
	env := []string{
		"PATH=" + path,
		"HOME=" + home,
		"LOG_FORMAT=json",
		"LOG_LEVEL=info",
		"RENOVATE_PLATFORM=github",
		"RENOVATE_TOKEN=" + token,
		"RENOVATE_REPOSITORIES=" + job.RepositoryFullName,
		"RENOVATE_ENDPOINT=" + h.opts.GitHubAPIURL + "/",
		"RENOVATE_CONFIG_FILE=" + cfgPath,
		"RENOVATE_BASE_DIR=" + baseDir,
		"RENOVATE_CACHE_DIR=" + cacheDir,
		"RENOVATE_REPOSITORY_CACHE=enabled",
		"RENOVATE_PERSIST_REPO_DATA=true",
		"RENOVATE_ALLOW_SCRIPTS=false",
		"RENOVATE_REPORT_TYPE=file",
		"RENOVATE_REPORT_PATH=" + reportPath,
	}
	if job.GitAuthor != "" {
		env = append(env, "RENOVATE_GIT_AUTHOR="+job.GitAuthor)
	}
	return env
}

// readReport reads Renovate's report file: ok when it is valid JSON and, redacted, within the
// cap; missing when there is none and unreadable otherwise, which includes anything that is not
// a regular file. The report is returned redacted, and none is returned unless it is ok. A
// report never fails a job.
func readReport(path string, redactor *redact.Redactor) (status, report string) {
	// O_NONBLOCK keeps the open of a FIFO that has no writer from waiting for one; the opened
	// descriptor is then checked, so only a regular file is ever read.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return queue.ReportMissing, ""
	}
	if err != nil {
		return queue.ReportUnreadable, ""
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return queue.ReportUnreadable, ""
	}
	raw, err := io.ReadAll(io.LimitReader(f, queue.MaxReportBytes+1))
	if err != nil || len(raw) > queue.MaxReportBytes || !json.Valid(raw) {
		return queue.ReportUnreadable, ""
	}
	redacted := redactor.String(string(raw))
	if len(redacted) > queue.MaxReportBytes {
		return queue.ReportUnreadable, ""
	}
	return queue.ReportOK, redacted
}
