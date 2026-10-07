package tickettemplate

const (
	defaultTitleMarkdown = "Release: {{ .Repo.SourceKind }} {{ .Repo.ProjectPath }} {{ .Release.Tag }}"

	defaultDescriptionMarkdown = "**Source:** {{ .Repo.SourceKind }}\n" +
		"**Repository:** {{ .Repo.ProjectPath }} ({{ .Repo.URL }})\n\n" +
		"**Release:** {{ .Release.Tag }} — {{ .Release.Name }}\n" +
		"**URL:** {{ .Release.URL }}\n" +
		"**Published:** {{ .Release.PublishedAt }}\n" +
		"**Pre-release:** {{ yesNo .Release.IsPrerelease }}\n\n" +
		"**Previous tag:** {{ .Previous.Tag }}\n\n" +
		"**Release notes:**\n{{ .Release.Notes }}"

	defaultDescriptionPlain = "Source: {{ .Repo.SourceKind }}\n" +
		"Repository: {{ .Repo.ProjectPath }} ({{ .Repo.URL }})\n\n" +
		"Release: {{ .Release.Tag }} — {{ .Release.Name }}\n" +
		"URL: {{ .Release.URL }}\n" +
		"Published: {{ .Release.PublishedAt }}\n" +
		"Pre-release: {{ yesNo .Release.IsPrerelease }}\n\n" +
		"Previous tag: {{ .Previous.Tag }}\n\n" +
		"Release notes:\n{{ .Release.Notes }}"

	defaultSupersedeCommentMarkdown = "Superseded: {{ .Supersede.OldTag }} → {{ .Supersede.NewTag }}\n{{ .Release.URL }}\n\nNew ticket: {{ .Supersede.NewTicketURL }}"

	defaultSupersedeCommentPlain = "Superseded: {{ .Supersede.OldTag }} → {{ .Supersede.NewTag }}\n{{ .Release.URL }}\n\nNew ticket: {{ .Supersede.NewTicketURL }}"
)

// ContentTemplates mirrors ticket_projects.content_templates JSON (specs §5.4).
type ContentTemplates struct {
	Title            string
	Description      string
	SupersedeComment string
}

// DefaultContentTemplates returns empty templates (integration-kind defaults apply per key).
func DefaultContentTemplates() ContentTemplates {
	return ContentTemplates{}
}

func defaultTitleTemplate() string {
	return defaultTitleMarkdown
}

func defaultDescriptionTemplate(integrationKind string) string {
	if integrationKind == "jira" {
		return defaultDescriptionPlain
	}
	return defaultDescriptionMarkdown
}

func defaultSupersedeCommentTemplate(integrationKind string) string {
	if integrationKind == "jira" {
		return defaultSupersedeCommentPlain
	}
	return defaultSupersedeCommentMarkdown
}
