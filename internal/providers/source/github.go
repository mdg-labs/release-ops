package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	githubAPIBase     = "https://api.github.com"
	githubUserAgent   = "release-ops"
	githubAPIVersion  = "2022-11-28"
	githubAcceptMedia = "application/vnd.github+json"
)

// ErrRateLimited is returned when a source API rejects the request due to rate limiting.
var ErrRateLimited = errors.New("rate limit exceeded")

// GitHubSource fetches latest releases from api.github.com.
// Token is optional but recommended; pass the decrypted integration payload token field.
type GitHubSource struct {
	client *http.Client
	token  string
}

// NewGitHubSource returns a GitHub source provider.
// client may be nil to use http.DefaultClient (useful for tests with httptest).
func NewGitHubSource(token string, client *http.Client) *GitHubSource {
	if client == nil {
		client = http.DefaultClient
	}
	return &GitHubSource{
		client: client,
		token:  strings.TrimSpace(token),
	}
}

type githubReleaseResponse struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
}

func releaseFromGitHub(payload githubReleaseResponse) (Release, error) {
	publishedAt, err := parsePublishedAt(payload.PublishedAt)
	if err != nil {
		return Release{}, err
	}
	return Release{
		Tag:          payload.TagName,
		Name:         payload.Name,
		URL:          payload.HTMLURL,
		PublishedAt:  publishedAt,
		Notes:        payload.Body,
		IsPrerelease: payload.Prerelease,
	}, nil
}

// GetLatestRelease implements SourceProvider.
func (g *GitHubSource) GetLatestRelease(ctx context.Context, projectPath string, opts ReleaseOptions) (*Release, error) {
	if opts.IncludePrereleases {
		return g.getLatestReleaseIncludingPrereleases(ctx, projectPath)
	}
	return g.getLatestStableRelease(ctx, projectPath)
}

func (g *GitHubSource) getLatestStableRelease(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateProjectPath(projectPath); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, projectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", githubAcceptMedia)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", githubUserAgent)
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err := checkGitHubRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}

	release, err := releaseFromGitHub(payload)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	return &release, nil
}

func (g *GitHubSource) getLatestReleaseIncludingPrereleases(ctx context.Context, projectPath string) (*Release, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateProjectPath(projectPath); err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/repos/%s/releases?per_page=100", githubAPIBase, projectPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", githubAcceptMedia)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", githubUserAgent)
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if err := checkGitHubRateLimit(resp); err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payloads []githubReleaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&payloads); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}

	candidates := make([]Release, 0, len(payloads))
	for _, payload := range payloads {
		if payload.Draft {
			continue
		}
		release, err := releaseFromGitHub(payload)
		if err != nil {
			return nil, fmt.Errorf("github: %w", err)
		}
		candidates = append(candidates, release)
	}

	return pickNewestRelease(candidates), nil
}

func checkGitHubRateLimit(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("github: %w", ErrRateLimited)
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return fmt.Errorf("github: %w", ErrRateLimited)
	}
	return nil
}

func validateProjectPath(projectPath string) error {
	parts := strings.Split(projectPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid project_path %q: want owner/repo", projectPath)
	}
	return nil
}

func parsePublishedAt(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse published_at %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}
