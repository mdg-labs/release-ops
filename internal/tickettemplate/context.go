package tickettemplate

import (
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/store"
)

// Context is the root template data object (specs §5.4).
type Context struct {
	Repo      RepoContext
	Release   ReleaseContext
	Previous  PreviousContext
	Supersede SupersedeContext
}

// RepoContext holds monitored repository fields available in templates.
type RepoContext struct {
	SourceKind  string
	ProjectPath string
	URL         string
}

// ReleaseContext holds normalized release fields for templates.
type ReleaseContext struct {
	Tag          string
	Name         string
	URL          string
	Notes        string
	PublishedAt  string
	IsPrerelease bool
}

// PreviousContext holds poll state before the current action.
type PreviousContext struct {
	Tag string
}

// SupersedeContext is populated for supersede comment rendering (specs §5.2.1).
type SupersedeContext struct {
	OldTag       string
	NewTag       string
	NewTicketURL string
}

// BuildContext assembles template context for create, merge, or supersede (new ticket).
func BuildContext(
	repo store.MonitoredRepo,
	release *source.Release,
	repoWebURL string,
	supersede *SupersedeContext,
) Context {
	var prevTag string
	if repo.LastKnownTag != nil {
		prevTag = *repo.LastKnownTag
	}

	ctx := Context{
		Repo: RepoContext{
			SourceKind:  repo.SourceKind,
			ProjectPath: repo.ProjectPath,
			URL:         repoWebURL,
		},
		Previous: PreviousContext{Tag: prevTag},
	}
	if supersede != nil {
		ctx.Supersede = *supersede
	}
	if release != nil {
		ctx.Release = releaseContextFromRelease(*release)
	}
	return ctx
}

func releaseContextFromRelease(release source.Release) ReleaseContext {
	publishedAt := ""
	if !release.PublishedAt.IsZero() {
		publishedAt = release.PublishedAt.UTC().Format(time.RFC3339)
	}
	return ReleaseContext{
		Tag:          release.Tag,
		Name:         release.Name,
		URL:          release.URL,
		Notes:        release.Notes,
		PublishedAt:  publishedAt,
		IsPrerelease: release.IsPrerelease,
	}
}
