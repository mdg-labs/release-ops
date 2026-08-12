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

func TestCodebergSourceGetLatestReleaseSuccess(t *testing.T) {
	t.Parallel()

	const token = "cb_test_token"
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/acme/widget/releases/latest" {
			t.Fatalf("path = %q, want /api/v1/repos/acme/widget/releases/latest", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "release-ops" {
			t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		gotAuth = r.Header.Get("Authorization")

		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name":     "v1.5.0",
			"name":         "Widget 1.5",
			"html_url":     "https://codeberg.org/acme/widget/releases/tag/v1.5.0",
			"published_at": "2026-08-06T15:30:00Z",
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource(token, client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if gotAuth != "token "+token {
		t.Fatalf("Authorization = %q, want token from decrypted integration payload", gotAuth)
	}
	if release.Tag != "v1.5.0" {
		t.Fatalf("tag = %q, want v1.5.0", release.Tag)
	}
	if release.Name != "Widget 1.5" {
		t.Fatalf("name = %q, want Widget 1.5", release.Name)
	}
	if release.URL != "https://codeberg.org/acme/widget/releases/tag/v1.5.0" {
		t.Fatalf("url = %q", release.URL)
	}
	wantPublished := time.Date(2026, 8, 6, 15, 30, 0, 0, time.UTC)
	if !release.PublishedAt.Equal(wantPublished) {
		t.Fatalf("publishedAt = %v, want %v", release.PublishedAt, wantPublished)
	}
}

func TestCodebergSourceNoReleasesReturnsNil(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("", client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/empty", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release != nil {
		t.Fatalf("release = %+v, want nil for 404", release)
	}
}

func TestCodebergSourceRateLimitSurfaced(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("cb_test", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if !errors.Is(err, source.ErrRateLimited) {
		t.Fatalf("error = %v, want ErrRateLimited", err)
	}
}

func TestCodebergSourceInvalidProjectPath(t *testing.T) {
	t.Parallel()

	provider := source.NewCodebergSource("", nil)

	_, err := provider.GetLatestRelease(context.Background(), "missing-repo-segment", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected error for invalid project_path")
	}
}

func TestCodebergSourceRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("", client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := provider.GetLatestRelease(ctx, "acme/widget", source.ReleaseOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestCodebergSourceWorksWithoutToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Fatalf("Authorization = %q, want empty for anonymous access", auth)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name":     "v0.1.0",
			"html_url":     "https://codeberg.org/acme/widget/releases/tag/v0.1.0",
			"published_at": "2026-01-01T00:00:00Z",
		})
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("", client)

	release, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err != nil {
		t.Fatalf("GetLatestRelease: %v", err)
	}
	if release.Tag != "v0.1.0" {
		t.Fatalf("tag = %q, want v0.1.0", release.Tag)
	}
}

func TestCodebergSourceServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("error = %v, want unexpected status mention", err)
	}
}

func TestCodebergSourceMalformedJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(server.Close)

	client := newHostRewritingClient(server, "codeberg.org")
	provider := source.NewCodebergSource("", client)

	_, err := provider.GetLatestRelease(context.Background(), "acme/widget", source.ReleaseOptions{})
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %v, want decode response mention", err)
	}
}
