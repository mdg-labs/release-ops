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

func TestLinearProviderCreateTicket(t *testing.T) {
	t.Parallel()

	const (
		teamID = "team-uuid-123"
		apiKey = "lin_api_test_key"
	)

	var gotAuth string
	var gotQuery string
	var gotVariables map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		gotQuery, _ = body["query"].(string)
		gotVariables, _ = body["variables"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueCreate": map[string]any{
					"success": true,
					"issue": map[string]string{
						"id": "issue-uuid-abc",
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint(apiKey, server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	externalID, err := provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title:       "Release: github acme/widget v1.0.0",
		Description: "**URL:** https://example.com/release",
		Project: ticket.TicketProject{
			ExternalProjectID: teamID,
			CreateConfig: map[string]any{
				"priority": 2,
				"stateId":  "state-uuid-open",
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateTicket: %v", err)
	}
	if externalID != "issue-uuid-abc" {
		t.Fatalf("externalID = %q, want issue-uuid-abc", externalID)
	}
	if gotAuth != apiKey {
		t.Fatalf("Authorization = %q, want api key header", gotAuth)
	}
	if !strings.Contains(gotQuery, "issueCreate") {
		t.Fatalf("query missing issueCreate: %q", gotQuery)
	}

	input, ok := gotVariables["input"].(map[string]any)
	if !ok {
		t.Fatalf("variables.input = %#v", gotVariables["input"])
	}
	if input["teamId"] != teamID {
		t.Fatalf("teamId = %v, want %s from external_project_id", input["teamId"], teamID)
	}
	if input["title"] != "Release: github acme/widget v1.0.0" {
		t.Fatalf("title = %v", input["title"])
	}
	if input["priority"] != float64(2) {
		t.Fatalf("priority = %v, want 2", input["priority"])
	}
	if input["stateId"] != "state-uuid-open" {
		t.Fatalf("stateId = %v", input["stateId"])
	}
}

func TestLinearProviderGetTicketStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		variables, _ := body["variables"].(map[string]any)
		if variables["id"] != "issue-uuid-abc" {
			t.Fatalf("id = %v", variables["id"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"state": map[string]string{
						"id": "state-uuid-in-progress",
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	status, err := provider.GetTicketStatus(context.Background(), "issue-uuid-abc")
	if err != nil {
		t.Fatalf("GetTicketStatus: %v", err)
	}
	if status != "state-uuid-in-progress" {
		t.Fatalf("status = %q, want state-uuid-in-progress (stateId)", status)
	}

	classified := ticket.ClassifyStatus(linearMapping(), status)
	if classified != ticket.StatusOpen {
		t.Fatalf("ClassifyStatus = %q, want open for Linear stateId mapping", classified)
	}
}

func TestLinearProviderUpdateTicketStatusViaMapping(t *testing.T) {
	t.Parallel()

	mapping := linearMapping()
	targetStatus := mapping.Superseded

	var gotStateID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		variables, _ := body["variables"].(map[string]any)
		input, _ := variables["input"].(map[string]any)
		gotStateID, _ = input["stateId"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]bool{"success": true},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	if err := provider.UpdateTicketStatus(context.Background(), "issue-uuid-abc", targetStatus); err != nil {
		t.Fatalf("UpdateTicketStatus: %v", err)
	}
	if gotStateID != "state-uuid-canceled" {
		t.Fatalf("stateId = %q, want state-uuid-canceled from status_mapping.superseded", gotStateID)
	}
}

func TestLinearProviderAddTicketComment(t *testing.T) {
	t.Parallel()

	const commentBody = "Superseded: v1.0.0 → v2.0.0\nhttps://example.com/releases/v2.0.0"
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		variables, _ := body["variables"].(map[string]any)
		input, _ := variables["input"].(map[string]any)
		gotBody, _ = input["body"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]bool{"success": true},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	if err := provider.AddTicketComment(context.Background(), "issue-uuid-abc", commentBody); err != nil {
		t.Fatalf("AddTicketComment: %v", err)
	}
	if gotBody != commentBody {
		t.Fatalf("body = %q, want supersede comment with release link", gotBody)
	}
	if !strings.Contains(gotBody, "https://example.com/releases/v2.0.0") {
		t.Fatalf("comment missing release link: %q", gotBody)
	}
}

func TestLinearProviderUpdateTicket(t *testing.T) {
	t.Parallel()

	var gotTitle, gotDescription string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		variables, _ := body["variables"].(map[string]any)
		input, _ := variables["input"].(map[string]any)
		gotTitle, _ = input["title"].(string)
		gotDescription, _ = input["description"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"issueUpdate": map[string]bool{"success": true},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	if err := provider.UpdateTicket(context.Background(), "issue-uuid-abc", "Release: github acme/widget v2.0.0", "updated body"); err != nil {
		t.Fatalf("UpdateTicket: %v", err)
	}
	if gotTitle != "Release: github acme/widget v2.0.0" {
		t.Fatalf("title = %q", gotTitle)
	}
	if gotDescription != "updated body" {
		t.Fatalf("description = %q", gotDescription)
	}
}

func TestLinearProviderRequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := ticket.NewLinearProvider("", nil)
	if err == nil {
		t.Fatal("expected error for empty api_key")
	}
	if !strings.Contains(err.Error(), "api_key") {
		t.Fatalf("error = %v, want api_key mention", err)
	}
}

func TestLinearProviderGraphQLError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []map[string]string{
				{"message": "Invalid team id"},
			},
		})
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	_, err = provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "bad-team",
		},
	})
	if err == nil {
		t.Fatal("expected error for graphql errors")
	}
	if !strings.Contains(err.Error(), "Invalid team id") {
		t.Fatalf("error = %v, want graphql error message", err)
	}
}

func TestLinearProviderUnauthorized(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"errors":[{"message":"unauthorized"}]}`)
	}))
	t.Cleanup(server.Close)

	provider, err := ticket.NewLinearProviderWithEndpoint("bad-key", server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	_, err = provider.CreateTicket(context.Background(), ticket.TicketInput{
		Title: "t",
		Project: ticket.TicketProject{
			ExternalProjectID: "team-uuid",
		},
	})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("error = %v, want 401 mention", err)
	}
}
