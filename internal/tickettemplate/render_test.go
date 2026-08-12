package tickettemplate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/source"
	"github.com/mdg-labs/release-ops/internal/store"
	"github.com/mdg-labs/release-ops/internal/tickettemplate"
)

func testContext() tickettemplate.Context {
	published := time.Date(2026, 8, 7, 10, 30, 0, 0, time.UTC)
	repo := store.MonitoredRepo{
		SourceKind:  source.KindGitHub,
		ProjectPath: "org/repo",
	}
	lastKnown := "v1.0.0"
	repo.LastKnownTag = &lastKnown
	release := &source.Release{
		Tag:          "v2.0.0",
		Name:         "Summer Release",
		URL:          "https://github.com/org/repo/releases/tag/v2.0.0",
		Notes:        "Changelog body",
		PublishedAt:  published,
		IsPrerelease: true,
	}
	supersede := &tickettemplate.SupersedeContext{
		OldTag:       "v1.0.0",
		NewTag:       "v2.0.0",
		NewTicketURL: "https://tickets.example/new-ticket",
	}
	return tickettemplate.BuildContext(repo, release, "https://github.com/org/repo", supersede)
}

func TestRenderAllMVPVariables(t *testing.T) {
	t.Parallel()

	ctx := testContext()
	tmpl := tickettemplate.ContentTemplates{
		Title: `{{ .Repo.SourceKind }}|{{ .Repo.ProjectPath }}|{{ .Repo.URL }}|{{ .Release.Tag }}|{{ .Release.Name }}|{{ .Release.URL }}|{{ .Release.Notes }}|{{ .Release.PublishedAt }}|{{ yesNo .Release.IsPrerelease }}|{{ .Previous.Tag }}|{{ .Supersede.OldTag }}|{{ .Supersede.NewTag }}|{{ .Supersede.NewTicketURL }}`,
		Description: `{{ .Repo.SourceKind }} {{ .Release.Tag }}`,
		SupersedeComment: `{{ .Supersede.NewTicketURL }}`,
	}
	renderer := tickettemplate.NewRenderer("phasical", tmpl)

	title, err := renderer.RenderTitle(ctx)
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	for _, want := range []string{
		"github",
		"org/repo",
		"https://github.com/org/repo",
		"v2.0.0",
		"Summer Release",
		"https://github.com/org/repo/releases/tag/v2.0.0",
		"Changelog body",
		"2026-08-07T10:30:00Z",
		"yes",
		"v1.0.0",
		"https://tickets.example/new-ticket",
	} {
		if !strings.Contains(title, want) {
			t.Fatalf("title = %q, missing %q", title, want)
		}
	}

	comment, err := renderer.RenderSupersedeComment(ctx)
	if err != nil {
		t.Fatalf("RenderSupersedeComment: %v", err)
	}
	if comment != "https://tickets.example/new-ticket" {
		t.Fatalf("comment = %q, want new ticket URL only", comment)
	}
}

func TestDefaultTemplatesWhenKeysEmpty(t *testing.T) {
	t.Parallel()

	ctx := testContext()
	renderer := tickettemplate.NewRenderer("phasical", tickettemplate.DefaultContentTemplates())

	title, err := renderer.RenderTitle(ctx)
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	if title != "Release: github org/repo v2.0.0" {
		t.Fatalf("title = %q", title)
	}

	description, err := renderer.RenderDescription(ctx)
	if err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
	for _, want := range []string{
		"**Release name:** Summer Release",
		"**URL:** https://github.com/org/repo/releases/tag/v2.0.0",
		"**Published:** 2026-08-07T10:30:00Z",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("description missing %q:\n%s", want, description)
		}
	}

	comment, err := renderer.RenderSupersedeComment(ctx)
	if err != nil {
		t.Fatalf("RenderSupersedeComment: %v", err)
	}
	if !strings.Contains(comment, "v1.0.0 → v2.0.0") {
		t.Fatalf("comment = %q, want supersede tags", comment)
	}
	if !strings.Contains(comment, "https://tickets.example/new-ticket") {
		t.Fatalf("comment = %q, want new ticket URL", comment)
	}
}

func TestJiraDefaultsUsePlainText(t *testing.T) {
	t.Parallel()

	ctx := testContext()
	renderer := tickettemplate.NewRenderer("jira", tickettemplate.DefaultContentTemplates())

	description, err := renderer.RenderDescription(ctx)
	if err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
	if strings.Contains(description, "**Release name:**") {
		t.Fatalf("jira default should be plain text:\n%s", description)
	}
	if !strings.Contains(description, "Release name: Summer Release") {
		t.Fatalf("description = %q", description)
	}
}

func TestValidateRejectsInvalidTemplate(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("phasical", tickettemplate.ContentTemplates{
		Title: "{{ .Release.Tag ",
	})
	if err := renderer.Validate(); err == nil {
		t.Fatal("Validate() should fail for invalid syntax")
	}
}

func TestRenderInvalidTemplateReturnsError(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("phasical", tickettemplate.ContentTemplates{
		Title: "{{ .Release.Tag ",
	})
	_, err := renderer.RenderTitle(testContext())
	if err == nil {
		t.Fatal("RenderTitle() should fail for invalid syntax")
	}
}

func TestTemplateHelperFunctions(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("phasical", tickettemplate.ContentTemplates{
		Title: `{{ formatRFC3339 .Release.PublishedAt }}|{{ formatDate .Release.PublishedAt }}|{{ yesNo .Release.IsPrerelease }}`,
	})
	title, err := renderer.RenderTitle(testContext())
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	if title != "2026-08-07T10:30:00Z|2026-08-07|yes" {
		t.Fatalf("title = %q", title)
	}
}

func TestParseContentTemplates(t *testing.T) {
	t.Parallel()

	got, err := tickettemplate.ParseContentTemplates(`{"title":"t","description":"d","supersedeComment":"c"}`)
	if err != nil {
		t.Fatalf("ParseContentTemplates: %v", err)
	}
	if got.Title != "t" || got.Description != "d" || got.SupersedeComment != "c" {
		t.Fatalf("parsed = %+v", got)
	}
}
