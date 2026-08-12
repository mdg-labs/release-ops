package ticket

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const jiraUserAgent = "release-ops"

// JiraProvider creates and updates tickets via Jira REST API v3.
type JiraProvider struct {
	baseURL  string
	email    string
	apiToken string
	client   *http.Client
}

// NewJiraProvider returns a Jira ticket provider.
// baseURL is the Jira instance root (e.g. https://company.atlassian.net).
func NewJiraProvider(baseURL, email, apiToken string, client *http.Client) (*JiraProvider, error) {
	normalized, err := normalizeTicketBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("jira base_url: %w", err)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("jira: email is required")
	}
	apiToken = strings.TrimSpace(apiToken)
	if apiToken == "" {
		return nil, errors.New("jira: api_token is required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &JiraProvider{
		baseURL:  normalized,
		email:    email,
		apiToken: apiToken,
		client:   client,
	}, nil
}

type jiraCreateRequest struct {
	Fields jiraCreateFields `json:"fields"`
}

type jiraCreateFields struct {
	Project     jiraProjectRef `json:"project"`
	Summary     string         `json:"summary"`
	Description jiraADF        `json:"description"`
	IssueType   jiraNamedRef   `json:"issuetype"`
	Priority    *jiraNamedRef  `json:"priority,omitempty"`
}

type jiraProjectRef struct {
	Key string `json:"key"`
}

type jiraNamedRef struct {
	Name string `json:"name"`
}

type jiraCreateResponse struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

type jiraIssueResponse struct {
	Fields jiraIssueFields `json:"fields"`
}

type jiraIssueFields struct {
	Status jiraNamedRef `json:"status"`
}

type jiraTransitionsResponse struct {
	Transitions []jiraTransition `json:"transitions"`
}

type jiraTransition struct {
	ID   string       `json:"id"`
	To   jiraNamedRef `json:"to"`
	Name string       `json:"name"`
}

type jiraTransitionRequest struct {
	Transition jiraTransitionRef `json:"transition"`
}

type jiraTransitionRef struct {
	ID string `json:"id"`
}

type jiraCommentRequest struct {
	Body jiraADF `json:"body"`
}

type jiraUpdateRequest struct {
	Fields jiraUpdateFields `json:"fields"`
}

type jiraUpdateFields struct {
	Summary     string  `json:"summary"`
	Description jiraADF `json:"description"`
}

type jiraADF struct {
	Type    string       `json:"type"`
	Version int          `json:"version"`
	Content []jiraADFBlock `json:"content"`
}

type jiraADFBlock struct {
	Type    string        `json:"type"`
	Content []jiraADFText `json:"content"`
}

type jiraADFText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// CreateTicket implements TicketProvider.
func (j *JiraProvider) CreateTicket(ctx context.Context, input TicketInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	projectKey := strings.TrimSpace(input.Project.ExternalProjectID)
	if projectKey == "" {
		return "", errors.New("jira: external_project_id (project key) is required")
	}

	issueType, priority := jiraCreateDefaults(input.Project.CreateConfig)

	fields := jiraCreateFields{
		Project:     jiraProjectRef{Key: projectKey},
		Summary:     input.Title,
		Description: plainTextADF(input.Description),
		IssueType:   jiraNamedRef{Name: issueType},
	}
	if priority != "" {
		fields.Priority = &jiraNamedRef{Name: priority}
	}

	var created jiraCreateResponse
	path := "/rest/api/3/issue"
	if err := j.doJSON(ctx, http.MethodPost, path, jiraCreateRequest{Fields: fields}, &created); err != nil {
		return "", fmt.Errorf("jira: create issue: %w", err)
	}
	if created.Key == "" {
		return "", errors.New("jira: create issue: empty key in response")
	}
	return created.Key, nil
}

// GetTicketStatus implements TicketProvider.
func (j *JiraProvider) GetTicketStatus(ctx context.Context, externalID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("jira: external id is required")
	}

	var issue jiraIssueResponse
	path := "/rest/api/3/issue/" + url.PathEscape(externalID)
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &issue); err != nil {
		return "", fmt.Errorf("jira: get issue: %w", err)
	}
	status := strings.TrimSpace(issue.Fields.Status.Name)
	if status == "" {
		return "", errors.New("jira: get issue: empty status in response")
	}
	return status, nil
}

// UpdateTicketStatus implements TicketProvider.
func (j *JiraProvider) UpdateTicketStatus(ctx context.Context, externalID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("jira: external id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return errors.New("jira: status is required")
	}

	transitionID, err := j.findTransitionID(ctx, externalID, status)
	if err != nil {
		return err
	}

	path := "/rest/api/3/issue/" + url.PathEscape(externalID) + "/transitions"
	body := jiraTransitionRequest{Transition: jiraTransitionRef{ID: transitionID}}
	if err := j.doJSON(ctx, http.MethodPost, path, body, nil); err != nil {
		return fmt.Errorf("jira: transition issue: %w", err)
	}
	return nil
}

// AddTicketComment implements TicketProvider.
func (j *JiraProvider) AddTicketComment(ctx context.Context, externalID, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("jira: external id is required")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("jira: comment body is required")
	}

	path := "/rest/api/3/issue/" + url.PathEscape(externalID) + "/comment"
	reqBody := jiraCommentRequest{Body: plainTextADF(body)}
	if err := j.doJSON(ctx, http.MethodPost, path, reqBody, nil); err != nil {
		return fmt.Errorf("jira: add comment: %w", err)
	}
	return nil
}

// UpdateTicket implements TicketProvider.
func (j *JiraProvider) UpdateTicket(ctx context.Context, externalID, title, description string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("jira: external id is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("jira: title is required")
	}

	path := "/rest/api/3/issue/" + url.PathEscape(externalID)
	body := jiraUpdateRequest{
		Fields: jiraUpdateFields{
			Summary:     title,
			Description: plainTextADF(description),
		},
	}
	if err := j.doJSON(ctx, http.MethodPut, path, body, nil); err != nil {
		return fmt.Errorf("jira: update issue: %w", err)
	}
	return nil
}

// TicketWebURL implements TicketProvider.
func (j *JiraProvider) TicketWebURL(externalID string) (string, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("jira: external id is required")
	}
	return j.baseURL + "/browse/" + url.PathEscape(externalID), nil
}

func (j *JiraProvider) findTransitionID(ctx context.Context, issueKey, targetStatus string) (string, error) {
	var transitions jiraTransitionsResponse
	path := "/rest/api/3/issue/" + url.PathEscape(issueKey) + "/transitions"
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &transitions); err != nil {
		return "", fmt.Errorf("jira: list transitions: %w", err)
	}

	for _, transition := range transitions.Transitions {
		if strings.EqualFold(strings.TrimSpace(transition.To.Name), targetStatus) {
			if transition.ID == "" {
				break
			}
			return transition.ID, nil
		}
	}
	return "", fmt.Errorf("jira: no transition found to status %q", targetStatus)
}

func jiraCreateDefaults(createConfig map[string]any) (issueType, priority string) {
	issueType = "Task"
	priority = "Medium"

	if createConfig == nil {
		return issueType, priority
	}
	if raw, ok := createConfig["issueType"].(string); ok && strings.TrimSpace(raw) != "" {
		issueType = strings.TrimSpace(raw)
	}
	if raw, ok := createConfig["priority"].(string); ok && strings.TrimSpace(raw) != "" {
		priority = strings.TrimSpace(raw)
	}
	return issueType, priority
}

func plainTextADF(text string) jiraADF {
	return jiraADF{
		Type:    "doc",
		Version: 1,
		Content: []jiraADFBlock{
			{
				Type: "paragraph",
				Content: []jiraADFText{
					{Type: "text", Text: text},
				},
			},
		},
	}
}

func (j *JiraProvider) doJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	endpoint := j.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", jiraUserAgent)
	req.Header.Set("Authorization", "Basic "+j.basicAuth())

	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}

	if respBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (j *JiraProvider) basicAuth() string {
	credentials := j.email + ":" + j.apiToken
	return base64.StdEncoding.EncodeToString([]byte(credentials))
}
