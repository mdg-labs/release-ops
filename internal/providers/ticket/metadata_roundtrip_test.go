package ticket_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
	"github.com/mdg-labs/release-ops/internal/providers/ticket/metadata"
)

// These tests round-trip ticket metadata → saved create_config / status_mapping → ticket
// provider, mirroring the web UI which stores the metadata item `id` (metadata-select.tsx).

func itemIDByName(t *testing.T, items []metadata.Item, name string) string {
	t.Helper()
	for _, item := range items {
		if item.Name == name {
			return item.ID
		}
	}
	t.Fatalf("metadata item %q not found in %#v", name, items)
	return ""
}

func TestJiraMetadataRoundTripToProvider(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var createBody map[string]any
	var transitionID string

	// Jira Cloud fixture: numeric ids differ from names, as on real instances.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/project/DEV/statuses":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": "10002", "name": "Task", "subtask": false,
					"statuses": []map[string]string{
						{"id": "10001", "name": "To Do"},
						{"id": "3", "name": "In Progress"},
						{"id": "10003", "name": "Done"},
						{"id": "10005", "name": "Cancelled"},
					},
				},
				{
					"id": "10003", "name": "Sub-task", "subtask": true,
					"statuses": []map[string]string{{"id": "10001", "name": "To Do"}},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/priority":
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"id": "2", "name": "High"},
				{"id": "3", "name": "Medium"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			_ = json.NewDecoder(r.Body).Decode(&createBody)
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "DEV-42", "id": "10042"})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/DEV-42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{"status": map[string]string{"id": "10001", "name": "To Do"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"transitions": []map[string]any{
					{"id": "11", "name": "Start", "to": map[string]string{"id": "3", "name": "In Progress"}},
					{"id": "31", "name": "Cancel", "to": map[string]string{"id": "10005", "name": "Cancelled"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			transition, _ := body["transition"].(map[string]any)
			transitionID, _ = transition["id"].(string)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	baseURL := server.URL
	meta, err := metadata.NewProvider("jira", &baseURL, []byte(`{"email":"u@example.com","api_token":"tok"}`), server.Client())
	if err != nil {
		t.Fatalf("metadata.NewProvider: %v", err)
	}
	statuses, err := meta.ListStatuses(ctx, "DEV")
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	issueTypes, err := meta.ListIssueTypes(ctx, "DEV")
	if err != nil {
		t.Fatalf("ListIssueTypes: %v", err)
	}
	if len(issueTypes) != 1 {
		t.Fatalf("issueTypes = %#v, want sub-task filtered", issueTypes)
	}
	priorities, err := meta.ListPriorities(ctx, "DEV")
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}

	// What the UI saves: metadata ids.
	project := ticket.TicketProject{
		ExternalProjectID: "DEV",
		CreateConfig: map[string]any{
			"issueType":     itemIDByName(t, issueTypes, "Task"),
			"priority":      itemIDByName(t, priorities, "High"),
			"initialStatus": itemIDByName(t, statuses, "In Progress"),
		},
		StatusMapping: ticket.StatusMapping{
			Open:       []string{itemIDByName(t, statuses, "To Do"), itemIDByName(t, statuses, "In Progress")},
			Done:       []string{itemIDByName(t, statuses, "Done")},
			Cancelled:  []string{itemIDByName(t, statuses, "Cancelled")},
			Superseded: itemIDByName(t, statuses, "Cancelled"),
		},
	}

	provider, err := ticket.NewJiraProvider(server.URL, "u@example.com", "tok", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	key, err := provider.CreateTicket(ctx, ticket.TicketInput{Title: "t", Description: "d", Project: project})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	fields := createFields(t, createBody)
	if issueType, _ := fields["issuetype"].(map[string]any); issueType["name"] != "Task" {
		t.Fatalf("issuetype = %#v, want name Task", fields["issuetype"])
	}
	if priority, _ := fields["priority"].(map[string]any); priority["name"] != "High" {
		t.Fatalf("priority = %#v, want name High", fields["priority"])
	}
	mu.Lock()
	gotInitial := transitionID
	transitionID = ""
	mu.Unlock()
	if gotInitial != "11" {
		t.Fatalf("initialStatus transition = %q, want 11", gotInitial)
	}

	status, err := provider.GetTicketStatus(ctx, key)
	if err != nil {
		t.Fatalf("GetTicketStatus: %v", err)
	}
	if got := ticket.ClassifyStatus(project.StatusMapping, status); got != ticket.StatusOpen {
		t.Fatalf("ClassifyStatus(%q) = %q, want open", status, got)
	}

	if err := provider.UpdateTicketStatus(ctx, key, project.StatusMapping.Superseded); err != nil {
		t.Fatalf("UpdateTicketStatus(superseded): %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if transitionID != "31" {
		t.Fatalf("supersede transition = %q, want 31", transitionID)
	}
}

func TestLinearMetadataRoundTripToProvider(t *testing.T) {
	t.Parallel()

	const teamID = "team-uuid"
	var mu sync.Mutex
	var createInput map[string]any
	urlLookups := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "workflowStates"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"workflowStates": map[string]any{"nodes": []map[string]string{
					{"id": "state-todo", "name": "Todo", "type": "unstarted"},
					{"id": "state-done", "name": "Done", "type": "completed"},
					{"id": "state-canceled", "name": "Canceled", "type": "canceled"},
				}},
			}})
		case strings.Contains(body.Query, "issueCreate"):
			createInput, _ = body.Variables["input"].(map[string]any)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"issueCreate": map[string]any{
					"success": true,
					"issue":   map[string]string{"id": "roundtrip-issue-uuid", "url": "https://linear.app/acme/issue/ENG-123/release"},
				},
			}})
		case strings.Contains(body.Query, "url"):
			urlLookups++
			t.Errorf("unexpected url lookup; url should be cached from issueCreate")
			w.WriteHeader(http.StatusInternalServerError)
		case strings.Contains(body.Query, "state"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"issue": map[string]any{"state": map[string]string{"id": "state-todo"}},
			}})
		default:
			t.Fatalf("unexpected query %s", body.Query)
		}
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	meta, err := metadata.NewLinearProviderWithEndpoint([]byte(`{"api_key":"k"}`), server.URL, server.Client())
	if err != nil {
		t.Fatalf("metadata provider: %v", err)
	}
	statuses, err := meta.ListStatuses(ctx, teamID)
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	priorities, err := meta.ListPriorities(ctx, teamID)
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}

	project := ticket.TicketProject{
		ExternalProjectID: teamID,
		CreateConfig: map[string]any{
			// metadata priority ids are strings ("0".."4")
			"priority": itemIDByName(t, priorities, "High"),
			"stateId":  itemIDByName(t, statuses, "Todo"),
		},
		StatusMapping: ticket.StatusMapping{
			Open:       []string{itemIDByName(t, statuses, "Todo")},
			Done:       []string{itemIDByName(t, statuses, "Done")},
			Cancelled:  []string{itemIDByName(t, statuses, "Canceled")},
			Superseded: itemIDByName(t, statuses, "Canceled"),
		},
	}

	provider, err := ticket.NewLinearProviderWithEndpoint("k", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}
	id, err := provider.CreateTicket(ctx, ticket.TicketInput{Title: "t", Description: "d", Project: project})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}

	mu.Lock()
	gotPriority := createInput["priority"]
	gotState := createInput["stateId"]
	mu.Unlock()
	if gotPriority != float64(2) {
		t.Fatalf("priority = %#v, want 2 (High)", gotPriority)
	}
	if gotState != "state-todo" {
		t.Fatalf("stateId = %#v", gotState)
	}

	status, err := provider.GetTicketStatus(ctx, id)
	if err != nil {
		t.Fatalf("GetTicketStatus: %v", err)
	}
	if got := ticket.ClassifyStatus(project.StatusMapping, status); got != ticket.StatusOpen {
		t.Fatalf("ClassifyStatus(%q) = %q, want open", status, got)
	}

	webURL, err := provider.TicketWebURL(id)
	if err != nil {
		t.Fatalf("TicketWebURL: %v", err)
	}
	if webURL != "https://linear.app/acme/issue/ENG-123/release" {
		t.Fatalf("TicketWebURL = %q, want API-provided url", webURL)
	}
	// A fresh provider (as the status API builds per request) reuses the cached url.
	other, err := ticket.NewLinearProviderWithEndpoint("k", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}
	if got, _ := other.TicketWebURL(id); got != webURL {
		t.Fatalf("fresh provider TicketWebURL = %q, want %q", got, webURL)
	}
	mu.Lock()
	defer mu.Unlock()
	if urlLookups != 0 {
		t.Fatalf("url lookups = %d, want 0", urlLookups)
	}
}

func TestLinearCreateTicketPriorityForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		priority any
		want     any // nil = not sent
	}{
		{name: "json number", priority: float64(1), want: float64(1)},
		{name: "int", priority: 3, want: float64(3)},
		{name: "numeric string", priority: "4", want: float64(4)},
		{name: "zero string", priority: "0", want: float64(0)},
		{name: "empty string", priority: "", want: nil},
		{name: "out of range", priority: "7", want: nil},
		{name: "non numeric", priority: "High", want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var input map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Variables map[string]any `json:"variables"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				input, _ = body.Variables["input"].(map[string]any)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
					"issueCreate": map[string]any{"success": true, "issue": map[string]string{"id": "prio-" + tc.name}},
				}})
			}))
			t.Cleanup(server.Close)

			provider, err := ticket.NewLinearProviderWithEndpoint("k", server.URL, server.Client())
			if err != nil {
				t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
			}
			_, err = provider.CreateTicket(context.Background(), ticket.TicketInput{
				Title: "t",
				Project: ticket.TicketProject{
					ExternalProjectID: "team",
					CreateConfig:      map[string]any{"priority": tc.priority},
				},
			})
			if err != nil {
				t.Fatalf("CreateTicket: %v", err)
			}
			got, sent := input["priority"]
			if tc.want == nil {
				if sent {
					t.Fatalf("priority sent = %#v, want omitted", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("priority = %#v, want %#v", got, tc.want)
			}
		})
	}
}
