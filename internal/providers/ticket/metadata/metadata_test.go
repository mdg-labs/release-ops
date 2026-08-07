package metadata_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/ticket/metadata"
)

func TestPhasicalMetadataWorkspacesProjectsStatuses(t *testing.T) {
	t.Parallel()

	const (
		apiKey      = "phasical-key"
		workspaceID = "ws-1"
		projectID   = "proj-1"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspace":
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"id": workspaceID, "name": "Main Workspace"},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/project"):
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"id": projectID, "name": "Release Ops", "slug": "release-ops"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/column/"+projectID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"columns": []map[string]string{
					{"id": "col-1", "name": "Ready", "slug": "ready"},
					{"id": "col-2", "name": "In Progress", "slug": "in-progress"},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	secret := []byte(`{"api_key":"` + apiKey + `"}`)
	provider, err := metadata.NewProvider("phasical", &baseURL, secret, server.Client())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	workspaces, err := provider.ListWorkspaces(context.Background())
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ID != workspaceID {
		t.Fatalf("workspaces = %#v", workspaces)
	}

	projects, err := provider.ListProjects(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != projectID {
		t.Fatalf("projects = %#v", projects)
	}

	statuses, err := provider.ListStatuses(context.Background(), projectID)
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	if len(statuses) != 2 || statuses[0].ID != "ready" {
		t.Fatalf("statuses = %#v", statuses)
	}

	priorities, err := provider.ListPriorities(context.Background(), projectID)
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}
	if len(priorities) != 5 {
		t.Fatalf("priorities = %#v", priorities)
	}
}

func TestPhasicalMetadataNormalizesAPIBase(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workspace" {
			t.Fatalf("path = %s, want /api/workspace", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{})
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	secret := []byte(`{"api_key":"key"}`)
	provider, err := metadata.NewProvider("phasical", &baseURL, secret, server.Client())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	if _, err := provider.ListWorkspaces(context.Background()); err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
}

func TestJiraMetadataProjectsStatusesPriorities(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/project/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"values": []map[string]string{
					{"id": "10000", "key": "DEV", "name": "Development"},
				},
			})
		case "/rest/api/3/project/DEV/statuses":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id":   "1",
					"name": "Task",
					"statuses": []map[string]string{
						{"id": "1", "name": "To Do"},
						{"id": "2", "name": "In Progress"},
					},
				},
			})
		case "/rest/api/3/priority":
			_ = json.NewEncoder(w).Encode([]map[string]string{
				{"id": "3", "name": "Medium"},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	baseURL := server.URL
	secret := []byte(`{"email":"user@example.com","api_token":"token"}`)
	provider, err := metadata.NewProvider("jira", &baseURL, secret, server.Client())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	projects, err := provider.ListProjects(context.Background(), "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != "DEV" {
		t.Fatalf("projects = %#v", projects)
	}

	statuses, err := provider.ListStatuses(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	if len(statuses) != 2 || statuses[0].Name != "To Do" {
		t.Fatalf("statuses = %#v", statuses)
	}

	issueTypes, err := provider.ListIssueTypes(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("ListIssueTypes: %v", err)
	}
	if len(issueTypes) != 1 || issueTypes[0].Name != "Task" {
		t.Fatalf("issueTypes = %#v", issueTypes)
	}

	priorities, err := provider.ListPriorities(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}
	if len(priorities) != 1 || priorities[0].Name != "Medium" {
		t.Fatalf("priorities = %#v", priorities)
	}
}

func TestLinearMetadataTeamsAndStates(t *testing.T) {
	t.Parallel()

	const teamID = "team-uuid"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.Contains(body.Query, "teams"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"teams": map[string]any{
						"nodes": []map[string]string{
							{"id": teamID, "name": "Engineering", "key": "ENG"},
						},
					},
				},
			})
		case strings.Contains(body.Query, "workflowStates"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"workflowStates": map[string]any{
						"nodes": []map[string]string{
							{"id": "state-open", "name": "Todo", "type": "unstarted"},
							{"id": "state-done", "name": "Done", "type": "completed"},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected query: %s", body.Query)
		}
	}))
	t.Cleanup(server.Close)

	secret := []byte(`{"api_key":"linear-key"}`)
	provider, err := metadata.NewLinearProviderWithEndpoint(secret, server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}

	projects, err := provider.ListProjects(context.Background(), "")
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != teamID {
		t.Fatalf("projects = %#v", projects)
	}

	statuses, err := provider.ListStatuses(context.Background(), teamID)
	if err != nil {
		t.Fatalf("ListStatuses: %v", err)
	}
	if len(statuses) != 2 || statuses[0].ID != "state-open" {
		t.Fatalf("statuses = %#v", statuses)
	}

	priorities, err := provider.ListPriorities(context.Background(), teamID)
	if err != nil {
		t.Fatalf("ListPriorities: %v", err)
	}
	if len(priorities) != 5 {
		t.Fatalf("priorities = %#v", priorities)
	}
}
