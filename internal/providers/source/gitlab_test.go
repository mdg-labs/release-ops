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

func TestGitLabSourceGetLatestRelease(t *testing.T) {
	t.Parallel()

	const token = "gl-token"
	publishedAt := "2026-03-15T14:30:00Z"
	expectedTime, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != token {
			t.Errorf("PRIVATE-TOKEN = %q, want %q", r.Header.Get("PRIVATE-TOKEN"), token)
		}
		wantPath := "/api/v4/projects/group%2Fsubgroup%2Frepo/releases/permalink/latest"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path = %q, want %q", r.URL.EscapedPath(), wantPath)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v2.1.0",
			"name": "Release 2.1",
			"released_at": "` + publishedAt + `",
			"_links": { "self": "https://gitlab.example/group/subgroup/repo/-/releases/v2.1.0" }
		}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGitLabSource(srv.URL+"/", token, srv.Client())
	if err != nil {
		t.Fatalf("NewGitLabSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "group/subgroup/repo")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release == nil {
		t.Fatal("expected release, got nil")
	}
	if release.Tag != "v2.1.0" {
		t.Fatalf("tag = %q, want v2.1.0", release.Tag)
	}
	if release.Name != "Release 2.1" {
		t.Fatalf("name = %q, want Release 2.1", release.Name)
	}
	if release.URL != "https://gitlab.example/group/subgroup/repo/-/releases/v2.1.0" {
		t.Fatalf("url = %q", release.URL)
	}
	if !release.PublishedAt.Equal(expectedTime) {
		t.Fatalf("publishedAt = %v, want %v", release.PublishedAt, expectedTime)
	}
}

func TestGitLabSourceGetLatestReleaseNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGitLabSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGitLabSource: %v", err)
	}

	release, err := provider.GetLatestRelease(context.Background(), "group/repo")
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func TestGitLabSourceInvalidBaseURL(t *testing.T) {
	t.Parallel()

	_, err := source.NewGitLabSource("not-a-url", "token", nil)
	if err == nil {
		t.Fatal("expected error for invalid base_url")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v, want base_url mention", err)
	}
}

func TestGitLabSourceRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGitLabSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGitLabSource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = provider.GetLatestRelease(ctx, "group/repo")
	if err == nil {
		t.Fatal("expected context error")
	}
}
