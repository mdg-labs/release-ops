package ticket_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

// adfPlainText converts an ADF doc back to plain text (paragraphs joined by a blank line,
// hardBreak → newline) and fails on ADF the Jira API rejects (empty text nodes).
func adfPlainText(t *testing.T, raw any) string {
	t.Helper()

	doc, ok := raw.(map[string]any)
	if !ok || doc["type"] != "doc" {
		t.Fatalf("ADF doc = %#v", raw)
	}
	content, ok := doc["content"].([]any)
	if !ok {
		t.Fatalf("ADF content = %#v, want array", doc["content"])
	}
	paragraphs := make([]string, 0, len(content))
	for _, rawBlock := range content {
		block, _ := rawBlock.(map[string]any)
		if block["type"] != "paragraph" {
			t.Fatalf("ADF block type = %v, want paragraph", block["type"])
		}
		nodes, _ := block["content"].([]any)
		var b strings.Builder
		for _, rawNode := range nodes {
			node, _ := rawNode.(map[string]any)
			switch node["type"] {
			case "text":
				text, _ := node["text"].(string)
				if text == "" {
					t.Fatalf("ADF text node is empty (Jira rejects it)")
				}
				b.WriteString(text)
			case "hardBreak":
				b.WriteString("\n")
			default:
				t.Fatalf("unexpected ADF node %#v", node)
			}
		}
		paragraphs = append(paragraphs, b.String())
	}
	return strings.Join(paragraphs, "\n\n")
}

type jiraCreateServer struct {
	mu            sync.Mutex
	createBodies  []map[string]any
	currentStatus string
	transitionID  string
	createErrors  []string // bodies returned with 400 for the first create calls
}

func (s *jiraCreateServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			s.createBodies = append(s.createBodies, body)
			if len(s.createErrors) > 0 {
				msg := s.createErrors[0]
				s.createErrors = s.createErrors[1:]
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, msg)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "DEV-42", "id": "10042"})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/DEV-42":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{
					"status": map[string]string{"id": "10000", "name": s.currentStatus},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"transitions": []map[string]any{
					{"id": "11", "name": "Start", "to": map[string]string{"id": "3", "name": "In Progress"}},
					{"id": "21", "name": "Cancel", "to": map[string]string{"id": "10005", "name": "Cancelled"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			transition, _ := body["transition"].(map[string]any)
			s.transitionID, _ = transition["id"].(string)
			if s.transitionID == "11" {
				s.currentStatus = "In Progress"
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method/path = %s %s", r.Method, r.URL.Path)
		}
	}
}

func newJiraCreateServer(t *testing.T, s *jiraCreateServer) *ticket.JiraProvider {
	t.Helper()
	server := httptest.NewServer(s.handler(t))
	t.Cleanup(server.Close)
	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}
	return provider
}

func createFields(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	fields, ok := body["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v", body["fields"])
	}
	return fields
}

func TestJiraCreateTicketSendsPriorityOnlyWhenConfigured(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	_, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title:   "t",
		Project: ticket.TicketProject{ExternalProjectID: "DEV", CreateConfig: map[string]any{"issueType": "Bug"}},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	fields := createFields(t, srv.createBodies[0])
	if _, ok := fields["priority"]; ok {
		t.Fatalf("priority sent although unset: %#v", fields["priority"])
	}
	if issueType, _ := fields["issuetype"].(map[string]any); issueType["name"] != "Bug" {
		t.Fatalf("issuetype = %#v", fields["issuetype"])
	}
	if srv.transitionID != "" {
		t.Fatalf("unexpected transition %q without initialStatus", srv.transitionID)
	}
}

func TestJiraCreateTicketRetriesWithoutPriorityWhenNotOnScreen(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{
		currentStatus: "To Do",
		createErrors: []string{
			`{"errorMessages":[],"errors":{"priority":"Field 'priority' cannot be set. It is not on the appropriate screen, or unknown."}}`,
		},
	}
	provider := newJiraCreateServer(t, srv)

	key, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "Task", "priority": "Medium"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if key != "DEV-42" {
		t.Fatalf("key = %q", key)
	}
	if len(srv.createBodies) != 2 {
		t.Fatalf("create calls = %d, want 2 (retry without priority)", len(srv.createBodies))
	}
	if _, ok := createFields(t, srv.createBodies[0])["priority"]; !ok {
		t.Fatal("first create should send configured priority")
	}
	if _, ok := createFields(t, srv.createBodies[1])["priority"]; ok {
		t.Fatal("retry should omit priority")
	}
}

func TestJiraCreateTicketOtherErrorsDoNotRetry(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{
		createErrors: []string{`{"errorMessages":[],"errors":{"issuetype":"Specify a valid issue type"}}`},
	}
	provider := newJiraCreateServer(t, srv)

	_, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "Nope", "priority": "Medium"},
		},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(srv.createBodies) != 1 {
		t.Fatalf("create calls = %d, want 1", len(srv.createBodies))
	}
}

func TestJiraCreateTicketAppliesInitialStatus(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	_, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "Task", "initialStatus": "In Progress"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if srv.transitionID != "11" {
		t.Fatalf("transition id = %q, want 11 (→ In Progress)", srv.transitionID)
	}
}

func TestJiraCreateTicketSkipsInitialStatusWhenAlreadyCurrent(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	_, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "Task", "initialStatus": "to do"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if srv.transitionID != "" {
		t.Fatalf("unexpected transition %q", srv.transitionID)
	}
}

func TestJiraCreateTicketInitialStatusFailureKeepsTicket(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	key, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "Task", "initialStatus": "Nonexistent"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v (initialStatus failure must not fail create)", err)
	}
	if key != "DEV-42" {
		t.Fatalf("key = %q", key)
	}
}

func TestJiraCreateTicketLegacyNumericIDs(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	_, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig:      map[string]any{"issueType": "10002", "priority": "3"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	fields := createFields(t, srv.createBodies[0])
	issueType, _ := fields["issuetype"].(map[string]any)
	if issueType["id"] != "10002" || issueType["name"] != nil {
		t.Fatalf("issuetype = %#v, want {id: 10002}", issueType)
	}
	priority, _ := fields["priority"].(map[string]any)
	if priority["id"] != "3" || priority["name"] != nil {
		t.Fatalf("priority = %#v, want {id: 3}", priority)
	}
}

func TestJiraUpdateTicketStatusMatchesLegacyStatusID(t *testing.T) {
	t.Parallel()

	srv := &jiraCreateServer{currentStatus: "To Do"}
	provider := newJiraCreateServer(t, srv)

	if err := provider.UpdateTicketStatus(context.Background(), "DEV-42", "10005"); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if srv.transitionID != "21" {
		t.Fatalf("transition id = %q, want 21", srv.transitionID)
	}
}

func TestJiraADFDescriptionLineBreaksAndEmpty(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var descriptions []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/DEV-42" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fields, _ := body["fields"].(map[string]any)
		mu.Lock()
		descriptions = append(descriptions, fields["description"])
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	const multi = "Release name: v2\r\nURL: https://example.com\n\n\nPublished: today"
	if err := provider.UpdateTicket(context.Background(), "DEV-42", "title", multi); err != nil {
		t.Fatalf("UpdateTicket multi-line: %v", err)
	}
	if err := provider.UpdateTicket(context.Background(), "DEV-42", "title", ""); err != nil {
		t.Fatalf("UpdateTicket empty: %v", err)
	}

	got := adfPlainText(t, descriptions[0])
	want := "Release name: v2\nURL: https://example.com\n\nPublished: today"
	if got != want {
		t.Fatalf("description = %q, want %q", got, want)
	}
	doc, _ := descriptions[0].(map[string]any)
	if content, _ := doc["content"].([]any); len(content) != 2 {
		t.Fatalf("paragraphs = %d, want 2", len(content))
	}

	emptyDoc, _ := descriptions[1].(map[string]any)
	content, ok := emptyDoc["content"].([]any)
	if !ok || len(content) != 0 {
		t.Fatalf("empty description content = %#v, want []", emptyDoc["content"])
	}
}
