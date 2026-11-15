package renovate_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/renovate"
	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

const (
	jobToken     = "ghs_JobTokenForTheRenovateHandlerTest0123456789"
	workerSecret = "worker-secret-0123456789abcdef0123456789abcdef"
	gitAuthor    = "release-ops-acme[bot] <424242+release-ops-acme[bot]@users.noreply.github.com>"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type stubTokens struct {
	mu        sync.Mutex
	calls     []string
	expiresIn time.Duration
	err       error
}

func (s *stubTokens) Token(_ context.Context, jobID, leaseOwner string) (queue.TokenResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, jobID+"/"+leaseOwner)
	if s.err != nil {
		return queue.TokenResponse{}, s.err
	}
	return queue.TokenResponse{Token: jobToken, ExpiresAt: time.Now().Add(s.expiresIn)}, nil
}

type env struct {
	t         *testing.T
	h         *renovate.Handler
	tokens    *stubTokens
	logs      *syncBuffer
	cacheRoot string
	cfgMount  string
	out       string
	binary    string
}

type envOption func(*renovate.Options)

// newEnv builds a handler over a fake renovate script. The script runs with the environment the
// handler gives it and sees $OUT, a directory of the test.
func newEnv(t *testing.T, script string, opts ...envOption) *env {
	t.Helper()
	dir := t.TempDir()
	e := &env{
		t: t, tokens: &stubTokens{expiresIn: time.Hour}, logs: &syncBuffer{},
		cacheRoot: filepath.Join(dir, "cache"), cfgMount: filepath.Join(dir, "config"), out: filepath.Join(dir, "out"),
	}
	for _, d := range []string{e.cacheRoot, e.cfgMount, e.out} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(e.cfgMount, "config.json"), []byte(`{"onboarding": false, "requireConfig": "required"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "renovate")
	body := "#!/bin/sh\nOUT=" + e.out + "\n" + script + "\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	o := renovate.Options{
		Tokens: e.tokens, GitHubAPIURL: "https://api.github.example/v3", SameUser: true, WorkerSecret: workerSecret,
		Binary: bin, CacheRoot: e.cacheRoot, ConfigMount: e.cfgMount, ToolsMount: filepath.Join(dir, "no-tools"),
		KillGrace: 300 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	for _, opt := range opts {
		opt(&o)
	}
	e.binary = bin
	h, err := renovate.NewHandler(o)
	if err != nil {
		t.Fatal(err)
	}
	e.h = h
	return e
}

func job(slot int) queue.ClaimedJob {
	return queue.ClaimedJob{
		ID: "job-" + strconv.Itoa(slot), Kind: queue.KindRenovateRun, RepositoryID: "repo-1", RepositoryFullName: "acme/repo-1",
		Slot: slot, LeaseOwner: strconv.Itoa(slot) + ".lease", GitAuthor: gitAuthor,
	}
}

func (e *env) read(name string) string {
	e.t.Helper()
	raw, err := os.ReadFile(filepath.Join(e.out, name))
	if err != nil {
		e.t.Fatalf("read %s: %v", name, err)
	}
	return string(raw)
}

func (e *env) exists(name string) bool {
	_, err := os.Stat(filepath.Join(e.out, name))
	return err == nil
}

// jobDirs lists what is left of per-job scratch directories under every slot.
func (e *env) jobDirs() []string {
	e.t.Helper()
	left, err := filepath.Glob(filepath.Join(e.cacheRoot, "slot-*", "job-*"))
	if err != nil {
		e.t.Fatal(err)
	}
	return left
}

func wantFailure(t *testing.T, res worker.Result, code string) {
	t.Helper()
	if res.Err == nil || res.Run == nil {
		t.Fatalf("result = %+v, want a failed attempt with a record", res)
	}
	if res.Run.ErrorCode != code {
		t.Fatalf("error code = %q (%s), want %q", res.Run.ErrorCode, res.Run.ErrorMessage, code)
	}
	if res.Err.Error() != res.Run.ErrorMessage || res.Run.ErrorMessage == "" || len(res.Run.ErrorMessage) > 120 {
		t.Errorf("the reason is %q, the record says %q: want one short message", res.Err, res.Run.ErrorMessage)
	}
}

// secretFree fails the test when anything the handler sent on or logged holds a secret.
func secretFree(t *testing.T, e *env, res worker.Result) {
	t.Helper()
	text := e.logs.String()
	if res.Err != nil {
		text += res.Err.Error()
	}
	if res.Run != nil {
		raw, err := json.Marshal(res.Run)
		if err != nil {
			t.Fatal(err)
		}
		text += string(raw)
	}
	for _, secret := range []string{jobToken, workerSecret, base64.StdEncoding.EncodeToString([]byte(jobToken))} {
		if strings.Contains(text, secret) {
			t.Errorf("a secret (%.8s...) is in the result or the logs:\n%.600s", secret, text)
		}
	}
}

func TestRunGivesRenovateExactlyTheListedEnvironment(t *testing.T) {
	// What the worker's own environment holds must never reach the child.
	canaries := map[string]string{
		"WORKER_SECRET": workerSecret, "APP_ENCRYPTION_KEY": strings.Repeat("ab", 32), "SESSION_SECRET": "session-secret-canary",
		"GITHUB_APP_PRIVATE_KEY": "-----BEGIN RSA PRIVATE KEY-----canary", "INTERNAL_URL": "http://app:9090",
		"GITHUB_TOKEN": "ghp_canarycanarycanarycanary0123456789", "RENOVATE_TOKEN": "ghp_hostrenovatetoken0123456789012345",
		"RENOVATE_ALLOWED_COMMANDS": `["rm -rf /"]`, "RENOVATE_ALLOW_SCRIPTS": "allow-canary", "HTTP_PROXY": "http://proxy.invalid",
	}
	for k, v := range canaries {
		t.Setenv(k, v)
	}
	e := newEnv(t, `env > "$OUT/env"; echo '{"repositories":{}}' > "$RENOVATE_REPORT_PATH"`)

	res := e.h.Run(context.Background(), job(1))
	if res.Err != nil {
		t.Fatalf("Run: %v (%+v)", res.Err, res.Run)
	}

	got := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(e.read("env")), "\n") {
		k, v, _ := strings.Cut(line, "=")
		got[k] = v
	}
	// The fake is a shell script, and dash adds PWD to what it started with.
	delete(got, "PWD")

	cacheDir := filepath.Join(e.cacheRoot, "slot-1", "renovate")
	want := map[string]string{
		"PATH": os.Getenv("PATH"), "HOME": os.Getenv("HOME"), "LOG_FORMAT": "json", "LOG_LEVEL": "info",
		"RENOVATE_PLATFORM": "github", "RENOVATE_TOKEN": jobToken, "RENOVATE_REPOSITORIES": "acme/repo-1",
		"RENOVATE_GIT_AUTHOR": gitAuthor, "RENOVATE_ENDPOINT": "https://api.github.example/v3/",
		"RENOVATE_CONFIG_FILE": filepath.Join(e.cfgMount, "config.json"), "RENOVATE_CACHE_DIR": cacheDir,
		"RENOVATE_REPOSITORY_CACHE": "enabled", "RENOVATE_PERSIST_REPO_DATA": "true", "RENOVATE_ALLOW_SCRIPTS": "false",
		"RENOVATE_REPORT_TYPE": "file",
	}
	report, base := got["RENOVATE_REPORT_PATH"], got["RENOVATE_BASE_DIR"]
	if !strings.HasPrefix(filepath.Dir(report), filepath.Join(e.cacheRoot, "slot-1", "job-")) {
		t.Errorf("report %q is not in a per-job scratch directory of slot 1", report)
	}
	if base != filepath.Join(e.cacheRoot, "slot-1", "base") {
		t.Errorf("base dir %q is not the slot's own directory on the cache volume", base)
	}
	delete(got, "RENOVATE_REPORT_PATH")
	delete(got, "RENOVATE_BASE_DIR")

	if !slices.Equal(sortedKeys(got), sortedKeys(want)) {
		t.Errorf("environment variables = %v\nwant              %v", sortedKeys(got), sortedKeys(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	dump := e.read("env")
	for k, v := range canaries {
		if strings.Contains(dump, v) {
			t.Errorf("the host's %s reached the child", k)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestRunOmitsTheGitAuthorWhileTheBotUserIsUnknown(t *testing.T) {
	e := newEnv(t, `env > "$OUT/env"`)
	j := job(0)
	j.GitAuthor = ""
	if res := e.h.Run(context.Background(), j); res.Err != nil {
		t.Fatalf("Run: %v", res.Err)
	}
	if strings.Contains(e.read("env"), "RENOVATE_GIT_AUTHOR") {
		t.Error("an empty git author was passed on")
	}
}

func TestRunRecordsExitCodeLogExcerptAndReport(t *testing.T) {
	e := newEnv(t, `
echo '{"level":30,"msg":"first"}'
echo '{"level":30,"msg":"last line of the log"}'
echo 'stderr noise' >&2
echo '{"repositories":{"acme/repo-1":{"branches":[]}}}' > "$RENOVATE_REPORT_PATH"`)

	res := e.h.Run(context.Background(), job(0))
	if res.Err != nil || res.Run == nil {
		t.Fatalf("result = %+v", res)
	}
	run := res.Run
	if run.ExitCode == nil || *run.ExitCode != 0 || run.ReportStatus != queue.ReportOK || run.ErrorCode != "" {
		t.Errorf("run = %+v", run)
	}
	if !strings.Contains(run.LogExcerpt, "last line of the log") || !strings.Contains(run.LogExcerpt, "stderr noise") {
		t.Errorf("log excerpt = %q", run.LogExcerpt)
	}
	if !strings.Contains(run.Report, `"acme/repo-1"`) {
		t.Errorf("report = %q", run.Report)
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

func TestRunKeepsTheTailOfALongLogWithinTheCap(t *testing.T) {
	e := newEnv(t, `
i=0
while [ $i -lt 6000 ]; do echo "{\"level\":30,\"msg\":\"line $i padding padding padding padding padding\"}"; i=$((i+1)); done`)
	res := e.h.Run(context.Background(), job(0))
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	ex := res.Run.LogExcerpt
	if len(ex) == 0 || len(ex) > queue.MaxLogExcerptBytes {
		t.Fatalf("excerpt is %d bytes, cap %d", len(ex), queue.MaxLogExcerptBytes)
	}
	if !strings.Contains(ex, `"line 5999 `) || strings.Contains(ex, `"line 0 `) {
		t.Error("the excerpt does not keep the tail of the log")
	}
	if res.Run.ReportStatus != queue.ReportMissing {
		t.Errorf("report status = %q, want missing: the fake wrote none", res.Run.ReportStatus)
	}
}

// A fake that prints the token on stdout, on stderr and in its report leaves it nowhere:
// not in the record, not in the logs, not in an error.
func TestTokenNeverReachesTheRecordTheLogsOrAnError(t *testing.T) {
	for name, exit := range map[string]int{"succeeding run": 0, "failing run": 1} {
		t.Run(name, func(t *testing.T) {
			b64 := base64.StdEncoding.EncodeToString([]byte(jobToken))
			e := newEnv(t, fmt.Sprintf(`
echo "stdout $RENOVATE_TOKEN"
echo "stderr $RENOVATE_TOKEN https://x-access-token:$RENOVATE_TOKEN@github.com/acme/repo-1.git" >&2
echo "encoded %s and %s" >&2
printf '{"token":"%%s","worker":"%s"}' "$RENOVATE_TOKEN" > "$RENOVATE_REPORT_PATH"
exit %d`, b64, "$RENOVATE_TOKEN", workerSecret, exit))

			res := e.h.Run(context.Background(), job(0))
			if res.Run == nil {
				t.Fatalf("no record: %+v", res)
			}
			if (res.Err != nil) != (exit != 0) {
				t.Fatalf("err = %v for exit %d", res.Err, exit)
			}
			secretFree(t, e, res)
			if !strings.Contains(res.Run.LogExcerpt, "stdout [REDACTED]") || res.Run.ReportStatus != queue.ReportOK {
				t.Errorf("the run was not recorded redacted: %+v", res.Run)
			}
		})
	}
}

func TestNonZeroExitAndAnUnusableReport(t *testing.T) {
	t.Run("non-zero exit", func(t *testing.T) {
		e := newEnv(t, `echo boom; exit 3`)
		res := e.h.Run(context.Background(), job(0))
		wantFailure(t, res, renovate.CodeRenovateExit)
		if res.Run.ExitCode == nil || *res.Run.ExitCode != 3 || !strings.Contains(res.Run.LogExcerpt, "boom") || res.Run.ErrorMessage != "renovate exited with status 3" {
			t.Errorf("run = %+v", res.Run)
		}
	})
	t.Run("invalid report", func(t *testing.T) {
		e := newEnv(t, `echo '{"repositories": ' > "$RENOVATE_REPORT_PATH"`)
		res := e.h.Run(context.Background(), job(0))
		if res.Err != nil || res.Run == nil || res.Run.ErrorCode != "" {
			t.Fatalf("an invalid report failed a run that exited 0: %+v (%+v)", res, res.Run)
		}
		if res.Run.ReportStatus != queue.ReportUnreadable || res.Run.Report != "" || *res.Run.ExitCode != 0 {
			t.Errorf("run = %+v", res.Run)
		}
	})
	t.Run("invalid report after a non-zero exit", func(t *testing.T) {
		e := newEnv(t, `echo '{"repositories": ' > "$RENOVATE_REPORT_PATH"; exit 2`)
		res := e.h.Run(context.Background(), job(0))
		wantFailure(t, res, renovate.CodeRenovateExit)
		if res.Run.ReportStatus != queue.ReportUnreadable || res.Run.Report != "" {
			t.Errorf("run = %+v", res.Run)
		}
	})
	t.Run("oversized report", func(t *testing.T) {
		e := newEnv(t, `printf '{"x":"' > "$RENOVATE_REPORT_PATH"; head -c 70000 /dev/zero | tr '\0' a >> "$RENOVATE_REPORT_PATH"; printf '"}' >> "$RENOVATE_REPORT_PATH"`)
		res := e.h.Run(context.Background(), job(0))
		if res.Err != nil || res.Run == nil || res.Run.ErrorCode != "" {
			t.Fatalf("an oversized report failed a run that exited 0: %+v (%+v)", res, res.Run)
		}
		if res.Run.ReportStatus != queue.ReportUnreadable || res.Run.Report != "" {
			t.Errorf("run = %+v", res.Run)
		}
	})
	// Redaction makes a report longer, and what the worker posts is capped: a report that is
	// within the cap as Renovate wrote it but over it once redacted is not posted.
	t.Run("report over the cap once redacted", func(t *testing.T) {
		e := newEnv(t, `cp "$OUT/in.json" "$RENOVATE_REPORT_PATH"`)
		raw := `{"u":"` + strings.Repeat("git+ssh://git@host/x ", 3000) + `"}`
		if err := os.WriteFile(filepath.Join(e.out, "in.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if redacted := redact.Shapes(raw); len(raw) > queue.MaxReportBytes || len(redacted) <= queue.MaxReportBytes {
			t.Fatalf("fixture: %d bytes raw, %d redacted; want under the cap raw and over it redacted", len(raw), len(redacted))
		}
		res := e.h.Run(context.Background(), job(0))
		if res.Err != nil || res.Run == nil || res.Run.ErrorCode != "" {
			t.Fatalf("the run failed: %+v (%+v)", res, res.Run)
		}
		if res.Run.ReportStatus != queue.ReportUnreadable || res.Run.Report != "" {
			t.Errorf("status = %q, report of %d bytes", res.Run.ReportStatus, len(res.Run.Report))
		}
	})
	t.Run("a large report within the cap is kept", func(t *testing.T) {
		e := newEnv(t, `printf '{"x":"' > "$RENOVATE_REPORT_PATH"; head -c 60000 /dev/zero | tr '\0' a >> "$RENOVATE_REPORT_PATH"; printf '"}' >> "$RENOVATE_REPORT_PATH"`)
		res := e.h.Run(context.Background(), job(0))
		if res.Err != nil || res.Run.ReportStatus != queue.ReportOK || len(res.Run.Report) != 60008 || len(res.Run.Report) > queue.MaxReportBytes {
			t.Errorf("result = %+v (%d bytes)", res, len(res.Run.Report))
		}
	})
	t.Run("no report is not a failure", func(t *testing.T) {
		e := newEnv(t, `echo done`)
		res := e.h.Run(context.Background(), job(0))
		if res.Err != nil || res.Run.ReportStatus != queue.ReportMissing {
			t.Errorf("result = %+v (%+v)", res, res.Run)
		}
	})
	t.Run("a signal the handler did not send", func(t *testing.T) {
		e := newEnv(t, `kill -9 $$`)
		wantFailure(t, e.h.Run(context.Background(), job(0)), renovate.CodeRenovateTerminated)
	})
	t.Run("a missing binary", func(t *testing.T) {
		e := newEnv(t, `true`, func(o *renovate.Options) { o.Binary = "/nonexistent/renovate" })
		wantFailure(t, e.h.Run(context.Background(), job(0)), renovate.CodeRenovateMissing)
	})
}

func TestTokenFailuresFailTheJobWithAShortReasonAndStartNothing(t *testing.T) {
	leaky := errors.New("Post http://app:9090/internal/v1/jobs/j/token: dial tcp 10.0.0.2:9090: connection refused (" + workerSecret + ")")
	for name, tc := range map[string]struct {
		err  error
		code string
	}{
		"the endpoint refuses the job": {fmt.Errorf("token: %w", worker.ErrLeaseLost), renovate.CodeTokenRefused},
		"the endpoint is unreachable":  {leaky, renovate.CodeTokenUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, `touch "$OUT/started"`)
			e.tokens.err = tc.err
			res := e.h.Run(context.Background(), job(0))
			wantFailure(t, res, tc.code)
			secretFree(t, e, res)
			if strings.Contains(res.Err.Error(), "10.0.0.2") {
				t.Errorf("the reason echoes the transport error: %q", res.Err)
			}
			if e.exists("started") || len(e.jobDirs()) != 0 {
				t.Error("Renovate ran, or a scratch directory was left, after a failed token request")
			}
		})
	}
}

// The deadline is the sooner of the configured one and the token's expiry less the margin.
func TestDeadlineKillsAHungRunBeforeTheTokenExpires(t *testing.T) {
	for name, script := range map[string]string{
		"stops on SIGTERM":    `echo $$ > "$OUT/pid"; exec sleep 60`,
		"ignores SIGTERM":     `trap '' TERM; echo $$ > "$OUT/pid"; while true; do sleep 1; done`,
		"leaves a grandchild": `sleep 60 & echo $! > "$OUT/pid"; wait`,
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, script, func(o *renovate.Options) {
				o.Deadline = time.Hour
				o.TokenMargin = 200 * time.Millisecond
				o.MinRun = 50 * time.Millisecond
			})
			expiry := 1500 * time.Millisecond
			e.tokens.expiresIn = expiry

			started := time.Now()
			res := e.h.Run(context.Background(), job(0))
			elapsed := time.Since(started)

			wantFailure(t, res, renovate.CodeTimeout)
			if res.Run.ExitCode != nil {
				t.Errorf("exit code = %d, want none for a killed run", *res.Run.ExitCode)
			}
			if elapsed >= expiry-200*time.Millisecond+time.Second {
				t.Errorf("the run lasted %v; the token expires after %v", elapsed, expiry)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(e.read("pid")))
			if err != nil {
				t.Fatal(err)
			}
			waitGone(t, pid)
			if left := e.jobDirs(); len(left) != 0 {
				t.Errorf("scratch directories left behind: %v", left)
			}
		})
	}
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d is still running", pid)
}

func TestTheDefaultDeadlineEndsAHungRun(t *testing.T) {
	e := newEnv(t, `echo $$ > "$OUT/pid"; exec sleep 60`, func(o *renovate.Options) {
		o.Deadline = 300 * time.Millisecond
		o.MinRun = 50 * time.Millisecond
	})
	started := time.Now()
	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeTimeout)
	if time.Since(started) > 5*time.Second {
		t.Errorf("a 300ms deadline took %v", time.Since(started))
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(e.read("pid")))
	waitGone(t, pid)
}

// A token the app hands out with little life left cannot carry a full run.
func TestANearlyExpiredTokenIsNotUsed(t *testing.T) {
	e := newEnv(t, `touch "$OUT/started"`, func(o *renovate.Options) {
		o.TokenMargin = time.Minute
		o.MinRun = 2 * time.Minute
	})
	e.tokens.expiresIn = 90 * time.Second
	res := e.h.Run(context.Background(), job(0))
	wantFailure(t, res, renovate.CodeTokenExpiring)
	if e.exists("started") || len(e.jobDirs()) != 0 {
		t.Error("Renovate started on a token about to expire")
	}
}

func TestAnAbortStopsTheRunAndGivesNoRecord(t *testing.T) {
	e := newEnv(t, `echo $$ > "$OUT/pid"; exec sleep 60`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan worker.Result, 1)
	go func() { done <- e.h.Run(ctx, job(0)) }()

	deadline := time.Now().Add(3 * time.Second)
	for !e.exists("pid") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case res := <-done:
		if !errors.Is(res.Err, context.Canceled) || res.Run != nil {
			t.Errorf("result = %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run did not stop on abort")
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(e.read("pid")))
	waitGone(t, pid)
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

// Jobs in different slots run in different cache directories and never see each other's.
func TestSlotsUseSeparateCaches(t *testing.T) {
	e := newEnv(t, `
slot=$(basename "$(dirname "$RENOVATE_CACHE_DIR")")
echo "$RENOVATE_CACHE_DIR" > "$OUT/cache-$slot"
touch "$RENOVATE_CACHE_DIR/written-by-$slot"
i=0
while [ ! -e "$OUT/cache-slot-0" ] || [ ! -e "$OUT/cache-slot-1" ]; do
  i=$((i+1)); [ $i -gt 400 ] && exit 9; sleep 0.01
done
ls "$RENOVATE_CACHE_DIR" > "$OUT/ls-$slot"`)

	var wg sync.WaitGroup
	results := make([]worker.Result, 2)
	for slot := range 2 {
		wg.Go(func() { results[slot] = e.h.Run(context.Background(), job(slot)) })
	}
	wg.Wait()
	for slot, res := range results {
		if res.Err != nil {
			t.Fatalf("slot %d: %v", slot, res.Err)
		}
	}

	c0, c1 := strings.TrimSpace(e.read("cache-slot-0")), strings.TrimSpace(e.read("cache-slot-1"))
	if c0 == c1 || c0 != filepath.Join(e.cacheRoot, "slot-0", "renovate") || c1 != filepath.Join(e.cacheRoot, "slot-1", "renovate") {
		t.Errorf("cache directories: %q and %q", c0, c1)
	}
	if got := strings.TrimSpace(e.read("ls-slot-0")); got != "written-by-slot-0" {
		t.Errorf("slot 0 saw %q", got)
	}
	if got := strings.TrimSpace(e.read("ls-slot-1")); got != "written-by-slot-1" {
		t.Errorf("slot 1 saw %q", got)
	}

	// A slot's cache outlives the job, the job's scratch directory does not.
	if _, err := os.Stat(filepath.Join(c0, "written-by-slot-0")); err != nil {
		t.Errorf("the slot cache did not persist: %v", err)
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

// Renovate's base directory is the slot's, on the cache volume, so the working copies it
// keeps with persistRepoData survive a job: a second job in the slot finds what the first one
// kept, and a job in another slot does not.
func TestBaseDirectoryIsKeptPerSlot(t *testing.T) {
	e := newEnv(t, `
slot=$(basename "$(dirname "$RENOVATE_BASE_DIR")")
copies="$RENOVATE_BASE_DIR/repos/github/acme"
mkdir -p "$copies"
ls "$copies" > "$OUT/seen-$slot"
mkdir -p "$copies/kept-by-$slot/.git"
touch "$copies/kept-by-$slot/.git/config"`)
	seen := func(slot int) string {
		t.Helper()
		return strings.TrimSpace(e.read("seen-slot-" + strconv.Itoa(slot)))
	}
	// Slot 0, then slot 1, then slot 0 again.
	for i, slot := range []int{0, 1, 0} {
		if res := e.h.Run(context.Background(), job(slot)); res.Err != nil {
			t.Fatalf("slot %d: %v", slot, res.Err)
		}
		if i == 1 && seen(1) != "" {
			t.Errorf("a job in slot 1 found %q kept by slot 0", seen(1))
		}
	}
	if got := seen(0); got != "kept-by-slot-0" {
		t.Errorf("the second job in slot 0 found %q, want what the first one kept", got)
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

// What Renovate creates in the scratch directory can be removed by the worker, which is a
// different user in the same group: the child runs with a group-writable umask.
func TestScratchDirectoryHoldingWhatRenovateCreatedIsRemoved(t *testing.T) {
	e := newEnv(t, `
scratch=$(dirname "$RENOVATE_REPORT_PATH")
mkdir -p "$scratch/work/repo-1/.git/objects"
touch "$scratch/work/repo-1/file"
ls -ld "$scratch/work" | cut -c1-10 > "$OUT/mode"`)
	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := strings.TrimSpace(e.read("mode")); got[5] != 'w' {
		t.Errorf("directories Renovate creates are %q: the group cannot write", got)
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

// A repository cannot change what the handler passes: the handler never reads the
// repository's own config, and allowedCommands and allowScripts come from the admin's global
// config alone, with allowScripts fixed to false.
func TestRepositoryConfigCannotSetAllowedCommandsOrAllowScripts(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		// The repository's own config, as a hostile repository would write it.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"type": "file", "encoding": "base64", "name": "renovate.json",
			"content": base64.StdEncoding.EncodeToString([]byte(`{"allowedCommands":[".*"],"allowScripts":true}`)),
		})
	}))
	defer gh.Close()

	e := newEnv(t, `
env > "$OUT/env"
cp "$RENOVATE_CONFIG_FILE" "$OUT/config"`, func(o *renovate.Options) { o.GitHubAPIURL = gh.URL })
	global := `{"platform":"gitlab","token":"x","repositories":["evil/repo"],"allowScripts":true,"allowedCommands":["^/opt/renovate-tools/hello\\.sh$"],"onboarding":false}`
	if err := os.WriteFile(filepath.Join(e.cfgMount, "config.json"), []byte(global), 0o644); err != nil {
		t.Fatal(err)
	}

	if res := e.h.Run(context.Background(), job(0)); res.Err != nil {
		t.Fatalf("Run: %v", res.Err)
	}

	mu.Lock()
	if len(requested) != 0 {
		t.Errorf("the handler read from the GitHub API: %v", requested)
	}
	mu.Unlock()

	env := e.read("env")
	if !strings.Contains(env, "RENOVATE_ALLOW_SCRIPTS=false\n") || strings.Contains(env, "RENOVATE_ALLOWED_COMMANDS") {
		t.Errorf("env = %s", env)
	}
	var handed map[string]any
	if err := json.Unmarshal([]byte(e.read("config")), &handed); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"platform", "token", "repositories", "allowScripts"} {
		if _, ok := handed[key]; ok {
			t.Errorf("the config handed to Renovate still sets %s", key)
		}
	}
	if cmds, _ := handed["allowedCommands"].([]any); len(cmds) != 1 || cmds[0] != `^/opt/renovate-tools/hello\.sh$` {
		t.Errorf("allowedCommands = %v, want the admin's list unchanged", handed["allowedCommands"])
	}
	if !strings.Contains(e.logs.String(), "key=allowScripts") || strings.Contains(e.logs.String(), "evil/repo") {
		t.Errorf("the warning names the keys and no value:\n%s", e.logs.String())
	}
}

type fakeContents struct {
	files map[string]string
	mu    sync.Mutex
	auth  []string
	refs  []string
	fail  int
}

func (f *fakeContents) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	f.refs = append(f.refs, r.URL.Query().Get("ref"))
	fail := f.fail
	f.mu.Unlock()
	if fail != 0 {
		http.Error(w, "nope", fail)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/repos/acme/global/contents/")
	if content, ok := f.files[p]; ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "size": len(content), "content": base64.StdEncoding.EncodeToString([]byte(content))})
		return
	}
	seen := map[string]bool{}
	var list []map[string]any
	for fp := range f.files {
		rest, ok := strings.CutPrefix(fp, p+"/")
		if !ok {
			continue
		}
		name, _, nested := strings.Cut(rest, "/")
		if seen[name] {
			continue
		}
		seen[name] = true
		typ := "file"
		if nested {
			typ = "dir"
		}
		list = append(list, map[string]any{"name": name, "path": p + "/" + name, "type": typ})
	}
	if len(list) == 0 {
		http.NotFound(w, r)
		return
	}
	_ = json.NewEncoder(w).Encode(list)
}

func fetchEnv(t *testing.T, script string, gh *fakeContents) (*env, queue.ClaimedJob) {
	t.Helper()
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	e := newEnv(t, script, func(o *renovate.Options) { o.GitHubAPIURL = srv.URL })
	if err := os.Remove(filepath.Join(e.cfgMount, "config.json")); err != nil {
		t.Fatal(err)
	}
	j := job(0)
	j.GlobalConfigRepo, j.GlobalConfigRef = "acme/global", "main"
	return e, j
}

func TestGlobalConfigAndToolsAreFetchedWithTheJobToken(t *testing.T) {
	gh := &fakeContents{files: map[string]string{
		"renovate-global/config.json":    `{"onboarding": false}`,
		"renovate-global/tools/hello.sh": "#!/bin/sh\necho hello\n",
	}}
	e, j := fetchEnv(t, `
cp "$RENOVATE_CONFIG_FILE" "$OUT/config"
echo "$PATH" > "$OUT/path"
hello.sh > "$OUT/tool"`, gh)

	res := e.h.Run(context.Background(), j)
	if res.Err != nil {
		t.Fatalf("Run: %v\n%+v", res.Err, res.Run)
	}
	if got := e.read("config"); got != `{"onboarding": false}` {
		t.Errorf("config = %q", got)
	}
	if got := strings.TrimSpace(e.read("tool")); got != "hello" {
		t.Errorf("the fetched tool printed %q: it is not runnable from PATH", got)
	}
	if !strings.Contains(e.read("path"), "tools") {
		t.Errorf("PATH = %q", e.read("path"))
	}
	gh.mu.Lock()
	defer gh.mu.Unlock()
	for i, a := range gh.auth {
		if a != "Bearer "+jobToken || gh.refs[i] != "main" {
			t.Errorf("request %d: Authorization %q, ref %q", i, a, gh.refs[i])
		}
	}
	if len(gh.auth) == 0 {
		t.Error("nothing was fetched")
	}
	if left := e.jobDirs(); len(left) != 0 {
		t.Errorf("scratch directories left behind: %v", left)
	}
}

func TestAMountOverridesTheFetch(t *testing.T) {
	gh := &fakeContents{files: map[string]string{"renovate-global/config.json": `{"from":"repository"}`}}
	e, j := fetchEnv(t, `cp "$RENOVATE_CONFIG_FILE" "$OUT/config"`, gh)
	if err := os.WriteFile(filepath.Join(e.cfgMount, "config.json"), []byte(`{"from":"mount"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := e.h.Run(context.Background(), j); res.Err != nil {
		t.Fatal(res.Err)
	}
	if got := e.read("config"); got != `{"from":"mount"}` {
		t.Errorf("config = %q", got)
	}
	if len(gh.auth) != 0 {
		t.Errorf("the repository was fetched with a mount present: %v", gh.auth)
	}
}

func TestGlobalConfigFailuresFailTheJobBeforeRenovateStarts(t *testing.T) {
	for name, tc := range map[string]struct {
		gh      *fakeContents
		mutate  func(*queue.ClaimedJob)
		message string
	}{
		"the fetch is refused":    {&fakeContents{fail: 403}, func(*queue.ClaimedJob) {}, "could not be fetched"},
		"the repository has none": {&fakeContents{files: map[string]string{"renovate-global/readme.md": "x"}}, func(*queue.ClaimedJob) {}, "has no global Renovate config"},
		"nothing is configured":   {&fakeContents{}, func(j *queue.ClaimedJob) { j.GlobalConfigRepo = "" }, "none is configured to fetch"},
	} {
		t.Run(name, func(t *testing.T) {
			e, j := fetchEnv(t, `touch "$OUT/started"`, tc.gh)
			tc.mutate(&j)
			res := e.h.Run(context.Background(), j)
			wantFailure(t, res, renovate.CodeGlobalConfig)
			if !strings.Contains(res.Err.Error(), tc.message) {
				t.Errorf("reason = %q, want it to contain %q", res.Err, tc.message)
			}
			secretFree(t, e, res)
			if e.exists("started") || len(e.jobDirs()) != 0 {
				t.Error("Renovate started, or a scratch directory was left, after a failed global config")
			}
		})
	}
}

func TestInvalidJobsAreRefusedBeforeAnythingIsRequested(t *testing.T) {
	for name, mutate := range map[string]func(*queue.ClaimedJob){
		"a repository that is not owner/repo": func(j *queue.ClaimedJob) { j.RepositoryFullName = "acme/repo-1,evil/other" },
		"a negative slot":                     func(j *queue.ClaimedJob) { j.Slot = -1 },
		"a git author with a newline":         func(j *queue.ClaimedJob) { j.GitAuthor = "a <b@c>\nRENOVATE_TOKEN=x" },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, `touch "$OUT/started"`)
			j := job(0)
			mutate(&j)
			wantFailure(t, e.h.Run(context.Background(), j), renovate.CodeInvalidJob)
			if len(e.tokens.calls) != 0 || e.exists("started") {
				t.Error("a token was requested or Renovate ran for an invalid job")
			}
		})
	}
}

// Renovate runs as the second Unix user. Unprivileged, the switch is refused, so the child
// never runs as the worker: nothing runs. As root the child's uid is the configured one and it
// cannot read a root-only file standing in for the worker's secret.
func TestRenovateIsStartedAsTheSecondUser(t *testing.T) {
	other := uint32(os.Getuid() + 1)
	if os.Geteuid() == 0 {
		other = 65534
	}
	secretFile := filepath.Join(t.TempDir(), "worker-secret")
	if err := os.WriteFile(secretFile, []byte(workerSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t, `id -u > "$OUT/uid"; cat `+secretFile+` > "$OUT/secret" 2> "$OUT/cat-error"; touch "$OUT/started"`,
		func(o *renovate.Options) {
			o.SameUser = false
			o.User = func() (renovate.User, error) {
				return renovate.User{Name: "renovate", UID: other, GID: uint32(os.Getgid()), Home: "/nonexistent"}, nil
			}
		})
	// Let the unprivileged user be anyone: the directories are world-accessible for this test.
	for _, d := range []string{e.cacheRoot, e.cfgMount, e.out, filepath.Dir(e.out)} {
		_ = os.Chmod(d, 0o777)
	}

	res := e.h.Run(context.Background(), job(0))
	if os.Geteuid() != 0 {
		wantFailure(t, res, renovate.CodeStartFailed)
		if e.exists("started") {
			t.Fatal("Renovate ran although the switch to the second user was refused")
		}
		return
	}
	if res.Err != nil {
		t.Fatalf("Run: %v", res.Err)
	}
	if got := strings.TrimSpace(e.read("uid")); got != strconv.Itoa(int(other)) || got == strconv.Itoa(os.Getuid()) {
		t.Errorf("the child ran as uid %s, the worker as %d", got, os.Getuid())
	}
	if e.read("secret") != "" || e.read("cat-error") == "" {
		t.Error("the child read the worker's secret file")
	}
}

func TestTheSecondUserIsRequiredAndNeverTheWorkersOwn(t *testing.T) {
	if _, err := renovate.NewHandler(renovate.Options{Tokens: &stubTokens{}, GitHubAPIURL: "https://api.github.com"}); err == nil {
		t.Error("a handler without the second user was built")
	}
	for name, u := range map[string]func() (renovate.User, error){
		"the user lookup fails": func() (renovate.User, error) { return renovate.User{}, errors.New("no such user") },
		"the user is the worker": func() (renovate.User, error) {
			return renovate.User{Name: "renovate", UID: uint32(os.Getuid())}, nil
		},
		"the user has no name": func() (renovate.User, error) { return renovate.User{UID: uint32(os.Getuid() + 1)}, nil },
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, `touch "$OUT/started"`, func(o *renovate.Options) { o.SameUser = false; o.User = u })
			wantFailure(t, e.h.Run(context.Background(), job(0)), renovate.CodeUserUnavailable)
			if len(e.tokens.calls) != 0 || e.exists("started") {
				t.Error("a token was requested or Renovate ran without a second user")
			}
		})
	}
}
