package setup_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/setup"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
)

const (
	appURL      = "https://release-ops.example.com"
	configToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCapture) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logCapture) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	return storetest.OpenMigrated(t).Writer
}

func insertApp(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO github_app (id, app_id, slug, client_id, html_url, owner_login, owner_type, webhook_active,
		encrypted_private_key, encrypted_client_secret, encrypted_webhook_secret, key_id, created_at, updated_at)
		VALUES (1, 1, 'release-ops', 'cid', 'https://github.com/apps/release-ops', 'acme', 'Organization', 0, 'x', 'x', 'x', 'k', 't', 't')`)
	if err != nil {
		t.Fatalf("insert github_app: %v", err)
	}
}

type env struct {
	db     *sql.DB
	srv    *httptest.Server
	clock  *clock
	logs   *logCapture
	client *http.Client
}

func newEnv(t *testing.T, configured string) *env {
	t.Helper()

	db := newDB(t)
	logs := &logCapture{}
	if err := setup.EnsureToken(context.Background(), db, configured, appURL, logs.logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}

	clk := &clock{now: time.Now()}
	sm := auth.NewSessionManager(db, strings.Repeat("s", 32), false)
	h := &setup.Handlers{Sessions: sm, Queries: storedb.New(db), Now: clk.Now}

	r := chi.NewRouter()
	r.Use(sm.LoadAndSave)
	r.Get("/setup/status", h.Status)
	r.Post("/setup/session", h.CreateSession)
	r.With(h.RequireSession).Get("/setup/session", h.GetSession)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &env{db: db, srv: srv, clock: clk, logs: logs, client: newClient(t)}
}

func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func (e *env) redeem(t *testing.T, client *http.Client, token string) int {
	t.Helper()
	resp, err := client.Post(e.srv.URL+"/setup/session", "application/json", strings.NewReader(fmt.Sprintf(`{"token":%q}`, token)))
	if err != nil {
		t.Fatalf("POST /setup/session: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func (e *env) sessionStatus(t *testing.T, client *http.Client) int {
	t.Helper()
	resp, err := client.Get(e.srv.URL + "/setup/session")
	if err != nil {
		t.Fatalf("GET /setup/session: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func generatedToken(t *testing.T, logs *logCapture) string {
	t.Helper()
	joined := logs.joined()
	const marker = "setup token: "
	i := strings.Index(joined, marker)
	if i < 0 {
		t.Fatalf("log %q does not print the setup token", joined)
	}
	token := strings.TrimSpace(joined[i+len(marker):])
	if len(token) != 64 {
		t.Fatalf("printed token %q is %d characters, want 64", token, len(token))
	}
	return token
}

func TestEnsureTokenGeneratesAndStoresOnlyTheHash(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	logs := &logCapture{}
	if err := setup.EnsureToken(context.Background(), db, "", appURL, logs.logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	raw := generatedToken(t, logs)
	if !strings.Contains(logs.joined(), appURL+"/setup") {
		t.Errorf("log %q does not point at %s/setup", logs.joined(), appURL)
	}

	rows, err := db.Query(`SELECT id, kind, email, token_hash, COALESCE(new_email, ''), COALESCE(invited_by_user_id, ''), expires_at, COALESCE(used_at, ''), created_at FROM auth_tokens`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		n++
		cols := make([]string, 9)
		ptrs := make([]any, 9)
		for i := range cols {
			ptrs[i] = &cols[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if cols[1] != "setup" || cols[3] != auth.HashToken(raw) {
			t.Errorf("row kind %q hash %q, want a setup row holding the token's SHA-256", cols[1], cols[3])
		}
		for _, c := range cols {
			if strings.Contains(c, raw) {
				t.Errorf("a stored column contains the raw token: %q", c)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if n != 1 {
		t.Fatalf("auth_tokens has %d rows, want 1", n)
	}
}

func TestEnsureTokenUsesConfiguredTokenWithoutPrintingIt(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	logs := &logCapture{}
	if err := setup.EnsureToken(context.Background(), db, configToken, appURL, logs.logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if strings.Contains(logs.joined(), configToken) {
		t.Errorf("log %q contains the SETUP_TOKEN value", logs.joined())
	}
	if !strings.Contains(logs.joined(), "SETUP_TOKEN") {
		t.Errorf("log %q does not say the token comes from SETUP_TOKEN", logs.joined())
	}
	row, err := storedb.New(db).GetUnusedSetupToken(context.Background())
	if err != nil {
		t.Fatalf("GetUnusedSetupToken: %v", err)
	}
	if row.TokenHash != auth.HashToken(configToken) {
		t.Error("stored hash is not the hash of SETUP_TOKEN")
	}
}

func TestEnsureTokenReplacesThePreviousTokenOnRestart(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	first := generatedToken(t, e.logs)

	second := &logCapture{}
	if err := setup.EnsureToken(context.Background(), e.db, "", appURL, second.logf); err != nil {
		t.Fatalf("second EnsureToken: %v", err)
	}
	if e.redeem(t, e.client, first) != http.StatusUnauthorized {
		t.Fatal("the token of the previous start still redeems after a restart")
	}
	if got := e.redeem(t, e.client, generatedToken(t, second)); got != http.StatusNoContent {
		t.Fatalf("redeeming the restarted token = %d, want 204", got)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM auth_tokens WHERE kind = 'setup'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("setup rows = %d, %v; want 1", n, err)
	}
}

func TestEnsureTokenDoesNothingOnceAnAppExists(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	insertApp(t, db)
	logs := &logCapture{}
	if err := setup.EnsureToken(context.Background(), db, configToken, appURL, logs.logf); err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_tokens`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("auth_tokens rows = %d, %v; want none", n, err)
	}
	if logs.joined() != "" {
		t.Errorf("logged %q with an App configured", logs.joined())
	}
}

func TestCorrectTokenOpensASetupSessionAndStatusReportsSetupRequired(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	raw := generatedToken(t, e.logs)

	resp, err := e.client.Get(e.srv.URL + "/setup/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if got := e.redeem(t, e.client, raw); got != http.StatusNoContent {
		t.Fatalf("redeem = %d, want 204", got)
	}
	if got := e.sessionStatus(t, e.client); got != http.StatusOK {
		t.Fatalf("GET /setup/session with the setup session = %d, want 200", got)
	}
}

func TestWrongTokenIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	raw := generatedToken(t, e.logs)

	for _, wrong := range []string{"", "nope", raw + "x", strings.ToUpper(raw)} {
		if got := e.redeem(t, e.client, wrong); got != http.StatusUnauthorized {
			t.Errorf("redeem(%q) = %d, want 401", wrong, got)
		}
	}
	if got := e.sessionStatus(t, e.client); got != http.StatusUnauthorized {
		t.Errorf("session after wrong tokens = %d, want 401", got)
	}
	if got := e.redeem(t, e.client, raw); got != http.StatusNoContent {
		t.Errorf("the right token after wrong ones = %d, want 204", got)
	}
}

func TestReusedTokenIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	raw := generatedToken(t, e.logs)

	if got := e.redeem(t, e.client, raw); got != http.StatusNoContent {
		t.Fatalf("first redeem = %d, want 204", got)
	}
	other := newClient(t)
	if got := e.redeem(t, other, raw); got != http.StatusUnauthorized {
		t.Fatalf("second redeem = %d, want 401", got)
	}
	if got := e.sessionStatus(t, other); got != http.StatusUnauthorized {
		t.Fatalf("session of the refused redeem = %d, want 401", got)
	}
}

func TestConcurrentRedemptionsOpenExactlyOneSession(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	raw := generatedToken(t, e.logs)

	const attempts = 8
	results := make(chan int, attempts)
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			results <- e.redeem(t, newClient(t), raw)
		})
	}
	wg.Wait()
	close(results)

	opened := 0
	for code := range results {
		if code == http.StatusNoContent {
			opened++
		}
	}
	if opened != 1 {
		t.Fatalf("%d concurrent redemptions succeeded, want exactly 1", opened)
	}
}

func TestSetupRouteWithoutASessionIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	if got := e.sessionStatus(t, e.client); got != http.StatusUnauthorized {
		t.Fatalf("GET /setup/session without a session = %d, want 401", got)
	}
}

func TestSetupSessionExpires(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	if got := e.redeem(t, e.client, generatedToken(t, e.logs)); got != http.StatusNoContent {
		t.Fatalf("redeem = %d, want 204", got)
	}

	e.clock.advance(setup.SessionTTL - time.Second)
	if got := e.sessionStatus(t, e.client); got != http.StatusOK {
		t.Fatalf("session just before expiry = %d, want 200", got)
	}
	e.clock.advance(2 * time.Second)
	if got := e.sessionStatus(t, e.client); got != http.StatusUnauthorized {
		t.Fatalf("session after expiry = %d, want 401", got)
	}
	e.clock.advance(-time.Hour)
	if got := e.sessionStatus(t, e.client); got != http.StatusUnauthorized {
		t.Fatalf("expired session reused = %d, want 401: it must be destroyed, not just refused", got)
	}
}

func TestSetupRoutesAnswer404OnceAnAppExists(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	raw := generatedToken(t, e.logs)
	if got := e.redeem(t, e.client, raw); got != http.StatusNoContent {
		t.Fatalf("redeem = %d, want 204", got)
	}

	insertApp(t, e.db)

	if got := e.sessionStatus(t, e.client); got != http.StatusNotFound {
		t.Errorf("GET /setup/session with a live setup session = %d, want 404", got)
	}
	if got := e.redeem(t, newClient(t), raw); got != http.StatusNotFound {
		t.Errorf("POST /setup/session = %d, want 404", got)
	}
	resp, err := e.client.Get(e.srv.URL + "/setup/status")
	if err != nil {
		t.Fatalf("GET status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := make([]byte, 64)
	n, _ := resp.Body.Read(body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body[:n]), `"setupRequired":false`) {
		t.Errorf("status = %d %q, want 200 with setupRequired false", resp.StatusCode, body[:n])
	}
}

func TestMalformedBodyIsRefused(t *testing.T) {
	t.Parallel()

	e := newEnv(t, "")
	for _, body := range []string{"", "not json", `{"token":`} {
		resp, err := e.client.Post(e.srv.URL+"/setup/session", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q = %d, want 400", body, resp.StatusCode)
		}
	}
}
