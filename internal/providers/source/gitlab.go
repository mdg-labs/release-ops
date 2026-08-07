package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const gitLabUserAgent = "release-ops"

// GitLabSource fetches latest releases from a GitLab instance (API v4).
type GitLabSource struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewGitLabSource returns a GitLab source provider for the given instance base URL.
func NewGitLabSource(baseURL, token string, client *http.Client) (*GitLabSource, error) {
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("gitlab base_url: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &GitLabSource{
		baseURL: normalized,
		token:   token,
		client:  client,
	}, nil
}

// GetLatestRelease fetches the latest release for a GitLab project path (namespace/project).
func (g *GitLabSource) GetLatestRelease(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("gitlab: project path is required")
	}

	encodedPath := url.PathEscape(projectPath)
	apiPath := fmt.Sprintf("/api/v4/projects/%s/releases/permalink/latest", encodedPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("gitlab: build request: %w", err)
	}
	// Preserve encoded slashes in the GitLab project path (net/http decodes %2F by default).
	req.URL.Opaque = "//" + req.URL.Host + apiPath
	endpoint := g.baseURL + apiPath
	req.Header.Set("User-Agent", gitLabUserAgent)
	if g.token != "" {
		req.Header.Set("PRIVATE-TOKEN", g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitlab: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gitlab: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload gitLabRelease
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("gitlab: decode response: %w", err)
	}

	releaseURL := payload.releaseURL()
	if releaseURL == "" {
		releaseURL = endpoint
	}

	publishedAt, err := time.Parse(time.RFC3339, payload.ReleasedAt)
	if err != nil {
		return nil, fmt.Errorf("gitlab: parse released_at: %w", err)
	}

	return &Release{
		Tag:         payload.TagName,
		Name:        payload.Name,
		URL:         releaseURL,
		PublishedAt: publishedAt,
	}, nil
}

type gitLabRelease struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	ReleasedAt string `json:"released_at"`
	Links      struct {
		Self string `json:"self"`
	} `json:"_links"`
	Assets struct {
		Links []struct {
			URL string `json:"url"`
		} `json:"links"`
	} `json:"assets"`
}

func (r gitLabRelease) releaseURL() string {
	if r.Links.Self != "" {
		return r.Links.Self
	}
	if len(r.Assets.Links) > 0 && r.Assets.Links[0].URL != "" {
		return r.Assets.Links[0].URL
	}
	return ""
}
