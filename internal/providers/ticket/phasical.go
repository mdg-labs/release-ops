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

const phasicalUserAgent = "release-ops"

// PhasicalProvider creates and updates tickets via the Phasical (Kaneo-compatible) REST API.
type PhasicalProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewPhasicalProvider returns a Phasical ticket provider.
// baseURL is the integration API root (e.g. https://api.phasical.example).
func NewPhasicalProvider(baseURL, apiKey string, client *http.Client) (*PhasicalProvider, error) {
	normalized, err := normalizeTicketBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("phasical base_url: %w", err)
	}
	normalized = normalizePhasicalAPIBase(normalized)
	if client == nil {
		client = http.DefaultClient
	}
	return &PhasicalProvider{
		baseURL: normalized,
		apiKey:  strings.TrimSpace(apiKey),
		client:  client,
	}, nil
}

type phasicalCreateRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	Status      string `json:"status"`
}

type phasicalTaskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type phasicalStatusRequest struct {
	Status string `json:"status"`
}

type phasicalTitleRequest struct {
	Title string `json:"title"`
}

type phasicalDescriptionRequest struct {
	Description string `json:"description"`
}

type phasicalCommentRequest struct {
	Content string `json:"content"`
}

// CreateTicket implements TicketProvider.
func (p *PhasicalProvider) CreateTicket(ctx context.Context, input TicketInput) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	projectID := strings.TrimSpace(input.Project.ExternalProjectID)
	if projectID == "" {
		return "", errors.New("phasical: external_project_id is required")
	}

	status, priority := phasicalCreateDefaults(input.Project.CreateConfig)

	body := phasicalCreateRequest{
		Title:       input.Title,
		Description: input.Description,
		Priority:    priority,
		Status:      status,
	}

	var created phasicalTaskResponse
	if err := p.doJSON(ctx, http.MethodPost, "/task/"+url.PathEscape(projectID), body, &created); err != nil {
		return "", fmt.Errorf("phasical: create task: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("phasical: create task: empty id in response")
	}
	return created.ID, nil
}

// GetTicketStatus implements TicketProvider.
func (p *PhasicalProvider) GetTicketStatus(ctx context.Context, externalID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("phasical: external id is required")
	}

	var task phasicalTaskResponse
	if err := p.doJSON(ctx, http.MethodGet, "/task/"+url.PathEscape(externalID), nil, &task); err != nil {
		return "", fmt.Errorf("phasical: get task: %w", err)
	}
	if task.Status == "" {
		return "", errors.New("phasical: get task: empty status in response")
	}
	return task.Status, nil
}

// UpdateTicketStatus implements TicketProvider.
func (p *PhasicalProvider) UpdateTicketStatus(ctx context.Context, externalID, status string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("phasical: external id is required")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return errors.New("phasical: status is required")
	}

	path := "/task/status/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, path, phasicalStatusRequest{Status: status}, nil); err != nil {
		return fmt.Errorf("phasical: update status: %w", err)
	}
	return nil
}

// AddTicketComment implements TicketProvider.
func (p *PhasicalProvider) AddTicketComment(ctx context.Context, externalID, body string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("phasical: external id is required")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return errors.New("phasical: comment body is required")
	}

	path := "/comment/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPost, path, phasicalCommentRequest{Content: body}, nil); err != nil {
		return fmt.Errorf("phasical: add comment: %w", err)
	}
	return nil
}

// UpdateTicket implements TicketProvider.
func (p *PhasicalProvider) UpdateTicket(ctx context.Context, externalID, title, description string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return errors.New("phasical: external id is required")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("phasical: title is required")
	}

	titlePath := "/task/title/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, titlePath, phasicalTitleRequest{Title: title}, nil); err != nil {
		return fmt.Errorf("phasical: update title: %w", err)
	}

	descPath := "/task/description/" + url.PathEscape(externalID)
	if err := p.doJSON(ctx, http.MethodPut, descPath, phasicalDescriptionRequest{Description: description}, nil); err != nil {
		return fmt.Errorf("phasical: update description: %w", err)
	}
	return nil
}

// TicketWebURL implements TicketProvider.
func (p *PhasicalProvider) TicketWebURL(externalID string) (string, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return "", errors.New("phasical: external id is required")
	}
	webBase := phasicalWebBase(p.baseURL)
	return webBase + "/task/" + url.PathEscape(externalID), nil
}

func phasicalWebBase(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if strings.HasSuffix(base, "/api") {
		return strings.TrimSuffix(base, "/api")
	}
	return base
}

func phasicalCreateDefaults(createConfig map[string]any) (status, priority string) {
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

func (p *PhasicalProvider) doJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	endpoint := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", phasicalUserAgent)
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

// normalizePhasicalAPIBase appends /api when missing (Kaneo-compatible Phasical hosts).
func normalizePhasicalAPIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return raw
	}
	if strings.HasSuffix(raw, "/api") {
		return raw
	}
	return raw + "/api"
}
