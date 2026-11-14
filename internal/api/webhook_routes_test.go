package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/store"
)

const webhookTestSecret = "fixture-webhook-secret-value"

type webhookFlow struct {
	srv  *httptest.Server
	deps *ServerDeps
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("hooks", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// newWebhookFlow serves the real router with a stored App whose webhook secret is
// webhookTestSecret. webhookURL empty leaves the instance without a webhook.
func newWebhookFlow(t *testing.T, webhookURL string, withApp bool, routes *hooks.Routes) *webhookFlow {
	t.Helper()

	deps := newRoutesTestDeps(t)
	deps.WebhookURL = webhookURL
	deps.WebhookRoutes = routes
	if withApp {
		if err := deps.Store.GitHubApp().Create(context.Background(), store.NewGitHubApp{
			AppID: 1234567, Slug: "release-ops-acme", ClientID: "cid", HTMLURL: "https://github.com/apps/release-ops-acme",
			OwnerLogin: "acme", OwnerType: "Organization", PrivateKey: "pem", ClientSecret: "client-secret-value", WebhookSecret: webhookTestSecret,
		}); err != nil {
			t.Fatalf("store app: %v", err)
		}
	}
	srv := httptest.NewServer(NewServerRouter(deps))
	t.Cleanup(srv.Close)
	return &webhookFlow{srv: srv, deps: deps}
}

type hookRequest struct {
	body      []byte
	signature string
	event     string
	guid      string
}

func signedHook(event, guid string, body []byte) hookRequest {
	return hookRequest{body: body, signature: sign(webhookTestSecret, body), event: event, guid: guid}
}

func (f *webhookFlow) post(t *testing.T, h hookRequest) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+hooks.Path, bytes.NewReader(h.body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if h.signature != "" {
		req.Header.Set("X-Hub-Signature-256", h.signature)
	}
	if h.event != "" {
		req.Header.Set("X-GitHub-Event", h.event)
	}
	if h.guid != "" {
		req.Header.Set("X-GitHub-Delivery", h.guid)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, string(out)
}

func (f *webhookFlow) deliveries(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.deps.DB.QueryRow(`SELECT COUNT(*) FROM webhook_deliveries`).Scan(&n); err != nil {
		t.Fatalf("count deliveries: %v", err)
	}
	return n
}

type seen struct{ calls atomic.Int64 }

func (s *seen) handler(err error) hooks.Handler {
	return func(_ context.Context, _ *sql.Tx, _ hooks.Delivery) error {
		s.calls.Add(1)
		return err
	}
}

func TestWebhookSignedInstallationReachesItsHandler(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var got hooks.Delivery
	var calls atomic.Int64
	routes.Register("installation", func(_ context.Context, _ *sql.Tx, d hooks.Delivery) error {
		calls.Add(1)
		got = d
		return nil
	})
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)
	body := fixture(t, "installation_created.json")

	status, resp := f.post(t, signedHook("installation", "guid-install-1", body))
	if status != http.StatusAccepted || resp != "" {
		t.Fatalf("status = %d body %q, want 202 and no body", status, resp)
	}
	if calls.Load() != 1 {
		t.Fatalf("installation handler ran %d times, want 1", calls.Load())
	}
	if got.GUID != "guid-install-1" || got.Event != "installation" || got.GitHubInstallationID != 1000 || !bytes.Equal(got.Payload, body) {
		t.Errorf("delivery = guid %q event %q installation %d payload equal %v", got.GUID, got.Event, got.GitHubInstallationID, bytes.Equal(got.Payload, body))
	}
	if f.deliveries(t) != 1 {
		t.Errorf("deliveries recorded = %d, want 1", f.deliveries(t))
	}
}

func TestWebhookInstallationRepositoriesIsRouted(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var s seen
	routes.Register("installation_repositories", s.handler(nil))
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)

	status, _ := f.post(t, signedHook("installation_repositories", "guid-repos-1", fixture(t, "installation_repositories_added.json")))
	if status != http.StatusAccepted || s.calls.Load() != 1 {
		t.Fatalf("status = %d, handler calls = %d, want 202 and 1", status, s.calls.Load())
	}
}

func TestWebhookDefaultRoutesAcknowledgeInstallationEvents(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	status, _ := f.post(t, signedHook("installation", "guid-default-1", fixture(t, "installation_created.json")))
	if status != http.StatusAccepted || f.deliveries(t) != 1 {
		t.Fatalf("status = %d, deliveries = %d, want 202 and 1", status, f.deliveries(t))
	}
}

func TestWebhookWrongSignaturesAreRefusedBeforeAnythingIsParsedOrStored(t *testing.T) {
	t.Parallel()

	body := fixture(t, "installation_created.json")
	modified := append(append([]byte{}, body...), ' ')
	cases := map[string]hookRequest{
		"missing signature":          {body: body, event: "installation", guid: "g1"},
		"wrong secret":               {body: body, signature: sign("another-secret", body), event: "installation", guid: "g2"},
		"signature over other body":  {body: modified, signature: sign(webhookTestSecret, body), event: "installation", guid: "g3"},
		"no sha256 prefix":           {body: body, signature: strings.TrimPrefix(sign(webhookTestSecret, body), "sha256="), event: "installation", guid: "g4"},
		"not hex":                    {body: body, signature: "sha256=zz", event: "installation", guid: "g5"},
		"truncated signature":        {body: body, signature: sign(webhookTestSecret, body)[:20], event: "installation", guid: "g6"},
		"sha1 header form":           {body: body, signature: "sha1=" + strings.TrimPrefix(sign(webhookTestSecret, body), "sha256="), event: "installation", guid: "g7"},
		"unparsable body, bad sign.": {body: []byte("{not json"), signature: "sha256=00", event: "installation", guid: "g8"},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			routes := hooks.NewRoutes()
			var s seen
			routes.Register("installation", s.handler(nil))
			f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)

			status, resp := f.post(t, h)
			if status != http.StatusUnauthorized || resp != "" {
				t.Errorf("status = %d body %q, want 401 and no body", status, resp)
			}
			if s.calls.Load() != 0 || f.deliveries(t) != 0 {
				t.Errorf("handler calls %d, deliveries %d, want none", s.calls.Load(), f.deliveries(t))
			}
		})
	}
}

func TestWebhookValidSignatureOverUnparsablePayloadIsRefusedUnrecorded(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var s seen
	routes.Register("installation", s.handler(nil))
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)

	status, _ := f.post(t, signedHook("installation", "guid-bad-json", []byte("{not json")))
	if status != http.StatusBadRequest || s.calls.Load() != 0 || f.deliveries(t) != 0 {
		t.Fatalf("status = %d, calls = %d, deliveries = %d, want 400 and nothing", status, s.calls.Load(), f.deliveries(t))
	}
}

func TestWebhookRequiresDeliveryAndEventHeaders(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	body := fixture(t, "installation_created.json")
	if status, _ := f.post(t, signedHook("installation", "", body)); status != http.StatusBadRequest {
		t.Errorf("no delivery id: status = %d, want 400", status)
	}
	if status, _ := f.post(t, signedHook("", "guid-no-event", body)); status != http.StatusBadRequest {
		t.Errorf("no event: status = %d, want 400", status)
	}
	if f.deliveries(t) != 0 {
		t.Errorf("deliveries = %d, want 0", f.deliveries(t))
	}
}

func TestWebhookOversizedBodyIsRefusedWith413(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var s seen
	routes.Register("installation", s.handler(nil))
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)

	big := bytes.Repeat([]byte("a"), hooks.MaxBodyBytes+1)
	// A correctly signed body over the cap is refused too: the cap comes first.
	status, _ := f.post(t, signedHook("installation", "guid-big", big))
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", status)
	}

	// A body without a declared length is cut off at the cap rather than buffered.
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+hooks.Path, hiddenLength{bytes.NewReader(big)})
	if err != nil {
		t.Fatal(err)
	}
	req.ContentLength = -1
	req.Header.Set("X-Hub-Signature-256", sign(webhookTestSecret, big))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-GitHub-Delivery", "guid-big-chunked")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post chunked: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("chunked status = %d, want 413", resp.StatusCode)
	}
	if s.calls.Load() != 0 || f.deliveries(t) != 0 {
		t.Errorf("handler calls %d, deliveries %d, want none", s.calls.Load(), f.deliveries(t))
	}
}

// hiddenLength hides the length of the wrapped reader so the client sends chunked.
type hiddenLength struct{ r *bytes.Reader }

func (n hiddenLength) Read(p []byte) (int, error) { return n.r.Read(p) }

func TestWebhookBodyAtTheCapIsAccepted(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	body := append([]byte(`{"pad":"`), bytes.Repeat([]byte("a"), hooks.MaxBodyBytes-len(`{"pad":""}`))...)
	body = append(body, []byte(`"}`)...)
	if len(body) != hooks.MaxBodyBytes {
		t.Fatalf("body length = %d, want %d", len(body), hooks.MaxBodyBytes)
	}
	if status, _ := f.post(t, signedHook("installation", "guid-cap", body)); status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
}

func TestWebhookDuplicateDeliveryHasNoSecondEffect(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var s seen
	routes.Register("installation", s.handler(nil))
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)
	h := signedHook("installation", "guid-dup", fixture(t, "installation_created.json"))

	for i := range 3 {
		if status, _ := f.post(t, h); status != http.StatusAccepted {
			t.Fatalf("delivery %d: status = %d, want 202", i, status)
		}
	}
	if s.calls.Load() != 1 || f.deliveries(t) != 1 {
		t.Errorf("handler calls %d, deliveries %d, want 1 each", s.calls.Load(), f.deliveries(t))
	}
}

func TestWebhookUnknownEventIsAcknowledgedAndIgnored(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	for _, event := range []string{"ping", "star", "issues", "pull_request", "push"} {
		if status, resp := f.post(t, signedHook(event, "guid-"+event, fixture(t, "ping.json"))); status != http.StatusAccepted || resp != "" {
			t.Errorf("%s: status = %d body %q, want 202 and no body", event, status, resp)
		}
	}
	if f.deliveries(t) != 0 {
		t.Errorf("deliveries %d, want none for ignored events", f.deliveries(t))
	}
}

func TestWebhookHandlerFailureRollsBackTheDeliveryRecord(t *testing.T) {
	t.Parallel()

	routes := hooks.NewRoutes()
	var fail atomic.Bool
	fail.Store(true)
	var applied atomic.Int64
	routes.Register("installation", func(ctx context.Context, tx *sql.Tx, d hooks.Delivery) error {
		// The record is already in this transaction when the handler runs.
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM webhook_deliveries WHERE guid = ?`, d.GUID).Scan(&n); err != nil || n != 1 {
			t.Errorf("record visible to the handler = %d, %v, want 1", n, err)
		}
		if fail.Load() {
			return errors.New("enqueue failed")
		}
		applied.Add(1)
		return nil
	})
	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, routes)
	h := signedHook("installation", "guid-retry", fixture(t, "installation_created.json"))

	status, resp := f.post(t, h)
	if status != http.StatusInternalServerError || resp != "" {
		t.Fatalf("status = %d body %q, want 500 and no body", status, resp)
	}
	if f.deliveries(t) != 0 {
		t.Fatalf("deliveries %d after a failed apply, want none", f.deliveries(t))
	}

	fail.Store(false)
	if status, _ := f.post(t, h); status != http.StatusAccepted {
		t.Fatalf("redelivery status = %d, want 202", status)
	}
	if applied.Load() != 1 || f.deliveries(t) != 1 {
		t.Errorf("applied %d, deliveries %d, want 1 each", applied.Load(), f.deliveries(t))
	}
}

func TestWebhookRecordsTheInstallationItBelongsTo(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	if _, err := f.deps.Store.Installations().Record(context.Background(), store.NewInstallation{
		GitHubInstallationID: 1000, AccountID: 4242, AccountLogin: "acme", AccountType: "Organization", Status: "active", RepositorySelection: "selected",
	}); err != nil {
		t.Fatal(err)
	}
	f.post(t, signedHook("installation", "guid-known", fixture(t, "installation_created.json")))
	other := bytes.ReplaceAll(fixture(t, "installation_created.json"), []byte(`"id": 1000,`), []byte(`"id": 2000,`))
	f.post(t, signedHook("installation", "guid-unknown", other))

	rows, err := f.deps.DB.Query(`SELECT guid, installation_id IS NOT NULL FROM webhook_deliveries ORDER BY guid`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]bool{}
	for rows.Next() {
		var guid string
		var linked bool
		if err := rows.Scan(&guid, &linked); err != nil {
			t.Fatal(err)
		}
		got[guid] = linked
	}
	if !got["guid-known"] || got["guid-unknown"] || len(got) != 2 {
		t.Errorf("linked = %v, want guid-known linked and guid-unknown not", got)
	}
}

func TestWebhookWithoutAnAppAnswers503(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", false, nil)
	status, resp := f.post(t, signedHook("installation", "guid-noapp", fixture(t, "installation_created.json")))
	if status != http.StatusServiceUnavailable || resp != "" || f.deliveries(t) != 0 {
		t.Fatalf("status = %d body %q deliveries %d, want 503, no body, none recorded", status, resp, f.deliveries(t))
	}
}

func TestWebhookRouteIsNotServedWithoutWebhookURL(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "", true, nil)
	status, _ := f.post(t, signedHook("installation", "guid-off", fixture(t, "installation_created.json")))
	if status != http.StatusNotFound || f.deliveries(t) != 0 {
		t.Fatalf("status = %d deliveries %d, want 404 and none", status, f.deliveries(t))
	}
}

func TestWebhookOnlyAcceptsPOSTWithoutASession(t *testing.T) {
	t.Parallel()

	f := newWebhookFlow(t, "https://release-ops.example.com/hooks/github", true, nil)
	resp, err := http.Get(f.srv.URL + hooks.Path)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", resp.StatusCode)
	}
}
