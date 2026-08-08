package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type phasicalProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func newPhasicalProvider(baseURL *string, secret []byte, client *http.Client) (*phasicalProvider, error) {
	root, err := requireBaseURL(baseURL, "phasical")
	if err != nil {
		return nil, err
	}
	apiKey, err := parsePhasicalSecret(secret)
	if err != nil {
		return nil, err
	}
	return &phasicalProvider{
		baseURL: normalizePhasicalAPIBase(root),
		apiKey:  apiKey,
		client:  client,
	}, nil
}

func (p *phasicalProvider) ListWorkspaces(ctx context.Context) ([]Item, error) {
	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := p.doJSON(ctx, http.MethodGet, "/auth/organization/list", nil, &workspaces); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(workspaces))
	for _, workspace := range workspaces {
		id := strings.TrimSpace(workspace.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(workspace.Name)
		if name == "" {
			name = strings.TrimSpace(workspace.Slug)
		}
		if name == "" {
			name = id
		}
		items = append(items, Item{ID: id, Name: name})
	}
	return items, nil
}

func (p *phasicalProvider) ListProjects(ctx context.Context, workspaceID string) ([]Item, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, errors.New("phasical: workspaceId query parameter is required")
	}

	path := "/project?workspaceId=" + url.QueryEscape(workspaceID)
	var projects []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := p.doJSON(ctx, http.MethodGet, path, nil, &projects); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(projects))
	for _, project := range projects {
		id := strings.TrimSpace(project.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(project.Name)
		if name == "" {
			name = strings.TrimSpace(project.Slug)
		}
		if name == "" {
			name = id
		}
		label := name
		if slug := strings.TrimSpace(project.Slug); slug != "" && slug != name {
			label = fmt.Sprintf("%s (%s)", name, slug)
		}
		items = append(items, Item{ID: id, Name: name, Label: label})
	}
	return items, nil
}

func (p *phasicalProvider) ListStatuses(ctx context.Context, externalProjectID string) ([]Item, error) {
	externalProjectID = strings.TrimSpace(externalProjectID)
	if externalProjectID == "" {
		return nil, errors.New("phasical: externalProjectId query parameter is required")
	}

	path := "/column/" + url.PathEscape(externalProjectID)
	body, err := p.doRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	columns, err := decodePhasicalColumns(body)
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(columns))
	seen := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		id := strings.TrimSpace(column.Slug)
		if id == "" {
			id = strings.TrimSpace(column.ID)
		}
		if id == "" {
			id = slugify(column.Name)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		name := strings.TrimSpace(column.Name)
		if name == "" {
			name = id
		}
		items = append(items, Item{ID: id, Name: name})
	}
	return items, nil
}

func (p *phasicalProvider) ListPriorities(_ context.Context, _ string) ([]Item, error) {
	return []Item{
		{ID: "no-priority", Name: "No priority"},
		{ID: "low", Name: "Low"},
		{ID: "medium", Name: "Medium"},
		{ID: "high", Name: "High"},
		{ID: "urgent", Name: "Urgent"},
	}, nil
}

func (p *phasicalProvider) ListIssueTypes(context.Context, string) ([]Item, error) {
	return nil, ErrUnsupported
}

type phasicalColumn struct {
	ID   string
	Name string
	Slug string
}

func decodePhasicalColumns(body []byte) ([]phasicalColumn, error) {
	var direct []map[string]any
	if err := json.Unmarshal(body, &direct); err == nil && len(direct) > 0 {
		return mapPhasicalColumns(direct), nil
	}

	var wrapped struct {
		Data struct {
			Columns []map[string]any `json:"columns"`
		} `json:"data"`
		Columns []map[string]any `json:"columns"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("decode phasical columns: %w", err)
	}

	switch {
	case len(wrapped.Data.Columns) > 0:
		return mapPhasicalColumns(wrapped.Data.Columns), nil
	case len(wrapped.Columns) > 0:
		return mapPhasicalColumns(wrapped.Columns), nil
	default:
		return nil, errors.New("phasical: columns response was empty")
	}
}

func mapPhasicalColumns(raw []map[string]any) []phasicalColumn {
	columns := make([]phasicalColumn, 0, len(raw))
	for _, item := range raw {
		column := phasicalColumn{
			ID:   stringValue(item["id"]),
			Name: stringValue(item["name"]),
			Slug: stringValue(item["slug"]),
		}
		if column.Slug == "" {
			column.Slug = stringValue(item["status"])
		}
		if column.Name != "" || column.Slug != "" || column.ID != "" {
			columns = append(columns, column)
		}
	}
	return columns
}

func (p *phasicalProvider) doJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	body, err := p.doRaw(ctx, method, path, reqBody)
	if err != nil {
		return err
	}
	if respBody == nil {
		return nil
	}
	if err := json.Unmarshal(body, respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (p *phasicalProvider) doRaw(ctx context.Context, method, path string, reqBody any) ([]byte, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	endpoint := p.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "release-ops")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return payload, nil
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func slugify(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
