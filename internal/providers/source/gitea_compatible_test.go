package source_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestGiteaCompatibleSourceInvalidProjectPath(t *testing.T) {
	t.Parallel()

	provider, err := source.NewGiteaCompatibleSource("https://gitea.example", "", nil)
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	_, err = provider.GetLatestRelease(context.Background(), "not-valid")
	if err == nil {
		t.Fatal("expected error for invalid project_path")
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Fatalf("error = %v, want owner/repo mention", err)
	}
}

func TestGiteaCompatibleSourceServerError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaCompatibleSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	_, err = provider.GetLatestRelease(context.Background(), "acme/widget")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "unexpected status 500") {
		t.Fatalf("error = %v, want unexpected status mention", err)
	}
}

func TestGiteaCompatibleSourceMalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaCompatibleSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	_, err = provider.GetLatestRelease(context.Background(), "acme/widget")
	if err == nil {
		t.Fatal("expected decode error")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %v, want decode response mention", err)
	}
}

func TestGiteaCompatibleSourceInvalidPublishedAt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.0.0",
			"name": "Release",
			"html_url": "https://gitea.example/acme/widget/releases/tag/v1.0.0",
			"published_at": "not-a-timestamp"
		}`))
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaCompatibleSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	_, err = provider.GetLatestRelease(context.Background(), "acme/widget")
	if err == nil {
		t.Fatal("expected published_at parse error")
	}
	if !strings.Contains(err.Error(), "published_at") {
		t.Fatalf("error = %v, want published_at mention", err)
	}
}

func TestGiteaCompatibleSourceRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	provider, err := source.NewGiteaCompatibleSource(srv.URL, "", srv.Client())
	if err != nil {
		t.Fatalf("NewGiteaCompatibleSource: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = provider.GetLatestRelease(ctx, "acme/widget")
	if err == nil {
		t.Fatal("expected context error")
	}
}
