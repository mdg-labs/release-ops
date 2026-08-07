package ticket_test

import (
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func phasicalMapping() ticket.StatusMapping {
	return ticket.StatusMapping{
		Open:       []string{"ready", "in-progress", "in-review"},
		Done:       []string{"done"},
		Cancelled:  []string{"cancelled", "canceled"},
		Superseded: "cancelled",
	}
}

func jiraMapping() ticket.StatusMapping {
	return ticket.StatusMapping{
		Open:       []string{"To Do", "In Progress"},
		Done:       []string{"Done"},
		Cancelled:  []string{"Cancelled"},
		Superseded: "Cancelled",
	}
}

func linearMapping() ticket.StatusMapping {
	return ticket.StatusMapping{
		Open:       []string{"state-uuid-open", "state-uuid-in-progress"},
		Done:       []string{"state-uuid-done"},
		Cancelled:  []string{"state-uuid-canceled"},
		Superseded: "state-uuid-canceled",
	}
}

func TestClassifyStatusMappingFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mapping   ticket.StatusMapping
		rawStatus string
		want      string
	}{
		{
			name:      "phasical open ready",
			mapping:   phasicalMapping(),
			rawStatus: "ready",
			want:      ticket.StatusOpen,
		},
		{
			name:      "phasical open in-progress case insensitive",
			mapping:   phasicalMapping(),
			rawStatus: "In-Progress",
			want:      ticket.StatusOpen,
		},
		{
			name:      "phasical done",
			mapping:   phasicalMapping(),
			rawStatus: "done",
			want:      ticket.StatusDone,
		},
		{
			name:      "phasical cancelled american spelling",
			mapping:   phasicalMapping(),
			rawStatus: "canceled",
			want:      ticket.StatusCancelled,
		},
		{
			name:      "jira open to do",
			mapping:   jiraMapping(),
			rawStatus: "To Do",
			want:      ticket.StatusOpen,
		},
		{
			name:      "jira done",
			mapping:   jiraMapping(),
			rawStatus: "done",
			want:      ticket.StatusDone,
		},
		{
			name:      "jira cancelled",
			mapping:   jiraMapping(),
			rawStatus: "Cancelled",
			want:      ticket.StatusCancelled,
		},
		{
			name:      "linear open state id",
			mapping:   linearMapping(),
			rawStatus: "state-uuid-in-progress",
			want:      ticket.StatusOpen,
		},
		{
			name:      "linear done state id",
			mapping:   linearMapping(),
			rawStatus: "state-uuid-done",
			want:      ticket.StatusDone,
		},
		{
			name:      "linear cancelled state id",
			mapping:   linearMapping(),
			rawStatus: "state-uuid-canceled",
			want:      ticket.StatusCancelled,
		},
		{
			name:      "unknown status",
			mapping:   phasicalMapping(),
			rawStatus: "archived",
			want:      ticket.StatusUnknown,
		},
		{
			name:      "empty raw status",
			mapping:   phasicalMapping(),
			rawStatus: "   ",
			want:      ticket.StatusUnknown,
		},
		{
			name:      "empty mapping",
			mapping:   ticket.StatusMapping{},
			rawStatus: "ready",
			want:      ticket.StatusUnknown,
		},
		{
			name:      "open wins over done when both match",
			mapping: ticket.StatusMapping{
				Open:      []string{"shared"},
				Done:      []string{"shared"},
				Cancelled: []string{"shared"},
			},
			rawStatus: "shared",
			want:      ticket.StatusOpen,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ticket.ClassifyStatus(tt.mapping, tt.rawStatus)
			if got != tt.want {
				t.Fatalf("ClassifyStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseStatusMapping(t *testing.T) {
	t.Parallel()

	raw := `{"open":["ready","in-progress"],"done":["done"],"cancelled":["cancelled"],"superseded":"cancelled"}`
	got, err := ticket.ParseStatusMapping(raw)
	if err != nil {
		t.Fatalf("ParseStatusMapping: %v", err)
	}

	want := phasicalMapping()
	want.Open = []string{"ready", "in-progress"}

	if len(got.Open) != len(want.Open) || got.Open[0] != want.Open[0] || got.Open[1] != want.Open[1] {
		t.Fatalf("Open = %#v, want %#v", got.Open, want.Open)
	}
	if got.Superseded != want.Superseded {
		t.Fatalf("Superseded = %q, want %q", got.Superseded, want.Superseded)
	}
}

func TestParseStatusMappingInvalidJSON(t *testing.T) {
	t.Parallel()

	if _, err := ticket.ParseStatusMapping("not-json"); err == nil {
		t.Fatal("ParseStatusMapping expected error for invalid JSON")
	}
}
