package metadata

import (
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

type jiraProvider struct {
	baseURL  string
	email    string
	apiToken string
	client   *http.Client
}

func newJiraProvider(baseURL *string, secret []byte, client *http.Client) (*jiraProvider, error) {
	root, err := requireBaseURL(baseURL, "jira")
	if err != nil {
		return nil, err
	}
	email, apiToken, err := parseJiraSecret(secret)
	if err != nil {
		return nil, err
	}
	return &jiraProvider{
		baseURL:  root,
		email:    email,
		apiToken: apiToken,
		client:   client,
	}, nil
}

func (j *jiraProvider) ListWorkspaces(context.Context) ([]Item, error) {
	return nil, ErrUnsupported
}

func (j *jiraProvider) ListProjects(ctx context.Context, _ string) ([]Item, error) {
	var response struct {
		Values []struct {
			ID   string `json:"id"`
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"values"`
	}
	path := "/rest/api/3/project/search?maxResults=100"
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(response.Values))
	for _, project := range response.Values {
		key := strings.TrimSpace(project.Key)
		if key == "" {
			continue
		}
		name := strings.TrimSpace(project.Name)
		if name == "" {
			name = key
		}
		items = append(items, Item{
			ID:    key,
			Name:  name,
			Label: fmt.Sprintf("%s (%s)", name, key),
		})
	}
	return items, nil
}

func (j *jiraProvider) ListStatuses(ctx context.Context, externalProjectID string) ([]Item, error) {
	externalProjectID = strings.TrimSpace(externalProjectID)
	if externalProjectID == "" {
		return nil, errors.New("jira: externalProjectId query parameter is required")
	}

	path := "/rest/api/3/project/" + url.PathEscape(externalProjectID) + "/statuses"
	var issueTypes []struct {
		Name     string `json:"name"`
		Statuses []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"statuses"`
	}
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &issueTypes); err != nil {
		return nil, err
	}

	items := make([]Item, 0)
	seen := make(map[string]struct{})
	for _, issueType := range issueTypes {
		for _, status := range issueType.Statuses {
			name := strings.TrimSpace(status.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[strings.ToLower(name)]; ok {
				continue
			}
			seen[strings.ToLower(name)] = struct{}{}
			id := strings.TrimSpace(status.ID)
			if id == "" {
				id = name
			}
			items = append(items, Item{ID: id, Name: name, Label: name})
		}
	}
	return items, nil
}

func (j *jiraProvider) ListPriorities(ctx context.Context, _ string) ([]Item, error) {
	var priorities []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := j.doJSON(ctx, http.MethodGet, "/rest/api/3/priority", nil, &priorities); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(priorities))
	for _, priority := range priorities {
		name := strings.TrimSpace(priority.Name)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(priority.ID)
		if id == "" {
			id = name
		}
		items = append(items, Item{ID: id, Name: name})
	}
	return items, nil
}

func (j *jiraProvider) ListIssueTypes(ctx context.Context, externalProjectID string) ([]Item, error) {
	externalProjectID = strings.TrimSpace(externalProjectID)
	if externalProjectID == "" {
		return nil, errors.New("jira: externalProjectId query parameter is required")
	}

	path := "/rest/api/3/project/" + url.PathEscape(externalProjectID) + "/statuses"
	var issueTypes []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &issueTypes); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(issueTypes))
	for _, issueType := range issueTypes {
		name := strings.TrimSpace(issueType.Name)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(issueType.ID)
		if id == "" {
			id = name
		}
		items = append(items, Item{ID: id, Name: name})
	}
	return items, nil
}

func (j *jiraProvider) doJSON(ctx context.Context, method, path string, reqBody any, respBody any) error {
	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		bodyReader = strings.NewReader(string(encoded))
	}

	endpoint := j.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "release-ops")
	req.Header.Set("Authorization", "Basic "+j.basicAuth())

	resp, err := j.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if respBody == nil {
		return nil
	}
	if err := json.Unmarshal(payload, respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (j *jiraProvider) basicAuth() string {
	credentials := j.email + ":" + j.apiToken
	return base64.StdEncoding.EncodeToString([]byte(credentials))
}
