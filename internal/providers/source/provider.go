package source

import (
	"context"
	"time"
)

// Release is the normalized latest-release payload from a source provider.
// See specs §6 Provider interfaces (Go).
type Release struct {
	Tag         string
	Name        string
	URL         string
	PublishedAt time.Time
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
