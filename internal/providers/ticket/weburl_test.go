package ticket_test

import (
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

	provider, err := ticket.NewLinearProvider("linear-api-key", nil)
	if err != nil {
		t.Fatalf("NewLinearProvider: %v", err)
	}
	got, err := provider.TicketWebURL(ticket.TicketProject{}, "issue-uuid")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://linear.app/issue/issue-uuid"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}
