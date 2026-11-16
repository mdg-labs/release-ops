import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import type { ContentTemplates } from "@/lib/query/types";

/** The template keys of docs/specs.html §5.4; the form edits the keys of ContentTemplates. */
export type TemplateField = keyof ContentTemplates;

// Mirror the default templates in internal/tickettemplate/templates.go
// (docs/specs.html §5.4); shown as placeholders while a field is empty.
const DEFAULT_TITLE = "{{ .PullRequest.Title }} ({{ .Repository }})";

const DEFAULT_DESCRIPTION_MARKDOWN =
  "**Repository:** {{ .Repository }}\n" +
  "**Pull request:** [#{{ .PullRequest.Number }} {{ .PullRequest.Title }}]({{ .PullRequest.URL }})\n\n" +
  "**Dependencies:**\n" +
  "{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
  "{{ end }}\n" +
  "**Release notes:**\n{{ .ReleaseNotes }}";

const DEFAULT_DESCRIPTION_PLAIN =
  "Repository: {{ .Repository }}\n" +
  "Pull request: #{{ .PullRequest.Number }} {{ .PullRequest.Title }} ({{ .PullRequest.URL }})\n\n" +
  "Dependencies:\n" +
  "{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
  "{{ end }}\n" +
  "Release notes:\n{{ .ReleaseNotes }}";

const DEFAULT_UPDATE_COMMENT =
  "Pull request updated:\n" +
  "{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n" +
  "{{ end }}{{ .PullRequest.URL }}";

const DEFAULT_SUPERSEDE_COMMENT =
  "Superseded by {{ .Supersede.NewPullRequest.URL }}\n" +
  "{{ if .Supersede.NewTicketURL }}New ticket: {{ .Supersede.NewTicketURL }}{{ end }}";

/** Integration-kind default for each template key; Jira uses plain text. */
export function integrationDefaultTemplates(
  kind: TicketIntegrationKind | undefined,
): ContentTemplates {
  const plain = kind === "jira";
  return {
    title: DEFAULT_TITLE,
    description: plain
      ? DEFAULT_DESCRIPTION_PLAIN
      : DEFAULT_DESCRIPTION_MARKDOWN,
    updateComment: DEFAULT_UPDATE_COMMENT,
    supersedeComment: DEFAULT_SUPERSEDE_COMMENT,
  };
}

export type TemplateVariable = {
  key: string;
  path: string;
  worksIn: readonly TemplateField[];
};

const ALL_FIELDS = [
  "title",
  "description",
  "updateComment",
  "supersedeComment",
] as const;
const WITH_NOTES = ["title", "description", "updateComment"] as const;
const SUPERSEDE_ONLY = ["supersedeComment"] as const;

// The variables of docs/specs.html §5.4. The paths from .Name to .UpdateType are the dot of a
// {{ range .Dependencies }} loop.
export const TEMPLATE_VARIABLES: readonly TemplateVariable[] = [
  { key: "pullRequestUrl", path: ".PullRequest.URL", worksIn: ALL_FIELDS },
  {
    key: "pullRequestNumber",
    path: ".PullRequest.Number",
    worksIn: ALL_FIELDS,
  },
  { key: "pullRequestTitle", path: ".PullRequest.Title", worksIn: ALL_FIELDS },
  { key: "repository", path: ".Repository", worksIn: ALL_FIELDS },
  { key: "dependencies", path: ".Dependencies", worksIn: ALL_FIELDS },
  { key: "dependencyName", path: ".Name", worksIn: ALL_FIELDS },
  { key: "dependencyFrom", path: ".From", worksIn: ALL_FIELDS },
  { key: "dependencyTo", path: ".To", worksIn: ALL_FIELDS },
  { key: "dependencyUpdateType", path: ".UpdateType", worksIn: ALL_FIELDS },
  { key: "releaseNotes", path: ".ReleaseNotes", worksIn: WITH_NOTES },
  {
    key: "supersedeNewPullRequestUrl",
    path: ".Supersede.NewPullRequest.URL",
    worksIn: SUPERSEDE_ONLY,
  },
  {
    key: "supersedeNewPullRequestNumber",
    path: ".Supersede.NewPullRequest.Number",
    worksIn: SUPERSEDE_ONLY,
  },
  {
    key: "supersedeNewPullRequestTitle",
    path: ".Supersede.NewPullRequest.Title",
    worksIn: SUPERSEDE_ONLY,
  },
  {
    key: "supersedeNewTicketUrl",
    path: ".Supersede.NewTicketURL",
    worksIn: SUPERSEDE_ONLY,
  },
];

/** The text a user pastes into a template field, e.g. `{{ .Repository }}`. */
export function templateVariableSyntax(path: string): string {
  return `{{ ${path} }}`;
}

export function defaultContentTemplates(): ContentTemplates {
  return {
    title: "",
    description: "",
    updateComment: "",
    supersedeComment: "",
  };
}

export function contentTemplatesFromRecord(
  record: Record<string, unknown> | ContentTemplates | undefined,
): ContentTemplates {
  if (!record) {
    return defaultContentTemplates();
  }

  return {
    title: typeof record.title === "string" ? record.title : "",
    description:
      typeof record.description === "string" ? record.description : "",
    updateComment:
      typeof record.updateComment === "string" ? record.updateComment : "",
    supersedeComment:
      typeof record.supersedeComment === "string"
        ? record.supersedeComment
        : "",
  };
}
