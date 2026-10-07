package ticket

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	IssueType   jiraFieldRef   `json:"issuetype"`
	Priority    *jiraFieldRef  `json:"priority,omitempty"`
}

// jiraFieldRef references a Jira field value by name (specs §5.3) or, for configs saved with
// numeric Jira IDs before RO-106, by id.
type jiraFieldRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
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
	ID   string           `json:"id"`
	To   jiraTransitionTo `json:"to"`
	Name string           `json:"name"`
}

type jiraTransitionTo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
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
	Type    string         `json:"type"`
	Version int            `json:"version"`
	Content []jiraADFBlock `json:"content"`
}

type jiraADFBlock struct {
	Type    string        `json:"type"`
	Content []jiraADFNode `json:"content"`
}

// jiraADFNode is an inline ADF node: "text" (non-empty Text) or "hardBreak".
type jiraADFNode struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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

	cfg := jiraCreateDefaults(input.Project.CreateConfig)

	fields := jiraCreateFields{
		Project:     jiraProjectRef{Key: projectKey},
		Summary:     input.Title,
		Description: plainTextADF(input.Description),
		IssueType:   jiraRef(cfg.issueType),
	}
	if cfg.priority != "" {
		ref := jiraRef(cfg.priority)
		fields.Priority = &ref
	}

	var created jiraCreateResponse
	path := "/rest/api/3/issue"
	err := j.doJSON(ctx, http.MethodPost, path, jiraCreateRequest{Fields: fields}, &created)
	if err != nil && fields.Priority != nil && isJiraPriorityFieldError(err) {
		// Team-managed projects / create screens without a Priority field reject it outright;
		// the ticket matters more than the priority, so retry once without it.
		fields.Priority = nil
		created = jiraCreateResponse{}
		err = j.doJSON(ctx, http.MethodPost, path, jiraCreateRequest{Fields: fields}, &created)
	}
	if err != nil {
		return "", fmt.Errorf("jira: create issue: %w", err)
	}
	if created.Key == "" {
		return "", errors.New("jira: create issue: empty key in response")
	}

	if cfg.initialStatus != "" {
		// The issue already exists: a failed transition must not fail the create, otherwise the
		// poll engine would not record the ticket and would create a duplicate next cycle.
		if err := j.applyInitialStatus(ctx, created.Key, cfg.initialStatus); err != nil {
			slog.WarnContext(ctx, "jira: initial status not applied",
				"issue", created.Key, "initialStatus", cfg.initialStatus, "error", err)
		}
	}
	return created.Key, nil
}

// applyInitialStatus transitions a freshly created issue to create_config.initialStatus
// (specs §5.3) unless it is already in that status.
func (j *JiraProvider) applyInitialStatus(ctx context.Context, issueKey, initialStatus string) error {
	current, err := j.GetTicketStatus(ctx, issueKey)
	if err == nil && strings.EqualFold(current, initialStatus) {
		return nil
	}
	return j.UpdateTicketStatus(ctx, issueKey, initialStatus)
}

// isJiraPriorityFieldError reports whether a create error is Jira rejecting the priority field
// itself (e.g. "Field 'priority' cannot be set. It is not on the appropriate screen").
func isJiraPriorityFieldError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unexpected status 400") && strings.Contains(msg, `"priority"`)
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
	path := "/rest/api/3/issue/" + url.PathEscape(externalID) + "?fields=status"
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

	// Status mapping values are status names (specs §5.3); also accept a status id so
	// mappings saved with numeric Jira ids before RO-106 still transition.
	for _, transition := range transitions.Transitions {
		if transition.ID == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(transition.To.Name), targetStatus) ||
			strings.TrimSpace(transition.To.ID) == targetStatus {
			return transition.ID, nil
		}
	}
	return "", fmt.Errorf("jira: no transition found to status %q", targetStatus)
}

type jiraCreateConfig struct {
	issueType     string
	priority      string
	initialStatus string
}

// jiraCreateDefaults reads create_config (specs §5.3: issueType, priority, initialStatus are
// names). issueType defaults to "Task"; priority and initialStatus are only applied when set,
// so projects without a Priority field on the create screen still work.
func jiraCreateDefaults(createConfig map[string]any) jiraCreateConfig {
	cfg := jiraCreateConfig{issueType: "Task"}
	if createConfig == nil {
		return cfg
	}
	if raw := configString(createConfig["issueType"]); raw != "" {
		cfg.issueType = raw
	}
	cfg.priority = configString(createConfig["priority"])
	cfg.initialStatus = configString(createConfig["initialStatus"])
	return cfg
}

func configString(raw any) string {
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// jiraRef references an issue type / priority by name. All-digit values are treated as Jira
// ids — configs saved before RO-106 stored numeric ids from ticket metadata.
func jiraRef(value string) jiraFieldRef {
	if isAllDigits(value) {
		return jiraFieldRef{ID: value}
	}
	return jiraFieldRef{Name: value}
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// plainTextADF converts plain text to an Atlassian Document Format doc: blank-line separated
// blocks become paragraphs, single newlines become hardBreak nodes, and empty text produces a
// doc without content (ADF rejects empty text nodes).
func plainTextADF(text string) jiraADF {
	doc := jiraADF{Type: "doc", Version: 1, Content: []jiraADFBlock{}}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		nodes := make([]jiraADFNode, 0, len(lines)*2)
		for i, line := range lines {
			if i > 0 {
				nodes = append(nodes, jiraADFNode{Type: "hardBreak"})
			}
			if line != "" {
				nodes = append(nodes, jiraADFNode{Type: "text", Text: line})
			}
		}
		doc.Content = append(doc.Content, jiraADFBlock{Type: "paragraph", Content: nodes})
		lines = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		lines = append(lines, strings.TrimRight(line, " \t"))
	}
	flush()
	return doc
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
