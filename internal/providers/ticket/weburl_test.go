package ticket_test

import (
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

	provider, err := ticket.NewLinearProvider("linear-api-key", nil)
	if err != nil {
		t.Fatalf("NewLinearProvider: %v", err)
	}
	got, err := provider.TicketWebURL("issue-uuid")
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	want := "https://linear.app/issue/issue-uuid"
	if got != want {
		t.Fatalf("TicketWebURL() = %q, want %q", got, want)
	}
}
