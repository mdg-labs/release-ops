package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const giteaUserAgent = "release-ops"

// GiteaCompatibleSource fetches latest releases from Gitea-compatible APIs (Gitea, Forgejo, Codeberg).
type GiteaCompatibleSource struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewGiteaCompatibleSource returns a source provider for a Gitea-compatible instance.
func NewGiteaCompatibleSource(baseURL, token string, client *http.Client) (*GiteaCompatibleSource, error) {
	normalized, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("gitea-compatible base_url: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &GiteaCompatibleSource{
		baseURL: normalized,
		token:   token,
		client:  client,
	}, nil
}

// GetLatestRelease fetches the latest release for an owner/repo project path.
func (g *GiteaCompatibleSource) GetLatestRelease(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	owner, repo, err := splitOwnerRepo(projectPath)
	if err != nil {
		return nil, fmt.Errorf("gitea-compatible: %w", err)
	}

	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/%s/releases/latest", g.baseURL, owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("gitea-compatible: build request: %w", err)
	}
	req.Header.Set("User-Agent", giteaUserAgent)
	if g.token != "" {
		req.Header.Set("Authorization", "token "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gitea-compatible: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("gitea-compatible: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload giteaRelease
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("gitea-compatible: decode response: %w", err)
	}

	publishedAt, err := time.Parse(time.RFC3339, payload.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("gitea-compatible: parse published_at: %w", err)
	}

	return &Release{
		Tag:         payload.TagName,
		Name:        payload.Name,
		URL:         payload.HTMLURL,
		PublishedAt: publishedAt,
	}, nil
}

type giteaRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
}

func normalizeBaseURL(raw string) (string, error) {
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

	normalized := strings.TrimRight(raw, "/")
	return normalized, nil
}

func splitOwnerRepo(projectPath string) (owner, repo string, err error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", "", errors.New("project path is required")
	}

	parts := strings.Split(projectPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("project path must be owner/repo, got %q", projectPath)
	}
	return parts[0], parts[1], nil
}
