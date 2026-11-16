package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/crypto"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/migrations"
	sqlitemigrate "github.com/mdg-labs/sqlite-migrate"
	_ "modernc.org/sqlite"
)

// bindAttempts bounds how often a server start is retried on fresh ports after one of its
// listeners finds its address taken.
const bindAttempts = 5

// withFreePorts calls start with n distinct loopback ports. The server's listeners bind
// addresses, so a port picked here can be taken by another process before the server binds
// it; start returns the server's error for that case and is called again with fresh ports,
// up to bindAttempts times. Any other error fails the test.
func withFreePorts(t *testing.T, n int, start func(ports []int) error) {
	t.Helper()

	var err error
	for attempt := 1; attempt <= bindAttempts; attempt++ {
		held := make([]net.Listener, 0, n)
		ports := make([]int, 0, n)
		for range n {
			l, lerr := net.Listen("tcp", "127.0.0.1:0")
			if lerr != nil {
				t.Fatalf("listen: %v", lerr)
			}
			held = append(held, l)
			ports = append(ports, l.Addr().(*net.TCPAddr).Port)
		}
		for _, l := range held {
			_ = l.Close()
		}

		if err = start(ports); err == nil {
			return
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			t.Fatalf("start server: %v", err)
		}
		t.Logf("attempt %d: %v; retrying on fresh ports", attempt, err)
	}
	t.Fatalf("start server: no free ports after %d attempts: %v", bindAttempts, err)
}

// probe is a request that only the server under test answers as wanted.
type probe struct {
	url    string
	status int
	bearer bool
}

// awaitReady polls every probe until it answers its status. It returns the server's own
// error as soon as done yields one: a server that stops before it is ready has failed to
// start, for example on a port that was taken.
func awaitReady(done <-chan error, probes ...probe) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(20 * time.Second)
	for _, p := range probes {
		for {
			select {
			case err := <-done:
				if err == nil {
					err = errors.New("the server stopped before it was ready")
				}
				return err
			default:
			}
			req, err := http.NewRequest(http.MethodGet, p.url, nil)
			if err != nil {
				return err
			}
			if p.bearer {
				req.Header.Set("Authorization", "Bearer "+testWorkerSecret)
			}
			var last error
			resp, err := client.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == p.status {
					break
				}
				last = fmt.Errorf("status %d", resp.StatusCode)
			} else {
				last = err
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("%s did not answer %d: %w", p.url, p.status, last)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	return nil
}

// launch runs serve in the background until the test ends and waits until every probe is
// answered. It returns serve's error if it stops first. A failed start leaves nothing running.
func launch(t *testing.T, serve func(ctx context.Context) error, probes ...probe) error {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx) }()
	if err := awaitReady(done, probes...); err != nil {
		cancel()
		return err
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("run: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Error("server did not shut down")
		}
	})
	return nil
}

// A listener that loses its port is reported by launch as an address-in-use error and
// withFreePorts starts the server again on fresh ports.
func TestLaunchReportsATakenPortAndWithFreePortsRetriesIt(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	attempts := 0
	var firstPorts []int
	withFreePorts(t, 2, func(ports []int) error {
		attempts++
		if attempts == 1 {
			firstPorts = ports
			cfg := &config.Config{InternalAddr: taken.Addr().String(), WorkerSecret: testWorkerSecret}
			err := launch(t, func(ctx context.Context) error { return serveInternal(ctx, cfg, nil, nil) },
				probe{url: "http://" + taken.Addr().String() + "/internal/v1/jobs/x/heartbeat", status: http.StatusMethodNotAllowed, bearer: true})
			if !errors.Is(err, syscall.EADDRINUSE) {
				t.Fatalf("launch on a taken port = %v, want address already in use", err)
			}
			return err
		}
		if len(ports) != 2 || ports[0] == ports[1] {
			t.Errorf("ports = %v, want two distinct ports", ports)
		}
		return nil
	})
	if attempts != 2 || len(firstPorts) != 2 {
		t.Errorf("attempts = %d, want the taken port retried once", attempts)
	}
}

type apiClient struct {
	t    *testing.T
	base string
	http *http.Client
}

func (c *apiClient) do(method, path string, body any) (int, []byte) {
	c.t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read %s %s: %v", method, path, err)
	}
	return resp.StatusCode, out
}

// TestServerStartsOnFreshDatabase starts the real server entry point against an empty data
// directory: startup applies every migration to it and the server serves the endpoints
// that exist in v2.
func TestServerStartsOnFreshDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")

	// Only the v2 environment: no SMTP and no bootstrap password.
	cfg := &config.Config{
		SessionSecret:    strings.Repeat("s", 32),
		AppEncryptionKey: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:        dbPath,
		AppURL:           "https://release-ops.example.com",
	}
	var base string
	withFreePorts(t, 1, func(ports []int) error {
		cfg.GoInternalPort = ports[0]
		base = fmt.Sprintf("http://127.0.0.1:%d", ports[0])
		return launch(t, func(ctx context.Context) error { return run(ctx, cfg) }, probe{url: base + "/healthz", status: http.StatusOK})
	})

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &apiClient{t: t, base: base, http: &http.Client{Jar: jar}}

	signInAsAdmin(t, cfg, c)

	// The migrations ran: the v2 tables exist and the v1 tables do not.
	verify, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = verify.Close() })
	for table, want := range map[string]int{"github_app": 1, "installations": 1, "jobs": 1, "update_prs": 1, "monitored_repos": 0, "poll_runs": 0} {
		var n int
		if err := verify.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil || n != want {
			t.Fatalf("table %q present = %d, %v; want %d", table, n, err, want)
		}
	}

	// The status endpoint reports the version and nothing from the removed tables.
	code, body := c.do(http.MethodGet, "/api/v1/status", nil)
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil || code != http.StatusOK || len(status) != 1 || status["version"] == nil {
		t.Fatalf("status = %d %s, want 200 with only version", code, body)
	}

	// Removed endpoints are gone: the v1 local-account routes and the release-monitoring ones.
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/auth/login"},
		{http.MethodPost, "/api/v1/auth/accept-invitation"},
		{http.MethodPost, "/api/v1/auth/forgot-password"},
		{http.MethodPost, "/api/v1/auth/reset-password"},
		{http.MethodPost, "/api/v1/auth/confirm-email-change"},
		{http.MethodDelete, "/api/v1/users/someone"},
		{http.MethodPost, "/api/v1/users/me/password-change"},
		{http.MethodPost, "/api/v1/users/me/email-change-request"},
		{http.MethodGet, "/api/v1/users/invitations"},
		{http.MethodPost, "/api/v1/users/invitations"},
		{http.MethodDelete, "/api/v1/users/invitations/someone"},
		{http.MethodPost, "/api/v1/users/invitations/someone/send-email"},
		{http.MethodGet, "/api/v1/repos"},
		{http.MethodGet, "/api/v1/notification-targets"},
		{http.MethodGet, "/api/v1/poll/runs"},
		{http.MethodPost, "/api/v1/poll/trigger"},
	} {
		if code, body := c.do(route.method, route.path, nil); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d %s, want 404", route.method, route.path, code, body)
		}
	}

	// A source integration kind is refused.
	if code, body := c.do(http.MethodPost, "/api/v1/integrations", map[string]string{
		"kind": "github", "name": "GitHub", "secret": `{"token":"tok"}`,
	}); code != http.StatusBadRequest {
		t.Fatalf("create github integration = %d %s, want 400", code, body)
	}

	// A Jira integration round-trips its encrypted payload: the connection test decrypts
	// the stored secret and sends it to the (fake) Jira host.
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("jira-user@example.com:jira-api-token"))
	gotAuth := make(chan string, 1)
	jira := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotAuth <- r.Header.Get("Authorization"):
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(jira.Close)

	code, body = c.do(http.MethodPost, "/api/v1/integrations", map[string]string{
		"kind": "jira", "name": "Jira", "baseUrl": jira.URL,
		"secret": `{"email":"jira-user@example.com","api_token":"jira-api-token"}`,
	})
	var integration struct {
		ID        string `json:"id"`
		HasSecret bool   `json:"hasSecret"`
	}
	if err := json.Unmarshal(body, &integration); err != nil || code != http.StatusCreated || integration.ID == "" || !integration.HasSecret {
		t.Fatalf("create jira integration = %d %s", code, body)
	}
	if strings.Contains(string(body), "jira-api-token") {
		t.Fatalf("create response leaks the secret: %s", body)
	}

	code, body = c.do(http.MethodPost, "/api/v1/integrations/"+integration.ID+"/test", nil)
	if code != http.StatusOK || !strings.Contains(string(body), `"success":true`) {
		t.Fatalf("test jira integration = %d %s", code, body)
	}
	if got := <-gotAuth; got != wantAuth {
		t.Fatalf("Jira host received Authorization %q, want the decrypted stored secret", got)
	}

	if code, body := c.do(http.MethodGet, "/api/v1/integrations", nil); code != http.StatusOK || !strings.Contains(string(body), `"name":"Jira"`) {
		t.Fatalf("list integrations = %d %s", code, body)
	}

	// Ticket-project CRUD.
	code, body = c.do(http.MethodPost, "/api/v1/ticket-projects", map[string]any{
		"integrationId":     integration.ID,
		"externalProjectId": "DEV",
		"name":              "Jira DEV",
		"createConfig":      json.RawMessage(`{"issueType":"Task"}`),
		"statusMapping":     json.RawMessage(`{"open":["To Do"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`),
		"contentTemplates":  json.RawMessage(`{"title":"","description":"","supersedeComment":""}`),
	})
	var project struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &project); err != nil || code != http.StatusCreated || project.ID == "" {
		t.Fatalf("create ticket project = %d %s", code, body)
	}
	if code, body := c.do(http.MethodPatch, "/api/v1/ticket-projects/"+project.ID, map[string]any{
		"name":               "Jira DEV renamed",
		"createConfig":       json.RawMessage(`{"issueType":"Task"}`),
		"statusMapping":      json.RawMessage(`{"open":["To Do"],"done":["Done"],"cancelled":["Cancelled"],"superseded":"Cancelled"}`),
		"onOpenTicketPolicy": "supersede",
	}); code != http.StatusOK {
		t.Fatalf("patch ticket project = %d %s", code, body)
	}
	if code, body := c.do(http.MethodGet, "/api/v1/ticket-projects", nil); code != http.StatusOK || !strings.Contains(string(body), "Jira DEV renamed") {
		t.Fatalf("list ticket projects = %d %s", code, body)
	}

	// An integration with a ticket project is in use, and so is a ticket project that an
	// update ticket was created in.
	if code, body := c.do(http.MethodDelete, "/api/v1/integrations/"+integration.ID, nil); code != http.StatusConflict {
		t.Fatalf("delete integration with ticket project = %d %s, want 409", code, body)
	}
	for _, stmt := range []string{
		`INSERT INTO installations (id, github_installation_id, account_id, account_login, account_type, status, repository_selection, created_at, updated_at)
		 VALUES ('inst-1', 1, 1, 'acme', 'Organization', 'active', 'all', 't', 't')`,
		`INSERT INTO repositories (id, installation_id, github_repository_id, full_name, default_branch, created_at, updated_at)
		 VALUES ('repo-1', 'inst-1', 1, 'acme/api', 'main', 't', 't')`,
		`INSERT INTO update_prs (id, repository_id, number, node_id, url, title, head_ref, state, last_seen_at, created_at, updated_at)
		 VALUES ('pr-1', 'repo-1', 1, 'node', 'https://example.test/pr/1', 'Update dep', 'renovate/dep', 'open', 't', 't', 't')`,
	} {
		if _, err := verify.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	if _, err := verify.Exec(`INSERT INTO update_tickets (id, update_pr_id, ticket_project_id, state, created_at, updated_at)
		VALUES ('ut-1', 'pr-1', ?, 'pending', 't', 't')`, project.ID); err != nil {
		t.Fatalf("seed update ticket: %v", err)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/ticket-projects/"+project.ID, nil); code != http.StatusConflict {
		t.Fatalf("delete ticket project used by an update ticket = %d %s, want 409", code, body)
	}
	if _, err := verify.Exec(`DELETE FROM update_tickets WHERE id = 'ut-1'`); err != nil {
		t.Fatalf("delete update ticket: %v", err)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/ticket-projects/"+project.ID, nil); code != http.StatusNoContent {
		t.Fatalf("delete ticket project = %d %s, want 204", code, body)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/integrations/"+integration.ID, nil); code != http.StatusNoContent {
		t.Fatalf("delete integration = %d %s, want 204", code, body)
	}
}

// TestServerRefusesV1Database starts the server entry point against a database a v1 install
// left behind. Startup fails with the fresh-volume message and leaves the data directory
// exactly as it was: no migration applied, no snapshot written.
func TestServerRefusesV1Database(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")

	all, err := sqlitemigrate.LoadDir(ctx, migrations.FS, ".")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	var v1 []sqlitemigrate.Migration
	for _, m := range all {
		if m.Version <= "20261008041322" {
			v1 = append(v1, m)
		}
	}
	runner := &sqlitemigrate.Runner{DBPath: dbPath}
	if _, err := runner.Apply(ctx, v1); err != nil {
		t.Fatalf("apply v1 migrations: %v", err)
	}
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open v1 db: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at)
		 VALUES ('int-v1', 'github', 'GitHub v1', NULL, '', 't', 't')`,
		`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at)
		 VALUES ('tp-v1', 'int-v1', 'X', 'v1 project', '{}', '{}', 't', 't')`,
		`INSERT INTO monitored_repos (id, source_kind, project_path, ticket_project_id, created_at, updated_at)
		 VALUES ('repo-v1', 'github', 'org/repo', 'tp-v1', 't', 't')`,
	} {
		if _, err := seed.Exec(stmt); err != nil {
			t.Fatalf("seed v1 rows: %v", err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close v1 db: %v", err)
	}

	before := snapshotDir(t, filepath.Dir(dbPath))
	cfg := &config.Config{
		SessionSecret:    strings.Repeat("s", 32),
		AppEncryptionKey: "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:        dbPath,
		GoInternalPort:   0,
	}

	err = run(ctx, cfg)
	if !errors.Is(err, store.ErrV1Database) || !strings.Contains(err.Error(), "fresh data volume") {
		t.Fatalf("run = %v, want the v1 refusal telling the operator to use a fresh data volume", err)
	}
	if after := snapshotDir(t, filepath.Dir(dbPath)); after != before {
		t.Fatalf("data directory changed by the refused start:\nbefore %s\nafter  %s", before, after)
	}
}

// A database that still holds local accounts, their sessions and their one-time tokens, as an
// instance that ran before the accounts were removed has it, is upgraded by the server's start:
// the server comes up with no mail and no bootstrap password and keeps the users, identities and
// sessions that survive.
func TestServerStartUpgradesADatabaseWithLocalAccounts(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "app.db")

	all, err := sqlitemigrate.LoadDir(ctx, migrations.FS, ".")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	// Later migrations may follow remove_local_accounts: seed the database as it stood just
	// before it, and let the server's start apply the complete set.
	removeAt := -1
	for i, m := range all {
		if strings.HasSuffix(m.Slug, "remove_local_accounts") {
			removeAt = i
			break
		}
	}
	if removeAt < 0 {
		t.Fatalf("no migration with a slug ending in remove_local_accounts among %d migrations", len(all))
	}
	runner := &sqlitemigrate.Runner{DBPath: dbPath}
	if _, err := runner.Apply(ctx, all[:removeAt]); err != nil {
		t.Fatalf("apply the earlier migrations: %v", err)
	}
	seed, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, email, password_hash, created_at, updated_at) VALUES ('u-local', 'admin@example.com', 'bcrypt-hash', 't', 't')`,
		`INSERT INTO users (id, created_at, updated_at) VALUES ('u-gh', 't', 't')`,
		`INSERT INTO auth_identities (id, user_id, provider, subject, login, created_at) VALUES ('ai-1', 'u-gh', 'github', '583231', 'octocat', 't')`,
		`INSERT INTO sessions (token, data, expiry, user_id) VALUES ('tok-local', x'00', 2461000.5, 'u-local')`,
		`INSERT INTO app_settings (id, updated_at) VALUES (1, 't')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, expires_at, created_at) VALUES ('t-invite', 'invitation', 'i@example.com', 'h', '9999-12-31T23:59:59.000Z', 't')`,
		`INSERT INTO auth_tokens (id, kind, email, token_hash, expires_at, created_at) VALUES ('t-reset', 'password_reset', 'admin@example.com', 'h2', '9999-12-31T23:59:59.000Z', 't')`,
	} {
		if _, err := seed.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	c := startServer(t, &config.Config{AppDBPath: dbPath})
	if code, body := c.do(http.MethodGet, "/api/v1/auth/session", nil); code != http.StatusOK || !strings.Contains(string(body), `"user":null`) {
		t.Fatalf("session = %d %s, want 200 with no user", code, body)
	}

	check, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = check.Close() })
	for query, want := range map[string]int{
		`SELECT COUNT(*) FROM users`:                                     2,
		`SELECT COUNT(*) FROM auth_identities WHERE login = 'octocat'`:   1,
		`SELECT COUNT(*) FROM sessions WHERE token = 'tok-local'`:        1,
		`SELECT COUNT(*) FROM auth_tokens WHERE kind <> 'setup'`:         0,
		`SELECT COUNT(*) FROM sqlite_master WHERE name = 'app_settings'`: 0,
	} {
		var n int
		if err := check.QueryRow(query).Scan(&n); err != nil || n != want {
			t.Errorf("%s = %d, %v; want %d", query, n, err, want)
		}
	}
}

// snapshotDir fingerprints every file under dir: its path, size and content hash.
func snapshotDir(t *testing.T, dir string) string {
	t.Helper()

	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, fmt.Sprintf("%s:%d:%x", path, len(raw), sha256.Sum256(raw)))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return strings.Join(out, "\n")
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// signInAsAdmin pins a bootstrap admin in the database of the running server and signs the
// client in with a login link, the way an operator does with `release-ops admin login-link`.
func signInAsAdmin(t *testing.T, cfg *config.Config, c *apiClient) {
	t.Helper()

	db, err := store.OpenPath(cfg.AppDBPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cipher, err := crypto.NewCipherFromHex(cfg.AppEncryptionKey)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	if _, err := store.New(db, cipher).SignIn().SignIn(context.Background(), store.GitHubUser{ID: 583231, Login: "octocat"}, true); err != nil {
		t.Fatalf("pin the bootstrap admin: %v", err)
	}
	var out bytes.Buffer
	if err := printLoginLink(context.Background(), cfg, &out); err != nil {
		t.Fatalf("printLoginLink: %v", err)
	}
	_, link, _ := strings.Cut(out.String(), "\n")
	_, token, ok := strings.Cut(strings.TrimSpace(link), "token=")
	if !ok {
		t.Fatalf("login link output = %q, want a link with a token", out.String())
	}
	if code, body := c.do(http.MethodPost, "/api/v1/auth/login-link", map[string]string{"token": token}); code != http.StatusOK {
		t.Fatalf("redeem login link = %d %s", code, body)
	}
}

// startServer runs the server entry point against a fresh database and returns an API client
// once it answers /healthz. The server stops with the test.
func startServer(t *testing.T, cfg *config.Config) *apiClient {
	t.Helper()

	cfg.SessionSecret = strings.Repeat("s", 32)
	cfg.AppEncryptionKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	if cfg.AppDBPath == "" {
		cfg.AppDBPath = filepath.Join(t.TempDir(), "app.db")
	}

	var base string
	withFreePorts(t, 1, func(ports []int) error {
		cfg.GoInternalPort = ports[0]
		base = fmt.Sprintf("http://127.0.0.1:%d", ports[0])
		return launch(t, func(ctx context.Context) error { return run(ctx, cfg) }, probe{url: base + "/healthz", status: http.StatusOK})
	})

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &apiClient{t: t, base: base, http: &http.Client{Jar: jar}}
}

// TestServerStartupCreatesTheSetupToken starts the real server with no GitHub App: the
// generated setup token is printed once, opens a setup session through POST
// /api/v1/setup/session, cannot be redeemed twice, and the session opens no other API.
func TestServerStartupCreatesTheSetupToken(t *testing.T) {
	logs := &lockedBuffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	c := startServer(t, &config.Config{AppURL: "https://release-ops.example.com"})

	const marker = "enter this one-time setup token: "
	out := logs.String()
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("startup log does not print the setup token:\n%s", out)
	}
	token := strings.Fields(out[i+len(marker):])[0]
	if len(token) != 64 || strings.Count(out, token) != 1 {
		t.Fatalf("printed token %q (%d chars) printed %d times, want 64 characters, once", token, len(token), strings.Count(out, token))
	}
	if !strings.Contains(out, "https://release-ops.example.com/setup") {
		t.Errorf("startup log does not point at APP_URL/setup:\n%s", out)
	}

	if code, body := c.do(http.MethodGet, "/api/v1/setup/status", nil); code != http.StatusOK || !strings.Contains(string(body), `"setupRequired":true`) {
		t.Fatalf("setup status = %d %s, want setupRequired true", code, body)
	}
	if code, _ := c.do(http.MethodGet, "/api/v1/setup/session", nil); code != http.StatusUnauthorized {
		t.Fatalf("setup session before redeeming = %d, want 401", code)
	}
	if code, body := c.do(http.MethodPost, "/api/v1/setup/session", map[string]string{"token": "wrong"}); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d %s, want 401", code, body)
	}
	if code, body := c.do(http.MethodPost, "/api/v1/setup/session", map[string]string{"token": token}); code != http.StatusNoContent {
		t.Fatalf("redeem = %d %s, want 204", code, body)
	}
	if code, body := c.do(http.MethodGet, "/api/v1/setup/session", nil); code != http.StatusOK {
		t.Fatalf("setup session after redeeming = %d %s, want 200", code, body)
	}
	if code, body := c.do(http.MethodGet, "/api/v1/status", nil); code != http.StatusUnauthorized {
		t.Fatalf("protected API with a setup session = %d %s, want 401", code, body)
	}

	other := &apiClient{t: t, base: c.base, http: &http.Client{}}
	if code, body := other.do(http.MethodPost, "/api/v1/setup/session", map[string]string{"token": token}); code != http.StatusUnauthorized {
		t.Fatalf("second redeem = %d %s, want 401", code, body)
	}
}

// TestServerStartupUsesTheConfiguredSetupToken: SETUP_TOKEN replaces the generated token and
// is never printed.
func TestServerStartupUsesTheConfiguredSetupToken(t *testing.T) {
	logs := &lockedBuffer{}
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	token := strings.Repeat("ab", 32)
	c := startServer(t, &config.Config{AppURL: "https://release-ops.example.com", SetupToken: token})

	if strings.Contains(logs.String(), token) {
		t.Fatalf("startup log contains SETUP_TOKEN:\n%s", logs.String())
	}
	if code, body := c.do(http.MethodPost, "/api/v1/setup/session", map[string]string{"token": token}); code != http.StatusNoContent {
		t.Fatalf("redeem the configured token = %d %s, want 204", code, body)
	}
}

// An instance whose App was stored before its bot user's id was recorded gets the id filled
// in at start-up, from the users endpoint of the configured GitHub API, without setup.
func TestServerFillsInTheBotUserIdOfAnAppStoredWithoutIt(t *testing.T) {
	var lookups atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v3/users/release-ops-acme[bot]" || r.Header.Get("Authorization") != "" {
			http.NotFound(w, r)
			return
		}
		lookups.Add(1)
		_, _ = w.Write([]byte(`{"id":424242,"login":"release-ops-acme[bot]","type":"Bot"}`))
	}))
	t.Cleanup(github.Close)

	dbPath := filepath.Join(t.TempDir(), "app.db")
	if _, err := store.Migrate(context.Background(), dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.OpenPath(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cipher, err := crypto.NewCipherFromHex("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(db, cipher).GitHubApp().Create(context.Background(), store.NewGitHubApp{
		AppID: 7, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
		OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "cs", WebhookSecret: "ws",
	}); err != nil {
		t.Fatalf("store app: %v", err)
	}

	startServer(t, &config.Config{
		AppDBPath:     dbPath,
		AppURL:        "https://release-ops.example.com",
		WebhookURL:    "https://release-ops.example.com/hooks/github",
		GitHubBaseURL: github.URL,
	})

	waitFor(t, "the start-up lookup to record the bot user id", func() bool {
		var id sql.NullInt64
		return db.Reader.QueryRow(`SELECT bot_user_id FROM github_app`).Scan(&id) == nil && id.Valid && id.Int64 == 424242
	})
	if n := lookups.Load(); n != 1 {
		t.Errorf("GitHub was asked %d times, want once", n)
	}
}
