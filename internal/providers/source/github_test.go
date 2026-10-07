package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestGitHubSourceGetLatestReleaseSuccess(t *testing.T) {
	t.Parallel()

	const token = "ghp_test_token"
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/releases/latest" {
			t.Fatalf("path = %q, want /repos/acme/widget/releases/latest", r.URL.Path)
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("Accept = %q", r.Header.Get("Accept"))
		}
		if r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
			t.Fatalf("X-GitHub-Api-Version = %q", r.Header.Get("X-GitHub-Api-Version"))
		}
		if r.Header.Get("User-Agent") != "release-ops" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		gotAuth = r.Header.Get("Authorization")

		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name":     "v2.0.0",
			"name":         "Widget 2.0",
			"html_url":     "https://github.com/acme/widget/releases/tag/v2.0.0",
			"published_at": "2026-08-07T10:00:00Z",
			"body":         "## Changelog\n\n- feature",
			"prerelease":   false,
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource(token, client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if gotAuth != "Bearer "+token {
		t.Fatalf("Authorization = %q, want Bearer token from decrypted integration payload", gotAuth)
	}
	if release.Tag != "v2.0.0" {
		t.Fatalf("tag = %q, want v2.0.0", release.Tag)
	}
	if release.Name != "Widget 2.0" {
		t.Fatalf("name = %q, want Widget 2.0", release.Name)
	}
	if release.URL != "https://github.com/acme/widget/releases/tag/v2.0.0" {
		t.Fatalf("url = %q", release.URL)
	}
	wantPublished := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	if !release.PublishedAt.Equal(wantPublished) {
		t.Fatalf("publishedAt = %v, want %v", release.PublishedAt, wantPublished)
	}
	if release.Notes != "## Changelog\n\n- feature" {
		t.Fatalf("notes = %q", release.Notes)
	}
	if release.IsPrerelease {
		t.Fatal("IsPrerelease = true, want false for stable release")
	}
}

func TestGitHubSourceNoReleasesReturnsNil(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/empty", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release != nil {
		t.Fatalf("release = %+v, want nil for 404", release)
	}
}

func TestGitHubSourceRateLimitSurfaced(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		headers map[string]string
	}{
		{
			name:   "429 too many requests",
			status: http.StatusTooManyRequests,
		},
		{
			name:   "403 with zero remaining",
			status: http.StatusForbidden,
			headers: map[string]string{
				"X-RateLimit-Remaining": "0",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for key, value := range tc.headers {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(server.Close)

			client := newHostRewritingClient(server, "api.github.com")
			provider := source.NewGitHubSource("ghp_test", client)

			_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
			if err == nil {
				t.Fatal("expected rate limit error")
			}
			if !errors.Is(err, source.ErrRateLimited) {
				t.Fatalf("error = %v, want ErrRateLimited", err)
			}
		})
	}
}

func TestGitHubSourceInvalidProjectPath(t *testing.T) {
	t.Parallel()

	provider := source.NewGitHubSource("", nil)

	_, err := provider.GetLatestRelease(context.Background(), "not-a-valid-path", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected error for invalid project_path")
	}
}

func TestGitHubSourceRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.GetLatestRelease(ctx, "acme/widget", source.ReleaseOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestGitHubSourceServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("error = %v, want unexpected status mention", err)
	}
}

func TestGitHubSourceMalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %v, want decode response mention", err)
	}
}

func TestGitHubSourceInvalidPublishedAt(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name":     "v1.0.0",
			"html_url":     "https://github.com/acme/widget/releases/tag/v1.0.0",
			"published_at": "not-a-timestamp",
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected published_at parse error")
	}
	if !strings.Contains(err.Error(), "published_at") {
		t.Fatalf("error = %v, want published_at mention", err)
	}
}

func TestGitHubSourceEmptyPublishedAtAllowed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v1.0.0",
			"html_url": "https://github.com/acme/widget/releases/tag/v1.0.0",
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release.Tag != "v1.0.0" {
		t.Fatalf("tag = %q, want v1.0.0", release.Tag)
	}
	if !release.PublishedAt.IsZero() {
		t.Fatalf("publishedAt = %v, want zero time", release.PublishedAt)
	}
}

func TestGitHubSourceIncludePrereleasesPicksNewestByPublishedAt(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widget/releases" {
			t.Fatalf("path = %q, want /repos/acme/widget/releases", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name":     "v1.0.0",
				"name":         "Stable",
				"html_url":     "https://github.com/acme/widget/releases/tag/v1.0.0",
				"published_at": "2026-08-01T10:00:00Z",
				"draft":        false,
				"prerelease":   false,
			},
			{
				"tag_name":     "v2.0.0-beta.1",
				"name":         "Beta",
				"html_url":     "https://github.com/acme/widget/releases/tag/v2.0.0-beta.1",
				"published_at": "2026-08-07T10:00:00Z",
				"draft":        false,
				"prerelease":   true,
			},
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "api.github.com")
	provider := source.NewGitHubSource("", client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{
		IncludePrereleases: true,
	})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release.Tag != "v2.0.0-beta.1" {
		t.Fatalf("tag = %q, want v2.0.0-beta.1", release.Tag)
	}
	if !release.IsPrerelease {
		t.Fatal("IsPrerelease = false, want true for pre-release payload")
	}
}

