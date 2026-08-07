package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestForgejoSourceGetLatestRelease(t *testing.T) {
	t.Parallel()

	const token = "forgejo-token"
	publishedAt := "2026-05-20T16:45:00Z"
	expectedTime, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "token "+token {
			t.Errorf("Authorization = %q, want token %q", auth, token)
		}
		wantPath := "/api/v1/repos/lib/core/releases/latest"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.4.2",
			"name": "Core 1.4.2",
			"html_url": "https://forgejo.example/lib/core/releases/tag/v1.4.2",
			"published_at": "` + publishedAt + `"
		}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewForgejoSource(srv.URL, token, srv.Client())
	if err != nil {
		t.Fatalf("NewForgejoSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "lib/core")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release == nil {
		t.Fatal("expected release, got nil")
	}
	if release.Tag != "v1.4.2" {
		t.Fatalf("tag = %q, want v1.4.2", release.Tag)
	}
	if !release.PublishedAt.Equal(expectedTime) {
		t.Fatalf("publishedAt = %v, want %v", release.PublishedAt, expectedTime)
	}
}

func TestForgejoSourceGetLatestReleaseNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewForgejoSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewForgejoSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "lib/core")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func TestForgejoSourceInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := source.NewForgejoSource("ftp://forgejo.example", "token", nil)
	if err == nil {
		t.Fatal("expected error for invalid base_url scheme")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url mention", err)
	}
}

func TestForgejoCompatibleSourceUsesGiteaCompatibleClient(t *testing.T) {
	t.Parallel()

	provider, err := source.NewForgejoSource("https://forgejo.example.com", "token", nil)
	if err != nil {
		t.Fatalf("NewForgejoSource: %v", err)
	}
	if _, ok := provider.(*source.GiteaCompatibleSource); !ok {
		t.Fatalf("provider type = %T, want *source.GiteaCompatibleSource", provider)
	}
}

func TestGiteaCompatibleSourceNormalizesTrailingSlash(t *testing.T) {
	t.Parallel()

	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	baseURL := srv.URL + "/"
	provider, err := source.NewGiteaCompatibleSource(baseURL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	_, err = provider.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if gotHost == "" {
		t.Fatal("expected request to reach mock server")
	}
}
