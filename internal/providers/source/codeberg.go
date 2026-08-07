package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const codebergBaseURL = "https://codeberg.org"

// CodebergSource fetches latest releases from codeberg.org via the Gitea-compatible API.
// Token is optional but recommended; pass the decrypted integration payload token field.
type CodebergSource struct {
	client *http.Client
	token  string
}

// NewCodebergSource returns a Codeberg source provider.
// client may be nil to use http.DefaultClient (useful for tests with httptest).
func NewCodebergSource(token string, client *http.Client) *CodebergSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &CodebergSource{
		client: client,
		token:  strings.TrimSpace(token),
	}
}

type giteaReleaseResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

// GetLatestRelease implements SourceProvider.
func (c *CodebergSource) GetLatestRelease(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, repo, err := splitProjectPath(projectPath)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/releases/latest", codebergBaseURL, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("codeberg: build request: %w", err)
	}
	req.Header.Set("User-Agent", githubUserAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "token "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("codeberg: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("codeberg: %w", ErrRateLimited)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("codeberg: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload giteaReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("codeberg: decode response: %w", err)
	}

	publishedAt, err := parsePublishedAt(payload.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("codeberg: %w", err)
	}

	return &Release{
		Tag:         payload.TagName,
		Name:        payload.Name,
		URL:         payload.HTMLURL,
		PublishedAt: publishedAt,
	}, nil
}

func splitProjectPath(projectPath string) (owner, repo string, err error) {
	parts := strings.Split(projectPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid project_path %q: want owner/repo", projectPath)
	}
	return parts[0], parts[1], nil
}
