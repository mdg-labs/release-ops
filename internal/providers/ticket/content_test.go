package ticket_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

func TestBuildTitle(t *testing.T) {
	t.Parallel()

	got := ticket.BuildTitle(source.KindGitHub, "org/repo", "v1.2.3")
	want := "Release: github org/repo v1.2.3"
	if got != want {
		t.Fatalf("BuildTitle() = %q, want %q", got, want)
	}
}

func TestBuildDescriptionIncludesURLAndPublishedAt(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC)
	release := source.Release{
		Tag:         "v1.2.3",
		Name:        "Summer Release",
		URL:         "https://github.com/org/repo/releases/tag/v1.2.3",
		PublishedAt: published,
	}

	desc := ticket.BuildDescription(release)

	for _, want := range []string{
		"**Release name:** Summer Release",
		"**URL:** https://github.com/org/repo/releases/tag/v1.2.3",
		"**Published:** 2026-08-07T10:30:00Z",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("BuildDescription() missing %q:\n%s", want, desc)
		}
	}
}

func TestBuildDescriptionOmitsEmptyName(t *testing.T) {
	t.Parallel()

	release := source.Release{
		URL:         "https://example.com/release",
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	desc := ticket.BuildDescription(release)
	if strings.Contains(desc, "**Release name:**") {
		t.Fatalf("BuildDescription() should omit empty name:\n%s", desc)
	}
	if !strings.Contains(desc, "**URL:** https://example.com/release") {
		t.Fatalf("BuildDescription() missing URL:\n%s", desc)
	}
}
