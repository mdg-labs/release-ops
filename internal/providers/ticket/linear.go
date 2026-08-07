package ticket

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

const (
	linearUserAgent        = "release-ops"
	linearGraphQLEndpoint  = "https://api.linear.app/graphql"
)

// LinearProvider creates and updates tickets via the Linear GraphQL API.
type LinearProvider struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

// NewLinearProvider returns a Linear ticket provider using the production GraphQL endpoint.
func NewLinearProvider(apiKey string, client *http.Client) (*LinearProvider, error) {
	return NewLinearProviderWithEndpoint(apiKey, linearGraphQLEndpoint, client)
}

// NewLinearProviderWithEndpoint returns a Linear provider targeting a custom GraphQL URL (tests).
func NewLinearProviderWithEndpoint(apiKey, endpoint string, client *http.Client) (*LinearProvider, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("linear: api_key is required")
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("linear: graphql endpoint is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &LinearProvider{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   client,
	}, nil
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type linearIssueCreateData struct {
	IssueCreate struct {
		Success bool `json:"success"`
		Issue   struct {
			ID string `json:"id"`
		} `json:"issue"`
	} `json:"issueCreate"`
}

type linearIssueQueryData struct {
	Issue struct {
		State struct {
			ID string `json:"id"`
		} `json:"state"`
	} `json:"issue"`
}

type linearMutationSuccessData struct {
	IssueUpdate struct {
		Success bool `json:"success"`
	} `json:"issueUpdate"`
	CommentCreate struct {
		Success bool `json:"success"`
	} `json:"commentCreate"`
}

// CreateTicket implements TicketProvider.
func (l *LinearProvider) CreateTicket(ctx context.Context, input TicketInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	teamID := strings.TrimSpace(input.Project.ExternalProjectID)
	if teamID == "" {
		return "", errors.New("linear: external_project_id (teamId) is required")
	}

	priority, stateID := linearCreateDefaults(input.Project.CreateConfig)

	issueInput := map[string]any{
		"teamId":      teamID,
		"title":       input.Title,
		"description": input.Description,
	}
	if priority != nil {
		issueInput["priority"] = *priority
	}
	if stateID != "" {
		issueInput["stateId"] = stateID
	}

	const query = `mutation IssueCreate($input: IssueCreateInput!) {
  issueCreate(input: $input) {
    success
    issue { id }
  }
}`

	var data linearIssueCreateData
	if err := l.doGraphQL(ctx, query, map[string]any{"input": issueInput}, &data); err != nil {
		return "", fmt.Errorf("linear: create issue: %w", err)
	}
	if !data.IssueCreate.Success {
		return "", errors.New("linear: create issue: mutation returned success=false")
	}
	if data.IssueCreate.Issue.ID == "" {
		return "", errors.New("linear: create issue: empty id in response")
	}
	return data.IssueCreate.Issue.ID, nil
}

// GetTicketStatus implements TicketProvider.
// Returns the Linear workflow stateId for status_mapping (specs §5.3).
func (l *LinearProvider) GetTicketStatus(ctx context.Context, externalID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("linear: external id is required")
	}

	const query = `query Issue($id: String!) {
  issue(id: $id) {
    state { id }
  }
}`

	var data linearIssueQueryData
	if err := l.doGraphQL(ctx, query, map[string]any{"id": externalID}, &data); err != nil {
		return "", fmt.Errorf("linear: get issue: %w", err)
	}
	stateID := strings.TrimSpace(data.Issue.State.ID)
	if stateID == "" {
		return "", errors.New("linear: get issue: empty state id in response")
	}
	return stateID, nil
}

// UpdateTicketStatus implements TicketProvider.
// status is a Linear stateId from status_mapping (e.g. superseded).
func (l *LinearProvider) UpdateTicketStatus(ctx context.Context, externalID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("linear: external id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return errors.New("linear: status is required")
	}

	const query = `mutation IssueUpdate($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) {
    success
  }
}`

	var data linearMutationSuccessData
	variables := map[string]any{
		"id": externalID,
		"input": map[string]any{
			"stateId": status,
		},
	}
	if err := l.doGraphQL(ctx, query, variables, &data); err != nil {
		return fmt.Errorf("linear: update status: %w", err)
	}
	if !data.IssueUpdate.Success {
		return errors.New("linear: update status: mutation returned success=false")
	}
	return nil
}

// AddTicketComment implements TicketProvider.
func (l *LinearProvider) AddTicketComment(ctx context.Context, externalID, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("linear: external id is required")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("linear: comment body is required")
	}

	const query = `mutation CommentCreate($input: CommentCreateInput!) {
  commentCreate(input: $input) {
    success
  }
}`

	var data linearMutationSuccessData
	variables := map[string]any{
		"input": map[string]any{
			"issueId": externalID,
			"body":    body,
		},
	}
	if err := l.doGraphQL(ctx, query, variables, &data); err != nil {
		return fmt.Errorf("linear: add comment: %w", err)
	}
	if !data.CommentCreate.Success {
		return errors.New("linear: add comment: mutation returned success=false")
	}
	return nil
}

// UpdateTicket implements TicketProvider.
func (l *LinearProvider) UpdateTicket(ctx context.Context, externalID, title, description string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("linear: external id is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("linear: title is required")
	}

	const query = `mutation IssueUpdate($id: String!, $input: IssueUpdateInput!) {
  issueUpdate(id: $id, input: $input) {
    success
  }
}`

	var data linearMutationSuccessData
	variables := map[string]any{
		"id": externalID,
		"input": map[string]any{
			"title":       title,
			"description": description,
		},
	}
	if err := l.doGraphQL(ctx, query, variables, &data); err != nil {
		return fmt.Errorf("linear: update issue: %w", err)
	}
	if !data.IssueUpdate.Success {
		return errors.New("linear: update issue: mutation returned success=false")
	}
	return nil
}

func linearCreateDefaults(createConfig map[string]any) (priority *int, stateID string) {
	if createConfig == nil {
		return nil, ""
	}

	switch raw := createConfig["priority"].(type) {
	case int:
		priority = &raw
	case float64:
		p := int(raw)
		priority = &p
	}
	if raw, ok := createConfig["stateId"].(string); ok {
		stateID = strings.TrimSpace(raw)
	}
	return priority, stateID
}

func (l *LinearProvider) doGraphQL(ctx context.Context, query string, variables map[string]any, data any) error {
	payload := graphQLRequest{
		Query:     query,
		Variables: variables,
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
	req.Header.Set("User-Agent", linearUserAgent)
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

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	if data == nil {
		return nil
	}
	if len(gqlResp.Data) == 0 || string(gqlResp.Data) == "null" {
		return errors.New("graphql response missing data")
	}
	if err := json.Unmarshal(gqlResp.Data, data); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}
