package source_test

import (
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

func TestBuildRepoWebURLGitHub(t *testing.T) {
	t.Parallel()

	url, err := source.BuildRepoWebURL(source.KindGitHub, "", "acme/widget")
	if err != nil {
		t.Fatalf("BuildRepoWebURL: %v", err)
	}
	if url != "https://github.com/acme/widget" {
		t.Fatalf("url = %q, want https://github.com/acme/widget", url)
	}
}

func TestBuildRepoWebURLCodeberg(t *testing.T) {
	t.Parallel()

	url, err := source.BuildRepoWebURL(source.KindCodeberg, "", "acme/widget")
	if err != nil {
		t.Fatalf("BuildRepoWebURL: %v", err)
	}
	if url != "https://codeberg.org/acme/widget" {
		t.Fatalf("url = %q, want https://codeberg.org/acme/widget", url)
	}
}

func TestBuildRepoWebURLGitLab(t *testing.T) {
	t.Parallel()

	url, err := source.BuildRepoWebURL(source.KindGitLab, "https://gitlab.example/", "group/subgroup/repo")
	if err != nil {
		t.Fatalf("BuildRepoWebURL: %v", err)
	}
	if url != "https://gitlab.example/group/subgroup/repo" {
		t.Fatalf("url = %q", url)
	}
}

func TestBuildRepoWebURLGiteaForgejo(t *testing.T) {
	t.Parallel()

	for _, kind := range []string{source.KindGitea, source.KindForgejo} {
		kind := kind
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			url, err := source.BuildRepoWebURL(kind, "https://forge.example", "owner/repo")
			if err != nil {
				t.Fatalf("BuildRepoWebURL: %v", err)
			}
			if url != "https://forge.example/owner/repo" {
				t.Fatalf("url = %q", url)
			}
		})
	}
}

func TestBuildRepoWebURLRequiresProjectPath(t *testing.T) {
	t.Parallel()

	_, err := source.BuildRepoWebURL(source.KindGitHub, "", "  ")
	if err == nil {
		t.Fatal("expected error for empty project path")
	}
	if !strings.Contains(err.Error(), "project path is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildRepoWebURLGitLabRequiresBaseURL(t *testing.T) {
	t.Parallel()

	_, err := source.BuildRepoWebURL(source.KindGitLab, "", "group/repo")
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
	if !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("error = %v", err)
	}
}
