package ticket_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func TestJiraProviderCreateTicket(t *testing.T) {
	t.Parallel()

	const (
		email    = "user@company.com"
		apiToken = "jira-api-token"
	)

	var gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key": "DEV-42",
			"id":  "10042",
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, email, apiToken, server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	externalID, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title:       "Release: github acme/widget v1.0.0",
		Description: "**URL:** https://example.com/release",
		Project: ticket.TicketProject{
			ExternalProjectID: "DEV",
			CreateConfig: map[string]any{
				"issueType": "Task",
				"priority":  "High",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if externalID != "DEV-42" {
		t.Fatalf("externalID = %q, want DEV-42", externalID)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(email+":"+apiToken))
	if gotAuth != wantAuth {
		t.Fatalf("Authorization = %q, want Basic email:token", gotAuth)
	}

	fields, ok := gotBody["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields = %#v, want object", gotBody["fields"])
	}
	project, ok := fields["project"].(map[string]any)
	if !ok || project["key"] != "DEV" {
		t.Fatalf("project key = %#v, want DEV", project)
	}
	issueType, ok := fields["issuetype"].(map[string]any)
	if !ok || issueType["name"] != "Task" {
		t.Fatalf("issuetype = %#v, want Task", issueType)
	}
}

func TestJiraProviderGetTicketStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/rest/api/3/issue/DEV-42" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"fields": map[string]any{
				"status": map[string]string{
					"name": "In Progress",
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	status, err := provider.GetTicketStatus(context.Background(), "DEV-42")
	if err != nil {
		t.Fatalf("GetTicketStatus: %v", err)
	}
	if status != "In Progress" {
		t.Fatalf("status = %q, want In Progress", status)
	}
}

func TestJiraProviderUpdateTicketStatusViaMapping(t *testing.T) {
	t.Parallel()

	mapping := jiraMapping()
	targetStatus := mapping.Superseded

	var gotTransitionID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"transitions": []map[string]any{
					{
						"id":   "21",
						"name": "Cancel",
						"to":   map[string]string{"name": "Cancelled"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue/DEV-42/transitions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			transition, _ := body["transition"].(map[string]any)
			gotTransitionID, _ = transition["id"].(string)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method/path = %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	if err := provider.UpdateTicketStatus(context.Background(), "DEV-42", targetStatus); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if gotTransitionID != "21" {
		t.Fatalf("transition id = %q, want 21 for Cancelled status", gotTransitionID)
	}
}

func TestJiraProviderAddTicketComment(t *testing.T) {
	t.Parallel()

	const commentBody = "Superseded: v1.0.0 → v2.0.0\nhttps://example.com/releases/v2.0.0"
	var gotText string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue/DEV-42/comment" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		adf, _ := body["body"].(map[string]any)
		content, _ := adf["content"].([]any)
		paragraph, _ := content[0].(map[string]any)
		textNodes, _ := paragraph["content"].([]any)
		textNode, _ := textNodes[0].(map[string]any)
		gotText, _ = textNode["text"].(string)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	if err := provider.AddTicketComment(context.Background(), "DEV-42", commentBody); err != nil {
		t.Fatalf("AddTicketComment: %v", err)
	}
	if gotText != commentBody {
		t.Fatalf("comment text = %q", gotText)
	}
	if !strings.Contains(gotText, "https://example.com/releases/v2.0.0") {
		t.Fatalf("comment missing release link: %q", gotText)
	}
}

func TestJiraProviderUpdateTicket(t *testing.T) {
	t.Parallel()

	var gotSummary string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/rest/api/3/issue/DEV-42" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		fields, _ := body["fields"].(map[string]any)
		gotSummary, _ = fields["summary"].(string)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	if err := provider.UpdateTicket(context.Background(), "DEV-42", "Release: github acme/widget v2.0.0", "updated"); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if gotSummary != "Release: github acme/widget v2.0.0" {
		t.Fatalf("summary = %q", gotSummary)
	}
}

func TestJiraProviderRequiresBaseURL(t *testing.T) {
	t.Parallel()

	_, err := ticket.NewJiraProvider("", "user@company.com", "token", nil)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url mention", err)
	}
}

func TestJiraProviderRequiresEmailAndToken(t *testing.T) {
	t.Parallel()

	_, err := ticket.NewJiraProvider("https://jira.example", "", "token", nil)
	if err == nil {
		t.Fatal("expected error for empty email")
	}
	_, err = ticket.NewJiraProvider("https://jira.example", "user@company.com", "", nil)
	if err == nil {
		t.Fatal("expected error for empty api_token")
	}
}

func TestJiraProviderInvalidProjectKey(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"errorMessages":["valid project is required"]}`)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewJiraProvider(server.URL, "user@company.com", "token", server.Client())
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}

	_, err = provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "INVALID",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid project key")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("error = %v, want 400 mention", err)
	}
}
