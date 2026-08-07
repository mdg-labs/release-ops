package ticket

import (
	"fmt"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
)

// BuildTitle returns the ticket title per specs §5.4:
// Release: {source_kind} {project_path} {tag}
func BuildTitle(sourceKind, projectPath, tag string) string {
	return fmt.Sprintf("Release: %s %s %s", sourceKind, projectPath, tag)
}

// BuildDescription returns markdown ticket body per specs §5.4:
// release name, URL, and publishedAt.
func BuildDescription(release source.Release) string {
	var b strings.Builder

	if name := strings.TrimSpace(release.Name); name != "" {
		fmt.Fprintf(&b, "**Release name:** %s\n\n", name)
	}

	if url := strings.TrimSpace(release.URL); url != "" {
		fmt.Fprintf(&b, "**URL:** %s\n\n", url)
	}

	if !release.PublishedAt.IsZero() {
		published := release.PublishedAt.UTC().Format(time.RFC3339)
		fmt.Fprintf(&b, "**Published:** %s\n", published)
	}

	return strings.TrimRight(b.String(), "\n")
}
