package ticket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const kaneoUserAgent = "release-ops"

// kaneoMaxCommentLength is the Kaneo API limit for POST /comment/{taskId} content.
const kaneoMaxCommentLength = 10000

// truncateKaneoComment keeps comment content within the Kaneo API length limit.
func truncateKaneoComment(body string) string {
	runes := []rune(body)
	if len(runes) <= kaneoMaxCommentLength {
		return body
	}
	return string(runes[:kaneoMaxCommentLength-1]) + "…"
}

// KaneoProvider creates and updates tickets via the Kaneo REST API.
type KaneoProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewKaneoProvider returns a Kaneo ticket provider.
// baseURL is the Kaneo host or API root (e.g. https://cloud.kaneo.app); /api is appended when missing.
func NewKaneoProvider(baseURL, apiKey string, client *http.Client) (*KaneoProvider, error) {
	normalized, err := normalizeTicketBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("kaneo base_url: %w", err)
	}
	normalized = normalizeKaneoAPIBase(normalized)
	if client == nil {
		client = http.DefaultClient
	}
	return &KaneoProvider{
		baseURL: normalized,
		apiKey:  strings.TrimSpace(apiKey),
		client:  client,
	}, nil
}

type kaneoCreateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
}

type kaneoTaskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type kaneoStatusRequest struct {
	Status string `json:"status"`
}

type kaneoTitleRequest struct {
	Title string `json:"title"`
}

type kaneoDescriptionRequest struct {
	Description string `json:"description"`
}

type kaneoCommentRequest struct {
	Content string `json:"content"`
}

// CreateTicket implements TicketProvider.
func (p *KaneoProvider) CreateTicket(ctx context.Context, input TicketInput) (string, error) {
	if err := ctx.Err(); err != nil {
		// Nothing was sent yet.
		return "", notSent(err)
	}

	projectID := strings.TrimSpace(input.Project.ExternalProjectID)
	if projectID == "" {
		return "", notSent(errors.New("kaneo: external_project_id is required"))
	}

	status, priority := kaneoCreateDefaults(input.Project.CreateConfig)

	body := kaneoCreateRequest{
		Title:       input.Title,
		Description: input.Description,
		Priority:    priority,
		Status:      status,
	}

	var created kaneoTaskResponse
	if err := p.doJSON(ctx, http.MethodPost, "/task/"+url.PathEscape(projectID), body, &created); err != nil {
		return "", fmt.Errorf("kaneo: create task: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("kaneo: create task: empty id in response")
	}
	return created.ID, nil
}

// GetTicketStatus implements TicketProvider.
func (p *KaneoProvider) GetTicketStatus(ctx context.Context, externalID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("kaneo: external id is required")
	}

	var task kaneoTaskResponse
	if err := p.doJSON(ctx, http.MethodGet, "/task/"+url.PathEscape(externalID), nil, &task); err != nil {
		return "", fmt.Errorf("kaneo: get task: %w", err)
	}
	if task.Status == "" {
		return "", errors.New("kaneo: get task: empty status in response")
	}
	return task.Status, nil
}

// UpdateTicketStatus implements TicketProvider.
func (p *KaneoProvider) UpdateTicketStatus(ctx context.Context, externalID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("kaneo: external id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return errors.New("kaneo: status is required")
	}

	path := "/task/status/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, path, kaneoStatusRequest{Status: status}, nil); err != nil {
		return fmt.Errorf("kaneo: update status: %w", err)
	}
	return nil
}

// AddTicketComment implements TicketProvider.
func (p *KaneoProvider) AddTicketComment(ctx context.Context, externalID, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("kaneo: external id is required")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("kaneo: comment body is required")
	}

	path := "/comment/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPost, path, kaneoCommentRequest{Content: truncateKaneoComment(body)}, nil); err != nil {
		return fmt.Errorf("kaneo: add comment: %w", err)
	}
	return nil
}

// UpdateTicket implements TicketProvider.
func (p *KaneoProvider) UpdateTicket(ctx context.Context, externalID, title, description string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("kaneo: external id is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("kaneo: title is required")
	}

	titlePath := "/task/title/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, titlePath, kaneoTitleRequest{Title: title}, nil); err != nil {
		return fmt.Errorf("kaneo: update title: %w", err)
	}

	descPath := "/task/description/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, descPath, kaneoDescriptionRequest{Description: description}, nil); err != nil {
		return fmt.Errorf("kaneo: update description: %w", err)
	}
	return nil
}

// KaneoCreateConfigWorkspaceID is the ticket_projects.create_config key holding the
// Kaneo workspace ID used to build task web links (specs §6.4).
const KaneoCreateConfigWorkspaceID = "workspaceId"

// TicketWebURL implements TicketProvider.
// Kaneo task pages live at {web}/dashboard/workspace/{workspaceId}/project/{projectId}/task/{taskId};
// the workspace ID comes from create_config.workspaceId and the project from external_project_id.
// When workspaceId is missing it returns "" (no link) per specs §6.4.
func (p *KaneoProvider) TicketWebURL(project TicketProject, externalID string) (string, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("kaneo: external id is required")
	}
	projectID := strings.TrimSpace(project.ExternalProjectID)
	if projectID == "" {
		return "", errors.New("kaneo: external_project_id is required for ticket web url")
	}
	workspaceID := ""
	if raw, ok := project.CreateConfig[KaneoCreateConfigWorkspaceID].(string); ok {
		workspaceID = strings.TrimSpace(raw)
	}
	if workspaceID == "" {
		// Specs §6.4: without a workspace the link is omitted.
		return "", nil
	}
	webBase := kaneoWebBase(p.baseURL)
	return webBase + "/dashboard/workspace/" + url.PathEscape(workspaceID) +
		"/project/" + url.PathEscape(projectID) +
		"/task/" + url.PathEscape(externalID), nil
}

func kaneoWebBase(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if strings.HasSuffix(base, "/api") {
		return strings.TrimSuffix(base, "/api")
	}
	return base
}

func kaneoCreateDefaults(createConfig map[string]any) (status, priority string) {
	status = "ready"
	priority = "medium"

	if createConfig == nil {
		return status, priority
	}
	if raw, ok := createConfig["status"].(string); ok && strings.TrimSpace(raw) != "" {
		status = strings.TrimSpace(raw)
	}
	if raw, ok := createConfig["priority"].(string); ok && strings.TrimSpace(raw) != "" {
		priority = strings.TrimSpace(raw)
	}
	return status, priority
}

func (p *KaneoProvider) doJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return notSent(fmt.Errorf("encode request: %w", err))
		}
		bodyReader = bytes.NewReader(encoded)
	}

	endpoint := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return notSent(fmt.Errorf("build request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", kaneoUserAgent)
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return newHTTPStatusError(resp.StatusCode, string(payload))
	}

	if respBody == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func normalizeTicketBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("must not be empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("must use http or https scheme")
	}
	if parsed.Host == "" {
		return "", errors.New("must include a host")
	}

	return strings.TrimRight(raw, "/"), nil
}

// normalizeKaneoAPIBase appends /api when missing (Kaneo hosts).
func normalizeKaneoAPIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return raw
	}
	if strings.HasSuffix(raw, "/api") {
		return raw
	}
	return raw + "/api"
}
