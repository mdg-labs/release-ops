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

func TestGiteaSourceGetLatestRelease(t *testing.T) {
	t.Parallel()

	const token = "gitea-token"
	publishedAt := "2026-04-01T09:00:00Z"
	expectedTime, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "token "+token {
			t.Errorf("Authorization = %q, want token %q", auth, token)
		}
		wantPath := "/api/v1/repos/acme/widget/releases/latest"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v3.0.0",
			"name": "Widget 3.0",
			"html_url": "https://gitea.example/acme/widget/releases/tag/v3.0.0",
			"published_at": "` + publishedAt + `"
		}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaSource(srv.URL+"/", token, srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release == nil {
		t.Fatal("expected release, got nil")
	}
	if release.Tag != "v3.0.0" {
		t.Fatalf("tag = %q, want v3.0.0", release.Tag)
	}
	if release.Name != "Widget 3.0" {
		t.Fatalf("name = %q, want Widget 3.0", release.Name)
	}
	if release.URL != "https://gitea.example/acme/widget/releases/tag/v3.0.0" {
		t.Fatalf("url = %q", release.URL)
	}
	if !release.PublishedAt.Equal(expectedTime) {
		t.Fatalf("publishedAt = %v, want %v", release.PublishedAt, expectedTime)
	}
}

func TestGiteaSourceGetLatestReleaseNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func TestGiteaSourceInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := source.NewGiteaSource("", "token", nil)
	if err == nil {
		t.Fatal("expected error for empty base_url")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url mention", err)
	}
}

func TestGiteaCompatibleSourceUsedForGitea(t *testing.T) {
	t.Parallel()

	provider, err := source.NewGiteaSource("https://gitea.example.com", "token", nil)
	if err != nil {
		t.Fatalf("NewGiteaSource: %v", err)
	}
	if _, ok := provider.(*source.GiteaCompatibleSource); !ok {
		t.Fatalf("provider type = %T, want *source.GiteaCompatibleSource", provider)
	}
}
