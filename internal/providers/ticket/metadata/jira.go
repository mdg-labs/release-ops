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
	"strconv"
	"strings"
)

// jiraProjectPageSize is the page size for GET /rest/api/3/project/search; jiraMaxProjectPages
// bounds the pagination loop.
const (
	jiraProjectPageSize = 100
	jiraMaxProjectPages = 50
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
	items := make([]Item, 0)
	startAt := 0
	for page := 0; page < jiraMaxProjectPages; page++ {
		var response struct {
			Values []struct {
				ID   string `json:"id"`
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"values"`
			Total  int  `json:"total"`
			IsLast bool `json:"isLast"`
		}
		path := "/rest/api/3/project/search?startAt=" + strconv.Itoa(startAt) +
			"&maxResults=" + strconv.Itoa(jiraProjectPageSize)
		if err := j.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}

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

		startAt += len(response.Values)
		if response.IsLast || len(response.Values) == 0 || startAt >= response.Total {
			break
		}
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
			// Status mapping and initialStatus store status names (specs §5.3) — the ticket
			// provider reads fields.status.name and matches transitions by name.
			items = append(items, Item{ID: name, Name: name, Label: name})
		}
	}
	return items, nil
}

func (j *jiraProvider) ListPriorities(ctx context.Context, _ string) ([]Item, error) {
	var priorities []struct {
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
		// create_config.priority is a priority name (specs §5.3).
		items = append(items, Item{ID: name, Name: name})
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
		Name    string `json:"name"`
		Subtask bool   `json:"subtask"`
	}
	if err := j.doJSON(ctx, http.MethodGet, path, nil, &issueTypes); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(issueTypes))
	seen := make(map[string]struct{})
	for _, issueType := range issueTypes {
		name := strings.TrimSpace(issueType.Name)
		// Sub-task types need a parent issue and cannot be created standalone.
		if name == "" || issueType.Subtask {
			continue
		}
		if _, ok := seen[strings.ToLower(name)]; ok {
			continue
		}
		seen[strings.ToLower(name)] = struct{}{}
		// create_config.issueType is an issue type name (specs §5.3).
		items = append(items, Item{ID: name, Name: name})
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
