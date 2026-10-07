package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const linearGraphQLEndpoint = "https://api.linear.app/graphql"

type linearProvider struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

func newLinearProvider(secret []byte, client *http.Client) (*linearProvider, error) {
	return newLinearProviderWithEndpoint(secret, linearGraphQLEndpoint, client)
}

func newLinearProviderWithEndpoint(secret []byte, endpoint string, client *http.Client) (*linearProvider, error) {
	apiKey, err := parseLinearSecret(secret)
	if err != nil {
		return nil, err
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("linear: graphql endpoint is required")
	}
	return &linearProvider{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   client,
	}, nil
}

func (l *linearProvider) ListWorkspaces(context.Context) ([]Item, error) {
	return nil, ErrUnsupported
}

func (l *linearProvider) ListProjects(ctx context.Context, _ string) ([]Item, error) {
	const query = `query Teams {
  teams {
    nodes {
      id
      name
      key
    }
  }
}`

	var data struct {
		Teams struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Key  string `json:"key"`
			} `json:"nodes"`
		} `json:"teams"`
	}
	if err := l.doGraphQL(ctx, query, nil, &data); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(data.Teams.Nodes))
	for _, team := range data.Teams.Nodes {
		id := strings.TrimSpace(team.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(team.Name)
		if name == "" {
			name = strings.TrimSpace(team.Key)
		}
		if name == "" {
			name = id
		}
		label := name
		if key := strings.TrimSpace(team.Key); key != "" && key != name {
			label = fmt.Sprintf("%s (%s)", name, key)
		}
		items = append(items, Item{ID: id, Name: name, Label: label})
	}
	return items, nil
}

func (l *linearProvider) ListStatuses(ctx context.Context, externalProjectID string) ([]Item, error) {
	externalProjectID = strings.TrimSpace(externalProjectID)
	if externalProjectID == "" {
		return nil, errors.New("linear: externalProjectId query parameter is required")
	}

	const query = `query WorkflowStates($teamId: ID!) {
  workflowStates(filter: { team: { id: { eq: $teamId } } }) {
    nodes {
      id
      name
      type
    }
  }
}`

	var data struct {
		WorkflowStates struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := l.doGraphQL(ctx, query, map[string]any{"teamId": externalProjectID}, &data); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(data.WorkflowStates.Nodes))
	for _, state := range data.WorkflowStates.Nodes {
		id := strings.TrimSpace(state.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(state.Name)
		if name == "" {
			name = id
		}
		label := name
		if stateType := strings.TrimSpace(state.Type); stateType != "" {
			label = fmt.Sprintf("%s (%s)", name, stateType)
		}
		items = append(items, Item{ID: id, Name: name, Label: label})
	}
	return items, nil
}

func (l *linearProvider) ListPriorities(context.Context, string) ([]Item, error) {
	return []Item{
		{ID: "0", Name: "No priority"},
		{ID: "1", Name: "Urgent"},
		{ID: "2", Name: "High"},
		{ID: "3", Name: "Medium"},
		{ID: "4", Name: "Low"},
	}, nil
}

func (l *linearProvider) ListIssueTypes(context.Context, string) ([]Item, error) {
	return nil, ErrUnsupported
}

func (l *linearProvider) doGraphQL(ctx context.Context, query string, variables map[string]any, data any) error {
	payload := map[string]any{
		"query": query,
	}
	if variables != nil {
		payload["variables"] = variables
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "release-ops")
	req.Header.Set("Authorization", l.apiKey)

	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var gqlResp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	if len(gqlResp.Data) == 0 || string(gqlResp.Data) == "null" {
		return errors.New("graphql response missing data")
	}
	if err := json.Unmarshal(gqlResp.Data, data); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}
