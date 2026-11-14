package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/config"
	"github.com/mdg-labs/release-ops/internal/store/storetest"
	_ "modernc.org/sqlite"
)

func freePort(t *testing.T) int {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
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

// TestServerStartsOnV1DatabaseWithoutReadingV1Tables starts the real server entry point
// against a database migrated to the current schema, where the removed feature's tables
// still exist, and exercises the endpoints that stay.
func TestServerStartsOnV1DatabaseWithoutReadingV1Tables(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	storetest.Migrate(t, dbPath)

	// A v1 install leaves a monitored repo behind; the server must start with it present.
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO integrations (id, kind, name, base_url, encrypted_payload, created_at, updated_at)
		 VALUES ('int-v1', 'github', 'GitHub v1', NULL, '', 't', 't')`,
		`INSERT INTO ticket_projects (id, integration_id, external_project_id, name, create_config, status_mapping, created_at, updated_at)
		 VALUES ('tp-v1', 'int-v1', 'X', 'v1 project', '{}', '{}', 't', 't')`,
		`INSERT INTO monitored_repos (id, source_kind, project_path, ticket_project_id, created_at, updated_at)
		 VALUES ('repo-v1', 'github', 'org/repo', 'tp-v1', 't', 't')`,
		`INSERT INTO poll_runs (id, started_at, status) VALUES ('run-v1', 't', 'running')`,
	} {
		if _, err := seed.Exec(stmt); err != nil {
			t.Fatalf("seed v1 rows: %v", err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	port := freePort(t)
	cfg := &config.Config{
		SessionSecret:          strings.Repeat("s", 32),
		AppEncryptionKey:       "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		AppDBPath:              dbPath,
		GoInternalPort:         port,
		BootstrapAdminEmail:    "admin@example.com",
		BootstrapAdminPassword: "correct-horse-battery",
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx, cfg)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("server did not shut down")
		}
	})

	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become healthy: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	c := &apiClient{t: t, base: base, http: &http.Client{Jar: jar}}

	if code, body := c.do(http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email": "admin@example.com", "password": "correct-horse-battery",
	}); code != http.StatusOK {
		t.Fatalf("login = %d %s", code, body)
	}

	// The status endpoint reports the version and nothing from the removed tables.
	code, body := c.do(http.MethodGet, "/api/v1/status", nil)
	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil || code != http.StatusOK || len(status) != 1 || status["version"] == nil {
		t.Fatalf("status = %d %s, want 200 with only version", code, body)
	}

	// Removed endpoints are gone.
	for _, route := range []struct{ method, path string }{
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

	// An integration with a ticket project is in use; the leftover v1 rows keep their own
	// references answering 409 as well.
	if code, body := c.do(http.MethodDelete, "/api/v1/integrations/"+integration.ID, nil); code != http.StatusConflict {
		t.Fatalf("delete integration with ticket project = %d %s, want 409", code, body)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/ticket-projects/tp-v1", nil); code != http.StatusConflict {
		t.Fatalf("delete ticket project referenced by a v1 repo = %d %s, want 409", code, body)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/ticket-projects/"+project.ID, nil); code != http.StatusNoContent {
		t.Fatalf("delete ticket project = %d %s, want 204", code, body)
	}
	if code, body := c.do(http.MethodDelete, "/api/v1/integrations/"+integration.ID, nil); code != http.StatusNoContent {
		t.Fatalf("delete integration = %d %s, want 204", code, body)
	}
}
