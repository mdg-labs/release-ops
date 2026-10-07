package ticket_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func TestKaneoTicketWebURL(t *testing.T) {
	t.Parallel()

	project := ticket.TicketProject{
		ExternalProjectID: "proj-1",
		CreateConfig:      map[string]any{"workspaceId": "ws-1", "status": "ready"},
	}

	for _, base := range []string{"https://cloud.kaneo.app", "https://cloud.kaneo.app/api", "https://cloud.kaneo.app/api/"} {
		provider, err := ticket.NewKaneoProvider(base, "key", nil)
		if err != nil {
			t.Fatalf("NewKaneoProvider(%q): %v", base, err)
		}
		got, err := provider.TicketWebURL(project, "task-123")
		if err != nil {
			t.Fatalf("TicketWebURL: %v", err)
		}
		want := "https://cloud.kaneo.app/dashboard/workspace/ws-1/project/proj-1/task/task-123"
		if got != want {
			t.Fatalf("TicketWebURL(%q) = %q, want %q", base, got, want)
		}
	}
}

func TestKaneoTicketWebURLOmittedWithoutWorkspace(t *testing.T) {
	t.Parallel()

	provider, err := ticket.NewKaneoProvider("https://cloud.kaneo.app", "key", nil)
	if err != nil {
		t.Fatalf("NewKaneoProvider: %v", err)
	}
	got, err := provider.TicketWebURL(ticket.TicketProject{ExternalProjectID: "proj-1"}, "task-123")
	if err != nil {
		t.Fatalf("TicketWebURL without workspaceId: %v", err)
	}
	if got != "" {
		t.Fatalf("TicketWebURL without workspaceId = %q, want empty (link omitted, specs §6.4)", got)
	}
	if _, err := provider.TicketWebURL(ticket.TicketProject{CreateConfig: map[string]any{"workspaceId": "ws-1"}}, "task-123"); err == nil {
		t.Fatal("expected error when external_project_id is missing")
	}
}

func TestJiraTicketWebURL(t *testing.T) {
	t.Parallel()

	provider, err := ticket.NewJiraProvider("https://jira.example", "user@example.com", "token", nil)
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}
	got, err := provider.TicketWebURL(ticket.TicketProject{}, "PROJ-1")
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
			t.Errorf("id = %v", variables["id"])
			w.WriteHeader(http.StatusBadRequest)
			return
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
	got, err := provider.TicketWebURL(ticket.TicketProject{}, "weburl-issue-uuid")
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
	got, err := provider.TicketWebURL(ticket.TicketProject{}, "fallback-issue-uuid")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://linear.app/issue/fallback-issue-uuid"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}
