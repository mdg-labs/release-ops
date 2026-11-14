package integrationtester_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/integrationtester"
)

type hostRewritingTransport struct {
	base *url.URL
	next http.RoundTripper
}

func (t hostRewritingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.base.Scheme
	cloned.URL.Host = t.base.Host
	cloned.Host = t.base.Host
	return t.next.RoundTrip(cloned)
}

func newHostRewritingClient(server *httptest.Server, host string) *http.Client {
	base, err := url.Parse(server.URL)
	if err != nil {
		panic(err)
	}
	return &http.Client{
		Transport: hostRewritingTransport{
			base: base,
			next: http.DefaultTransport,
		},
	}
}

func TestTesterKaneoSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/organization/list" {
			t.Fatalf("path = %q, want /api/auth/organization/list", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer kaneo-key" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	tester := integrationtester.New(server.Client())
	baseURL := server.URL
	secret := []byte(`{"api_key":"kaneo-key"}`)

	if err := tester.TestConnection(context.Background(), "kaneo", &baseURL, secret); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestTesterJiraSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/myself" {
			t.Fatalf("path = %q, want /rest/api/3/myself", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			t.Fatalf("Authorization = %q, want Basic", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	tester := integrationtester.New(server.Client())
	baseURL := server.URL
	secret := []byte(`{"email":"user@company.com","api_token":"jira-token"}`)

	if err := tester.TestConnection(context.Background(), "jira", &baseURL, secret); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
}

func TestTesterLinearSuccess(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "lin_api_test" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotQuery = body["query"]
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]string{"id": "viewer-id"}}})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.linear.app")
	tester := integrationtester.New(client)
	secret := []byte(`{"api_key":"lin_api_test"}`)

	if err := tester.TestConnection(context.Background(), "linear", nil, secret); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if !strings.Contains(gotQuery, "viewer") {
		t.Fatalf("query = %q, want viewer query", gotQuery)
	}
}

func TestTesterAuthFailureReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	tester := integrationtester.New(server.Client())
	baseURL := server.URL
	secret := []byte(`{"email":"user@company.com","api_token":"bad-token"}`)

	err := tester.TestConnection(context.Background(), "jira", &baseURL, secret)
	if err == nil {
		t.Fatal("expected error for unauthorized response")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %q, want status 401 mention", err.Error())
	}
}

func TestTesterInvalidSecretJSON(t *testing.T) {
	t.Parallel()

	tester := integrationtester.New(nil)
	err := tester.TestConnection(context.Background(), "linear", nil, []byte("not-json"))
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse integration secret") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestTesterMissingBaseURL(t *testing.T) {
	t.Parallel()

	tester := integrationtester.New(nil)
	secret := []byte(`{"email":"user@company.com","api_token":"token"}`)
	err := tester.TestConnection(context.Background(), "jira", nil, secret)
	if err == nil {
		t.Fatal("expected base_url error")
	}
	if !strings.Contains(err.Error(), "base_url is required") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestTesterRejectsRemovedSourceKinds(t *testing.T) {
	t.Parallel()

	tester := integrationtester.New(nil)
	baseURL := "https://forge.example.com"
	for _, kind := range []string{"github", "gitlab", "gitea", "forgejo", "codeberg"} {
		err := tester.TestConnection(context.Background(), kind, &baseURL, []byte(`{"token":"t"}`))
		if err == nil || !strings.Contains(err.Error(), "unsupported integration kind") {
			t.Fatalf("%s: error = %v, want unsupported integration kind", kind, err)
		}
	}
}
