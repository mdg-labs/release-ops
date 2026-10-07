package ticket_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func TestPhasicalTicketWebURL(t *testing.T) {
	t.Parallel()

	provider, err := ticket.NewPhasicalProvider("https://api.phasical.example", "key", nil)
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}
	got, err := provider.TicketWebURL("task-123")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://api.phasical.example/task/task-123"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}

func TestJiraTicketWebURL(t *testing.T) {
	t.Parallel()

	provider, err := ticket.NewJiraProvider("https://jira.example", "user@example.com", "token", nil)
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}
	got, err := provider.TicketWebURL("PROJ-1")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://jira.example/browse/PROJ-1"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}

func TestLinearTicketWebURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		variables, _ := body["variables"].(map[string]any)
		if variables["id"] != "weburl-issue-uuid" {
			t.Fatalf("id = %v", variables["id"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issue": map[string]string{"url": "https://linear.app/acme/issue/ENG-7/release"},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("linear-api-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}
	got, err := provider.TicketWebURL("weburl-issue-uuid")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://linear.app/acme/issue/ENG-7/release"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}

func TestLinearTicketWebURLFallsBackWhenLookupFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("linear-api-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}
	got, err := provider.TicketWebURL("fallback-issue-uuid")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://linear.app/issue/fallback-issue-uuid"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}
