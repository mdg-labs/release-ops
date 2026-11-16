package tickettemplate

const (
	defaultTitle = "{{ .PullRequest.Title }} ({{ .Repository }})"

	defaultDescriptionMarkdown = "**Repository:** {{ .Repository }}\n" +
		"**Pull request:** [#{{ .PullRequest.Number }} {{ .PullRequest.Title }}]({{ .PullRequest.URL }})\n\n" +
		"**Dependencies:**\n" +
		"{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
		"{{ end }}\n" +
		"**Release notes:**\n{{ .ReleaseNotes }}"

	defaultDescriptionPlain = "Repository: {{ .Repository }}\n" +
		"Pull request: #{{ .PullRequest.Number }} {{ .PullRequest.Title }} ({{ .PullRequest.URL }})\n\n" +
		"Dependencies:\n" +
		"{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
		"{{ end }}\n" +
		"Release notes:\n{{ .ReleaseNotes }}"

	defaultUpdateComment = "Pull request updated:\n" +
		"{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
		"{{ end }}{{ .PullRequest.URL }}"

	defaultSupersedeComment = "Superseded by {{ .Supersede.NewPullRequest.URL }}\n" +
		"{{ if .Supersede.NewTicketURL }}New ticket: {{ .Supersede.NewTicketURL }}{{ end }}"
)

// ContentTemplates mirrors ticket_projects.content_templates JSON (specs §5.4). An empty
// key selects the integration-kind default.
type ContentTemplates struct {
	Title            string
	Description      string
	UpdateComment    string
	SupersedeComment string
}

// DefaultContentTemplates returns empty templates (integration-kind defaults apply per key).
func DefaultContentTemplates() ContentTemplates {
	return ContentTemplates{}
}

func defaultDescriptionTemplate(integrationKind string) string {
	if integrationKind == "jira" {
		return defaultDescriptionPlain
	}
	return defaultDescriptionMarkdown
}
