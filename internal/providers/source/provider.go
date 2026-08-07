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

// SourceProvider fetches the latest release for a monitored repository.
//
// GetLatestRelease returns:
//   - (*Release, nil) when the provider responds successfully but the repo has no release
//   - (non-nil Release, nil) when a release exists
//   - (nil, err) on transport, auth, or other operational errors
//
// Implementations must respect ctx cancellation and return ctx.Err() promptly.
type SourceProvider interface {
	GetLatestRelease(ctx context.Context, projectPath string) (*Release, error)
}
