package source

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	githubWebBase   = "https://github.com"
	codebergWebBase = "https://codeberg.org"
)

// Release is the normalized latest-release payload from a source provider.
// See specs §6 Provider interfaces (Go).
type Release struct {
	Tag          string
	Name         string
	URL          string
	PublishedAt  time.Time
	Notes        string
	IsPrerelease bool
}

// BuildRepoWebURL returns the browser URL for a monitored repository.
// integrationBaseURL is the source integration base_url (ignored for github and codeberg).
func BuildRepoWebURL(sourceKind, integrationBaseURL, projectPath string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", fmt.Errorf("project path is required")
	}
	if !IsValidKind(sourceKind) {
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, sourceKind)
	}

	switch sourceKind {
	case KindGitHub:
		return githubWebBase + "/" + projectPath, nil
	case KindCodeberg:
		return codebergWebBase + "/" + projectPath, nil
	case KindGitLab, KindGitea, KindForgejo:
		base, err := normalizeBaseURL(integrationBaseURL)
		if err != nil {
			return "", fmt.Errorf("integration base_url: %w", err)
		}
		return base + "/" + projectPath, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, sourceKind)
	}
}

// BuildReleaseWebURL returns the browser URL for a release tag on the source host.
// repoURL must be the repository web URL from BuildRepoWebURL.
func BuildReleaseWebURL(sourceKind, repoURL, tag string) (string, error) {
	repoURL = strings.TrimRight(strings.TrimSpace(repoURL), "/")
	tag = strings.TrimSpace(tag)
	if repoURL == "" {
		return "", fmt.Errorf("repo url is required")
	}
	if tag == "" {
		return "", fmt.Errorf("tag is required")
	}
	if !IsValidKind(sourceKind) {
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, sourceKind)
	}

	escapedTag := url.PathEscape(tag)
	switch sourceKind {
	case KindGitLab:
		return repoURL + "/-/releases/" + escapedTag, nil
	case KindGitHub, KindGitea, KindForgejo, KindCodeberg:
		return repoURL + "/releases/tag/" + escapedTag, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, sourceKind)
	}
}

// ReleaseOptions configures how GetLatestRelease resolves the newest release.
type ReleaseOptions struct {
	IncludePrereleases bool
}

// SourceProvider fetches the latest release for a monitored repository.
//
// GetLatestRelease returns:
//   - (*Release, nil) when the provider responds successfully but the repo has no release
//   - (non-nil Release, nil) when a release exists
//   - (nil, err) on transport, auth, or other operational errors
//
// When opts.IncludePrereleases is false (default), providers use each host's "latest"
// endpoint, which excludes pre-releases. When true, providers list releases and return
// the newest non-draft release by published_at (including pre-releases).
//
// Implementations must respect ctx cancellation and return ctx.Err() promptly.
type SourceProvider interface {
	GetLatestRelease(ctx context.Context, projectPath string, opts ReleaseOptions) (*Release, error)
}
