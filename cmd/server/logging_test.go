package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/logging"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/queue/worker"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

// captureLogs installs the process's redacting handler over a buffer, as main does over stderr,
// for the length of the test.
func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	previous := slog.Default()
	out := &lockedBuffer{}
	logging.Install(out)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})
	return out
}

// The values the app and the worker hold or mint. None has the shape of a token or key, so a
// pass means the registry removed it.
var secretKinds = []struct {
	kind   string
	value  string
	minted bool
}{
	{"App private key", "-----BEGIN RSA PRIVATE KEY-----\nopaque-app-key-body-line-one-0001\nopaque-app-key-body-line-two-0002\n-----END RSA PRIVATE KEY-----\n", false},
	{"client secret", "opaque-client-secret-table-0001", false},
	{"webhook secret", "opaque-webhook-secret-table-0001", false},
	{"setup token", "opaque-setup-token-table-0001", false},
	{"worker secret", "opaque-worker-secret-table-0001", false},
	{"session secret", "opaque-session-secret-table-0001", false},
	{"installation token", "opaque-installation-token-table-0001", true},
	{"user token", "opaque-user-token-table-0001", true},
	{"OAuth code", "opaque-oauth-code-table-0001", true},
	{"OAuth state", "opaque-oauth-state-table-0001", true},
	{"session token", "opaque-session-token-table-0001", true},
	{"integration credentials", `{"apiKey":"opaque-integration-key-table-0001"}`, true},
}

func registerSecretKinds() {
	for _, s := range secretKinds {
		if s.minted {
			logging.RegisterFor(time.Hour, s.value)
		} else {
			logging.Register(s.value)
		}
	}
}

// leak is one form of a secret kind found in a log output.
type leak struct{ kind, form string }

// secretForms are the texts a secret kind can show up as: the value, each long line of it and,
// for a JSON document, each string in it, each also path-escaped, query-escaped and escaped
// as the inside of a JSON string, which is how a value in a JSON log line is written.
func secretForms(value string) []string {
	bases := []string{value}
	for _, line := range strings.Split(value, "\n") {
		if len(line) > 20 && !strings.HasPrefix(line, "-----") {
			bases = append(bases, line)
		}
	}
	var doc any
	if json.Unmarshal([]byte(value), &doc) == nil {
		bases = appendJSONStrings(bases, doc)
	}
	var forms []string
	for _, b := range bases {
		forms = append(forms, b, url.PathEscape(b), url.QueryEscape(b))
		if quoted, err := json.Marshal(b); err == nil {
			forms = append(forms, string(quoted[1:len(quoted)-1]))
		}
	}
	return forms
}

func appendJSONStrings(out []string, doc any) []string {
	switch v := doc.(type) {
	case string:
		out = append(out, v)
	case []any:
		for _, e := range v {
			out = appendJSONStrings(out, e)
		}
	case map[string]any:
		for _, e := range v {
			out = appendJSONStrings(out, e)
		}
	}
	return out
}

// leakedForms lists every form of every secret kind that out contains. Whatever else out
// holds, a redaction marker included, does not hide one.
func leakedForms(out string) []leak {
	var leaks []leak
	for _, s := range secretKinds {
		for _, f := range secretForms(s.value) {
			if strings.Contains(out, f) {
				leaks = append(leaks, leak{s.kind, f})
			}
		}
	}
	return leaks
}

func assertNoSecretKind(t *testing.T, path, out string) {
	t.Helper()
	if out == "" {
		t.Fatalf("%s: nothing was logged", path)
	}
	for _, l := range leakedForms(out) {
		t.Errorf("%s: the %s (%.24q...) is in the log:\n%s", path, l.kind, l.form, out)
	}
	if !strings.Contains(out, logging.Placeholder) {
		t.Errorf("%s: the log holds no redaction marker:\n%s", path, out)
	}
}

func TestLeakedFormsFindsASecretNextToARedactionMarker(t *testing.T) {
	jsonEscaped := func(v string) string {
		quoted, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(quoted[1 : len(quoted)-1])
	}
	cases := []struct {
		name, out, kind string
	}{
		{"the credential document as a JSON log line escapes it", `{"msg":"` + logging.Placeholder + ` {\"apiKey\":\"opaque-integration-key-table-0001\"}"}`, "integration credentials"},
		{"the scalar credential alone", "token " + logging.Placeholder + " opaque-integration-key-table-0001", "integration credentials"},
		{"a multi-line key escaped in a JSON string", `{"err":"` + logging.Placeholder + " " + jsonEscaped(secretKinds[0].value) + `"}`, "App private key"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			leaks := leakedForms(c.out)
			if !slices.ContainsFunc(leaks, func(l leak) bool { return l.kind == c.kind }) {
				t.Fatalf("a leak of the %s next to %q is not reported: %v", c.kind, logging.Placeholder, leaks)
			}
		})
	}
	if leaks := leakedForms("line " + logging.Placeholder + " and " + logging.Placeholder); len(leaks) != 0 {
		t.Fatalf("an output with only redaction markers reports leaks: %v", leaks)
	}
}

// Every secret kind, through the startup, request, error and worker job log paths of the real
// code, never appears in the output of the process's logger.
func TestNoSecretKindReachesTheLogOnAnyPath(t *testing.T) {
	registerSecretKinds()

	t.Run("startup", func(t *testing.T) {
		logs := captureLogs(t)
		var segments []string
		for _, s := range secretKinds {
			segments = append(segments, url.PathEscape(s.value))
		}
		// The first-start line names APP_URL; every secret is in it.
		startServer(t, &config.Config{AppURL: "https://release-ops.example.com/" + strings.Join(segments, "/")})
		out := logs.String()
		if !strings.Contains(out, "setup required: open") {
			t.Fatalf("the startup did not log the setup line:\n%s", out)
		}
		assertNoSecretKind(t, "startup", out)
	})

	t.Run("request", func(t *testing.T) {
		logs := captureLogs(t)
		c := startServer(t, &config.Config{AppURL: "https://release-ops.example.com"})
		for _, s := range secretKinds {
			c.do(http.MethodGet, "/"+url.PathEscape(s.value), nil)
		}
		out := logs.String()
		if !strings.Contains(out, `msg=request`) || !strings.Contains(out, "status=404") {
			t.Fatalf("the requests were not logged:\n%s", out)
		}
		assertNoSecretKind(t, "request", out)
	})

	t.Run("error", func(t *testing.T) {
		logs := captureLogs(t)
		for _, s := range secretKinds {
			slog.Error("github sync failed", "err", fmt.Errorf("sync: %w", errors.Join(errors.New("github said"), fmt.Errorf("body %q", s.value))))
			slog.Error("scheduler failed", "err", fmt.Errorf("%s", s.value))
		}
		assertNoSecretKind(t, "error", logs.String())
	})

	t.Run("worker job", func(t *testing.T) {
		logs := captureLogs(t)
		app := newAppSide(t)
		id := app.enqueue(t)
		ctx, stop := context.WithCancel(context.Background())
		done := make(chan struct{})
		var ran atomic.Bool
		go func() {
			defer close(done)
			_ = runWorker(ctx, &config.WorkerConfig{InternalURL: app.url, WorkerSecret: testWorkerSecret}, worker.Registry{
				queue.KindRenovateRun: worker.HandlerFunc(func(context.Context, queue.ClaimedJob) worker.Result {
					// What a job writes through the process's logger: Renovate output, a
					// failing token request, a rejected response.
					for _, s := range secretKinds {
						slog.Info("renovate: "+s.value, "output", s.value)
						slog.Error("renovate: token request failed", "err", fmt.Errorf("app answered: %s", s.value))
					}
					ran.Store(true)
					return worker.Result{Err: errors.New("renovate failed")}
				}),
			}, worker.Options{Slots: 1, HeartbeatInterval: 10 * time.Millisecond})
		}()
		t.Cleanup(func() {
			stop()
			<-done
		})
		deadline := time.Now().Add(15 * time.Second)
		for !ran.Load() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !ran.Load() {
			t.Fatalf("the job %s did not run", id)
		}
		// The runner's own line for the failed job.
		for !strings.Contains(logs.String(), "worker: job failed") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !strings.Contains(logs.String(), "worker: job failed") {
			t.Fatalf("the worker did not log the failed job:\n%s", logs.String())
		}
		assertNoSecretKind(t, "worker job", logs.String())
	})
}

type staticCredentials struct{ pem []byte }

func (s staticCredentials) SigningKey(context.Context) (int64, []byte, error) {
	return githubtest.AppID, slices.Clone(s.pem), nil
}

// An error that wraps a GitHub API response echoing an installation token, logged at error
// level, shows the redaction marker and not the token.
func TestAnErrorWrappingAGitHubResponseThatEchoesAnInstallationTokenIsRedacted(t *testing.T) {
	logs := captureLogs(t)
	clock := githubtest.NewClock(time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	fake := githubtest.New(t, clock.Now)
	fake.AddInstallation(1000, githubtest.AppID)
	client := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), staticCredentials{fake.PEM}, github.AppClientOptions{Now: clock.Now})
	tok, err := client.Token(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}

	echo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, `{"message":"bad credentials","request":{"authorization":"token %s"},"token":"%s"}`, tok.Value(), tok.Value())
	}))
	t.Cleanup(echo.Close)
	resp, err := http.Get(echo.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	slog.Error("github sync failed", "err", fmt.Errorf("github: status %d: %s", resp.StatusCode, body))

	out := logs.String()
	if strings.Contains(out, tok.Value()) {
		t.Fatalf("the installation token is in the log:\n%s", out)
	}
	if !strings.Contains(out, logging.Placeholder) || !strings.Contains(out, "bad credentials") {
		t.Fatalf("the log lacks the marker or the rest of the message:\n%s", out)
	}
}

// A webhook delivery with a bad signature leaves no signature value in the log, and the app's
// own line for the request is still written.
func TestAWebhookWithABadSignatureLogsNoSignatureValue(t *testing.T) {
	logs := captureLogs(t)
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db := openStoreWithApp(t, dbPath)
	t.Cleanup(func() { _ = db.Close() })

	c := startServer(t, &config.Config{
		AppDBPath: dbPath, AppURL: "https://release-ops.example.com", WebhookURL: "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: "http://127.0.0.1:1",
	})
	const signature = "sha256=opaquebadsignaturevalue0001"
	req, err := http.NewRequest(http.MethodPost, c.base+"/hooks/github", strings.NewReader(`{"zen":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "00000000-0000-0000-0000-000000000001")
	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a bad signature = %d, want 401", resp.StatusCode)
	}

	// A diagnostic line that carries the request's headers, as a handler may write one, shows
	// the signature header masked.
	slog.Warn("webhook: delivery refused", "headers", req.Header)
	out := logs.String()
	if strings.Contains(out, "opaquebadsignaturevalue0001") {
		t.Fatalf("the signature value is in the log:\n%s", out)
	}
	if !strings.Contains(out, "path=/hooks/github") || !strings.Contains(out, "status=401") {
		t.Fatalf("the request was not logged:\n%s", out)
	}
}

func openStoreWithApp(t *testing.T, dbPath string) *store.DB {
	t.Helper()
	if _, err := store.Migrate(context.Background(), dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	cipher, err := crypto.NewCipherFromHex("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(db, cipher).GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: 7, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "cs", WebhookSecret: "opaque-hook-secret-for-the-bad-signature-test",
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}
	return db
}

// The credentials decrypted for each ticket creation are registered where they are decrypted.
func TestIntegrationCredentialsAreRegisteredWhereTheyAreDecrypted(t *testing.T) {
	db := storetest.OpenMigrated(t)
	cipher, err := crypto.NewCipherFromHex("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(db, cipher)
	base := "https://kaneo.example.com"
	integ, err := st.Integrations().Create(context.Background(), store.CreateIntegrationInput{
		Kind: "kaneo", Name: "Kaneo", BaseURL: &base,
		Secret: []byte(`{"api_key":"opaque-integration-api-key-0001","extra":{"token":"opaque-integration-nested-0002"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (integrationTargets{integrations: st.Integrations(), client: http.DefaultClient}).Target(context.Background(), integ.ID); err != nil {
		t.Fatalf("Target: %v", err)
	}
	for _, v := range []string{"opaque-integration-api-key-0001", "opaque-integration-nested-0002"} {
		if got := logging.Redact("line " + v); strings.Contains(got, v) {
			t.Errorf("%s is not removed from log text: %q", v, got)
		}
	}
}

// The first-start line is the one place the setup token is shown: once, in the record that
// prints it, and removed from every line after it.
func TestTheSetupTokenIsPrintedOnceAndRedactedOnEveryLaterLine(t *testing.T) {
	logs := captureLogs(t)
	c := startServer(t, &config.Config{AppURL: "https://release-ops.example.com"})

	const marker = "enter this one-time setup token: "
	out := logs.String()
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("the startup log does not print the setup token:\n%s", out)
	}
	token := strings.Trim(strings.Fields(out[i+len(marker):])[0], `"`)
	if len(token) != 64 {
		t.Fatalf("printed token %q is not 64 characters", token)
	}

	c.do(http.MethodGet, "/"+token, nil)
	c.do(http.MethodPost, "/api/v1/setup/session", map[string]string{"token": "wrong"})
	slog.Info("later line "+token, "token", token)
	slog.Error("later error", "err", fmt.Errorf("redeeming %s failed", token))
	if n := strings.Count(logs.String(), token); n != 1 {
		t.Fatalf("the setup token appears %d times in the log, want once:\n%s", n, logs.String())
	}
}

// main installs the redacting handler before anything logs: the real entry points, started as
// processes, never write a secret to stderr.
func TestMain(m *testing.M) {
	if os.Getenv("RELEASE_OPS_TEST_MAIN") == "1" {
		main()
		return
	}
	os.Exit(m.Run())
}

// runMain starts this test binary as the release-ops command with args and env and returns its
// stderr once it has printed want, after stopping it with SIGTERM.
func runMain(t *testing.T, args []string, env []string, want string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "RELEASE_OPS_TEST_MAIN=1"}, env...)
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(stderr.String(), want) && time.Now().Before(deadline) {
		select {
		case err := <-exited:
			t.Fatalf("the process ended (%v) before it logged %q:\n%s", err, want, stderr.String())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !strings.Contains(stderr.String(), want) {
		_ = cmd.Process.Kill()
		t.Logf("killed: %v", <-exited)
		t.Fatalf("the process did not log %q:\n%s", want, stderr.String())
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-exited:
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Errorf("wait: %v", err)
		}
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Logf("killed: %v", <-exited)
		t.Error("the process did not stop on SIGTERM")
	}
	return stderr.String()
}

func freePort(t *testing.T) int {
	t.Helper()
	var port int
	withFreePorts(t, 1, func(ports []int) error { port = ports[0]; return nil })
	return port
}

func TestTheWorkerCommandLogsNoSecret(t *testing.T) {
	const workerSecret = "opaque-worker-secret-for-the-process-test-0001"
	out := runMain(t, []string{"worker"}, []string{
		// The secret is in the URL's user-info as well, so the line that names the URL carries it.
		"INTERNAL_URL=http://runner:" + workerSecret + "@127.0.0.1:1",
		"WORKER_SECRET=" + workerSecret,
		fmt.Sprintf("METRICS_ADDR=127.0.0.1:%d", freePort(t)),
	}, "release-ops worker claiming jobs")
	if strings.Contains(out, workerSecret) || !strings.Contains(out, logging.Placeholder) {
		t.Fatalf("the worker's log carries the secret or no marker:\n%s", out)
	}
	if !strings.Contains(out, "level=INFO") {
		t.Fatalf("the worker did not log through slog:\n%s", out)
	}
}

func serverEnv(t *testing.T, extra ...string) []string {
	t.Helper()
	return append([]string{
		"SESSION_SECRET=opaque-session-secret-for-the-process-test-0001",
		"APP_ENCRYPTION_KEY=" + strings.Repeat("a1b2c3d4", 8),
		"APP_URL=https://release-ops.example.com",
		"APP_DB_PATH=" + filepath.Join(t.TempDir(), "app.db"),
		fmt.Sprintf("GO_INTERNAL_PORT=%d", freePort(t)),
		fmt.Sprintf("METRICS_ADDR=127.0.0.1:%d", freePort(t)),
	}, extra...)
}

func TestTheServerCommandLogsNoSecretAndPrintsTheGeneratedSetupTokenOnce(t *testing.T) {
	out := runMain(t, nil, serverEnv(t), "release-ops server listening")
	for _, secret := range []string{"opaque-session-secret-for-the-process-test-0001", strings.Repeat("a1b2c3d4", 8)} {
		if strings.Contains(out, secret) {
			t.Errorf("the server's log carries %.12q...:\n%s", secret, out)
		}
	}
	const marker = "enter this one-time setup token: "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("the server did not print the setup token:\n%s", out)
	}
	token := strings.Trim(strings.Fields(out[i+len(marker):])[0], `"`)
	if len(token) != 64 || strings.Count(out, token) != 1 {
		t.Fatalf("the setup token %q appears %d times, want once", token, strings.Count(out, token))
	}
}

func TestTheServerCommandNeverPrintsAConfiguredSetupToken(t *testing.T) {
	token := strings.Repeat("c0ffee11", 8)
	out := runMain(t, nil, serverEnv(t, "SETUP_TOKEN="+token), "release-ops server listening")
	if strings.Contains(out, token) {
		t.Fatalf("the configured setup token is in the log:\n%s", out)
	}
}

// A fatal error is logged through the handler, with the exit status 1.
func TestAFatalErrorIsLoggedThroughTheRedactingHandler(t *testing.T) {
	const secret = "opaque-secret-in-a-refused-config-0001"
	cmd := exec.Command(os.Args[0])
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "RELEASE_OPS_TEST_MAIN=1", "SESSION_SECRET=" + secret, "APP_ENCRYPTION_KEY=" + strings.Repeat("a1b2c3d4", 8)}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("the command ended with %v, want exit status 1", err)
	}
	if strings.Contains(string(out), secret) || !strings.Contains(string(out), "level=ERROR") || !strings.Contains(string(out), "APP_URL is required") {
		t.Fatalf("the fatal error was not logged through slog without the secret:\n%s", out)
	}
}
