package ticket_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func TestPhasicalProviderCreateTicket(t *testing.T) {
	t.Parallel()

	const (
		projectID = "proj-123"
		apiKey    = "phasical-test-key"
	)

	var gotAuth string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/task/"+projectID {
			t.Fatalf("method/path = %s %s, want POST /task/%s", r.Method, r.URL.Path, projectID)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":     "task-abc",
			"status": "ready",
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, apiKey, server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	externalID, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title:       "Release: github acme/widget v1.0.0",
		Description: "**URL:** https://example.com/release",
		Project: ticket.TicketProject{
			ExternalProjectID: projectID,
			CreateConfig: map[string]any{
				"status":   "ready",
				"priority": "high",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if externalID != "task-abc" {
		t.Fatalf("externalID = %q, want task-abc", externalID)
	}
	if gotAuth != "Bearer "+apiKey {
		t.Fatalf("Authorization = %q, want Bearer token", gotAuth)
	}
	if gotBody["title"] != "Release: github acme/widget v1.0.0" {
		t.Fatalf("title = %v", gotBody["title"])
	}
	if gotBody["status"] != "ready" {
		t.Fatalf("status = %v, want ready", gotBody["status"])
	}
	if gotBody["priority"] != "high" {
		t.Fatalf("priority = %v, want high", gotBody["priority"])
	}
}

func TestPhasicalProviderGetTicketStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/task/task-abc" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":     "task-abc",
			"status": "in-progress",
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, "key", server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	status, err := provider.GetTicketStatus(context.Background(), "task-abc")
	if err != nil {
		t.Fatalf("GetTicketStatus: %v", err)
	}
	if status != "in-progress" {
		t.Fatalf("status = %q, want in-progress", status)
	}
}

func TestPhasicalProviderUpdateTicketStatusViaMapping(t *testing.T) {
	t.Parallel()

	mapping := phasicalMapping()
	targetStatus := mapping.Superseded

	var gotStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/task/status/task-abc" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStatus = body["status"]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, "key", server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	if err := provider.UpdateTicketStatus(context.Background(), "task-abc", targetStatus); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if gotStatus != "cancelled" {
		t.Fatalf("status payload = %q, want cancelled from status_mapping.superseded", gotStatus)
	}
}

func TestPhasicalProviderAddTicketComment(t *testing.T) {
	t.Parallel()

	const commentBody = "Superseded: v1.0.0 → v2.0.0\nhttps://example.com/releases/v2.0.0"
	var gotContent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/comment/task-abc" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotContent = body["content"]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, "key", server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	if err := provider.AddTicketComment(context.Background(), "task-abc", commentBody); err != nil {
		t.Fatalf("AddTicketComment: %v", err)
	}
	if gotContent != commentBody {
		t.Fatalf("content = %q, want supersede comment with release link", gotContent)
	}
	if !strings.Contains(gotContent, "https://example.com/releases/v2.0.0") {
		t.Fatalf("comment missing release link: %q", gotContent)
	}
}

func TestPhasicalProviderUpdateTicket(t *testing.T) {
	t.Parallel()

	var gotTitle, gotDescription string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/task/task-abc" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTitle = body["title"]
		gotDescription = body["description"]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, "key", server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	if err := provider.UpdateTicket(context.Background(), "task-abc", "Release: github acme/widget v2.0.0", "updated body"); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if gotTitle != "Release: github acme/widget v2.0.0" {
		t.Fatalf("title = %q", gotTitle)
	}
	if gotDescription != "updated body" {
		t.Fatalf("description = %q", gotDescription)
	}
}

func TestPhasicalProviderRequiresBaseURL(t *testing.T) {
	t.Parallel()

	_, err := ticket.NewPhasicalProvider("", "key", nil)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url mention", err)
	}
}

func TestPhasicalProviderUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewPhasicalProvider(server.URL, "bad-key", server.Client())
	if err != nil {
		t.Fatalf("NewPhasicalProvider: %v", err)
	}

	_, err = provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "proj",
		},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want 401 mention", err)
	}
}
