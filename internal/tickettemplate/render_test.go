package tickettemplate_test

import (
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/tickettemplate"
)

func testContext() tickettemplate.Context {
	return tickettemplate.Context{
		PullRequest: tickettemplate.PullRequestContext{
			URL:    "https://github.com/org/repo/pull/42",
			Number: 42,
			Title:  "Update dependency foo to v2",
		},
		Repository: "org/repo",
		Dependencies: []tickettemplate.DependencyContext{
			{Name: "foo", From: "1.4.0", To: "2.0.0", UpdateType: "major"},
			{Name: "bar", From: "abc", To: "def"},
		},
		ReleaseNotes: "Changelog body",
		Supersede: tickettemplate.SupersedeContext{
			NewPullRequest: tickettemplate.PullRequestContext{URL: "https://github.com/org/repo/pull/43", Number: 43, Title: "Update foo to v3"},
			NewTicketURL:   "https://tickets.example/new-ticket",
		},
	}
}

func TestRenderAllVariables(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("kaneo", tickettemplate.ContentTemplates{
		Title: `{{ .PullRequest.URL }}|{{ .PullRequest.Number }}|{{ .PullRequest.Title }}|{{ .Repository }}|` +
			`{{ range .Dependencies }}{{ .Name }}:{{ .From }}>{{ .To }}:{{ .UpdateType }};{{ end }}|{{ .ReleaseNotes }}`,
		SupersedeComment: `{{ .Supersede.NewPullRequest.URL }}|{{ .Supersede.NewPullRequest.Number }}|{{ .Supersede.NewPullRequest.Title }}|{{ .Supersede.NewTicketURL }}`,
	})

	title, err := renderer.RenderTitle(testContext())
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	want := "https://github.com/org/repo/pull/42|42|Update dependency foo to v2|org/repo|foo:1.4.0>2.0.0:major;bar:abc>def:;|Changelog body"
	if title != want {
		t.Fatalf("title = %q, want %q", title, want)
	}

	comment, err := renderer.RenderSupersedeComment(testContext())
	if err != nil {
		t.Fatalf("RenderSupersedeComment: %v", err)
	}
	if want := "https://github.com/org/repo/pull/43|43|Update foo to v3|https://tickets.example/new-ticket"; comment != want {
		t.Fatalf("comment = %q, want %q", comment, want)
	}
}

func TestMarkdownDefaultsListEveryDependency(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("kaneo", tickettemplate.DefaultContentTemplates())
	title, err := renderer.RenderTitle(testContext())
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	if title != "Update dependency foo to v2 (org/repo)" {
		t.Fatalf("title = %q", title)
	}

	description, err := renderer.RenderDescription(testContext())
	if err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
	for _, want := range []string{
		"**Repository:** org/repo",
		"**Pull request:** [#42 Update dependency foo to v2](https://github.com/org/repo/pull/42)",
		"- foo: 1.4.0 → 2.0.0 (major)\n",
		"- bar: abc → def\n",
		"**Release notes:**\nChangelog body",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("description missing %q:\n%s", want, description)
		}
	}

	update, err := renderer.RenderUpdateComment(testContext())
	if err != nil {
		t.Fatalf("RenderUpdateComment: %v", err)
	}
	if !strings.Contains(update, "- foo: 1.4.0 → 2.0.0 (major)") || !strings.HasSuffix(update, "https://github.com/org/repo/pull/42") {
		t.Fatalf("update comment = %q", update)
	}

	comment, err := renderer.RenderSupersedeComment(testContext())
	if err != nil {
		t.Fatalf("RenderSupersedeComment: %v", err)
	}
	if comment != "Superseded by https://github.com/org/repo/pull/43\nNew ticket: https://tickets.example/new-ticket" {
		t.Fatalf("comment = %q", comment)
	}

	ctx := testContext()
	ctx.Supersede.NewTicketURL = ""
	comment, err = renderer.RenderSupersedeComment(ctx)
	if err != nil {
		t.Fatalf("RenderSupersedeComment: %v", err)
	}
	if comment != "Superseded by https://github.com/org/repo/pull/43" {
		t.Fatalf("comment without a ticket = %q, want the PR only", comment)
	}
}

func TestJiraDefaultsUsePlainText(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("jira", tickettemplate.DefaultContentTemplates())
	description, err := renderer.RenderDescription(testContext())
	if err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
	if strings.Contains(description, "**") || strings.Contains(description, "](") {
		t.Fatalf("jira default should be plain text:\n%s", description)
	}
	for _, want := range []string{
		"Repository: org/repo",
		"Pull request: #42 Update dependency foo to v2 (https://github.com/org/repo/pull/42)",
		"- foo: 1.4.0 → 2.0.0 (major)",
		"- bar: abc → def\n",
		"Release notes:\nChangelog body",
	} {
		if !strings.Contains(description, want) {
			t.Fatalf("description missing %q:\n%s", want, description)
		}
	}
}

func TestContentIsDataNotTemplateSource(t *testing.T) {
	t.Parallel()

	ctx := testContext()
	ctx.PullRequest.Title = "{{ .Repository }} {{ range .Dependencies }}x{{ end }}"
	ctx.ReleaseNotes = "{{ .PullRequest.URL }}"
	ctx.Dependencies[0].Name = "{{ .ReleaseNotes }}"
	got, err := tickettemplate.NewRenderer("kaneo", tickettemplate.DefaultContentTemplates()).RenderDescription(ctx)
	if err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
	for _, want := range []string{ctx.PullRequest.Title, "{{ .PullRequest.URL }}", "- {{ .ReleaseNotes }}: 1.4.0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("description should carry %q literally:\n%s", want, got)
		}
	}
}

func TestValidateRejectsInvalidSyntax(t *testing.T) {
	t.Parallel()

	for _, tmpl := range []tickettemplate.ContentTemplates{
		{Title: "{{ .PullRequest.Title "},
		{UpdateComment: "{{ range .Dependencies }}"},
	} {
		if err := tickettemplate.NewRenderer("kaneo", tmpl).Validate(); err == nil {
			t.Fatalf("Validate(%+v) should fail for invalid syntax", tmpl)
		}
	}
	if _, err := tickettemplate.NewRenderer("kaneo", tickettemplate.ContentTemplates{Title: "{{ .PullRequest.Title "}).RenderTitle(testContext()); err == nil {
		t.Fatal("RenderTitle() should fail for invalid syntax")
	}
}

func TestValidateRefusesTheVariablesOfTheReleaseContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, tmpl, want string
	}{
		{"repo source kind", "{{ .Repo.SourceKind }}", ".Repository"},
		{"repo url", "x {{ .Repo.URL }}", ".Repository"},
		{"release tag", "Release {{ .Release.Tag }}", ".Dependencies and .ReleaseNotes"},
		{"release notes", "{{ .Release.Notes }}", ".Dependencies and .ReleaseNotes"},
		{"previous tag", "{{ .Previous.Tag }}", ".Dependencies"},
		{"old tag", "{{ .Supersede.OldTag }}", ".PullRequest"},
		{"new tag", "{{ .Supersede.NewTag }}", ".Supersede.NewPullRequest"},
		{"in a branch not taken", "{{ if false }}{{ .Release.Tag }}{{ end }}", ".Release.Tag"},
		{"through the root inside a range", "{{ range .Dependencies }}{{ $.Release.Tag }}{{ end }}", ".Release.Tag"},
		{"a field of a dependency that is not there", "{{ range .Dependencies }}{{ .Version }}{{ end }}", ".Version"},
		{"a field below a string", "{{ .Repository.Owner }}", ".Repository.Owner"},
		{"a field of the list", "{{ .Dependencies.Name }}", ".Dependencies.Name"},
		{"a field of the pull request that is not there", "{{ .PullRequest.Branch }}", ".PullRequest.Branch"},
		{"a function argument", "{{ yesNo .Release.IsPrerelease }}", ".Release.IsPrerelease"},
	}
	for _, c := range cases {
		for _, key := range []string{"title", "description", "updateComment", "supersedeComment"} {
			tmpl := tickettemplate.ContentTemplates{}
			switch key {
			case "title":
				tmpl.Title = c.tmpl
			case "description":
				tmpl.Description = c.tmpl
			case "updateComment":
				tmpl.UpdateComment = c.tmpl
			default:
				tmpl.SupersedeComment = c.tmpl
			}
			err := tickettemplate.NewRenderer("kaneo", tmpl).Validate()
			if err == nil {
				t.Errorf("%s in %s: Validate() accepted %q", c.name, key, c.tmpl)
				continue
			}
			if !strings.Contains(err.Error(), "content_templates."+key) || !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s in %s: error %q should name the key and %q", c.name, key, err, c.want)
			}
		}
	}
}

func TestValidateAcceptsEveryVariableOfTheContract(t *testing.T) {
	t.Parallel()

	tmpl := tickettemplate.ContentTemplates{
		Title: "{{ .PullRequest.Title }} {{ .PullRequest.Number }} {{ .PullRequest.URL }} {{ .Repository }}",
		Description: "{{ range .Dependencies }}{{ .Name }} {{ .From }} {{ .To }} {{ .UpdateType }} {{ $.Repository }}{{ end }}\n" +
			"{{ with .PullRequest }}{{ .Title }}{{ end }}{{ if .ReleaseNotes }}{{ .ReleaseNotes }}{{ else }}none{{ end }}",
		UpdateComment:    "{{ range $i, $d := .Dependencies }}{{ $d.Name }}{{ end }}{{ .PullRequest.URL }}",
		SupersedeComment: "{{ .Supersede.NewPullRequest.URL }} {{ .Supersede.NewPullRequest.Number }} {{ .Supersede.NewPullRequest.Title }} {{ with .Supersede.NewPullRequest }}{{ .URL }}{{ end }} {{ .Supersede.NewTicketURL }}",
	}
	if err := tickettemplate.NewRenderer("jira", tmpl).Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	renderer := tickettemplate.NewRenderer("jira", tmpl)
	if _, err := renderer.RenderDescription(testContext()); err != nil {
		t.Fatalf("RenderDescription: %v", err)
	}
}

func TestRenderRefusesAStoredReleaseTemplate(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("kaneo", tickettemplate.ContentTemplates{Title: "Release: {{ .Release.Tag }}"})
	if _, err := renderer.RenderTitle(testContext()); err == nil || !strings.Contains(err.Error(), ".Release.Tag") {
		t.Fatalf("RenderTitle() = %v, want an error naming .Release.Tag", err)
	}
}

func TestTemplateHelperFunctions(t *testing.T) {
	t.Parallel()

	renderer := tickettemplate.NewRenderer("kaneo", tickettemplate.ContentTemplates{
		Title: `{{ formatRFC3339 "2026-08-07T10:30:00+02:00" }}|{{ formatDate "2026-08-07T10:30:00Z" }}|{{ yesNo true }}|{{ yesNo false }}`,
	})
	title, err := renderer.RenderTitle(testContext())
	if err != nil {
		t.Fatalf("RenderTitle: %v", err)
	}
	if title != "2026-08-07T08:30:00Z|2026-08-07|yes|no" {
		t.Fatalf("title = %q", title)
	}
}

func TestParseContentTemplates(t *testing.T) {
	t.Parallel()

	got, err := tickettemplate.ParseContentTemplates(`{"title":"t","description":"d","updateComment":"u","supersedeComment":"c"}`)
	if err != nil {
		t.Fatalf("ParseContentTemplates: %v", err)
	}
	if got.Title != "t" || got.Description != "d" || got.UpdateComment != "u" || got.SupersedeComment != "c" {
		t.Fatalf("parsed = %+v", got)
	}
	got, err = tickettemplate.ParseContentTemplates(`{"title":"t","description":"d","supersedeComment":"c"}`)
	if err != nil || got.UpdateComment != "" {
		t.Fatalf("a stored value without updateComment = %+v, %v, want it empty", got, err)
	}
}
