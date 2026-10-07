import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import type { ContentTemplates } from "@/lib/query/types";

export type TemplateField = keyof ContentTemplates;

// Mirrors the default templates in internal/tickettemplate/templates.go
// (docs/specs.html §5.4); shown as placeholders while a field is empty.
const DEFAULT_TITLE =
  "Release: {{ .Repo.SourceKind }} {{ .Repo.ProjectPath }} {{ .Release.Tag }}";

const DEFAULT_DESCRIPTION_MARKDOWN =
  "**Source:** {{ .Repo.SourceKind }}\n" +
  "**Repository:** {{ .Repo.ProjectPath }} ({{ .Repo.URL }})\n\n" +
  "**Release:** {{ .Release.Tag }} — {{ .Release.Name }}\n" +
  "**URL:** {{ .Release.URL }}\n" +
  "**Published:** {{ .Release.PublishedAt }}\n" +
  "**Pre-release:** {{ yesNo .Release.IsPrerelease }}\n\n" +
  "**Previous tag:** {{ .Previous.Tag }}\n\n" +
  "**Release notes:**\n{{ .Release.Notes }}";

const DEFAULT_DESCRIPTION_PLAIN =
  "Source: {{ .Repo.SourceKind }}\n" +
  "Repository: {{ .Repo.ProjectPath }} ({{ .Repo.URL }})\n\n" +
  "Release: {{ .Release.Tag }} — {{ .Release.Name }}\n" +
  "URL: {{ .Release.URL }}\n" +
  "Published: {{ .Release.PublishedAt }}\n" +
  "Pre-release: {{ yesNo .Release.IsPrerelease }}\n\n" +
  "Previous tag: {{ .Previous.Tag }}\n\n" +
  "Release notes:\n{{ .Release.Notes }}";

const DEFAULT_SUPERSEDE_COMMENT =
  "Superseded: {{ .Supersede.OldTag }} → {{ .Supersede.NewTag }}\n{{ .Release.URL }}\n\nNew ticket: {{ .Supersede.NewTicketURL }}";

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
    supersedeComment: DEFAULT_SUPERSEDE_COMMENT,
  };
}

export type TemplateVariable = {
  key: string;
  path: string;
  worksIn: readonly TemplateField[];
};

const ALL_FIELDS = ["title", "description", "supersedeComment"] as const;
const SUPERSEDE_ONLY = ["supersedeComment"] as const;

export const TEMPLATE_VARIABLES: readonly TemplateVariable[] = [
  { key: "repoSourceKind", path: ".Repo.SourceKind", worksIn: ALL_FIELDS },
  { key: "repoProjectPath", path: ".Repo.ProjectPath", worksIn: ALL_FIELDS },
  { key: "repoUrl", path: ".Repo.URL", worksIn: ALL_FIELDS },
  { key: "releaseTag", path: ".Release.Tag", worksIn: ALL_FIELDS },
  { key: "releaseName", path: ".Release.Name", worksIn: ALL_FIELDS },
  { key: "releaseUrl", path: ".Release.URL", worksIn: ALL_FIELDS },
  { key: "releaseNotes", path: ".Release.Notes", worksIn: ALL_FIELDS },
  {
    key: "releasePublishedAt",
    path: ".Release.PublishedAt",
    worksIn: ALL_FIELDS,
  },
  {
    key: "releaseIsPrerelease",
    path: ".Release.IsPrerelease",
    worksIn: ALL_FIELDS,
  },
  { key: "previousTag", path: ".Previous.Tag", worksIn: ALL_FIELDS },
  {
    key: "supersedeOldTag",
    path: ".Supersede.OldTag",
    worksIn: SUPERSEDE_ONLY,
  },
  {
    key: "supersedeNewTag",
    path: ".Supersede.NewTag",
    worksIn: SUPERSEDE_ONLY,
  },
  {
    key: "supersedeNewTicketUrl",
    path: ".Supersede.NewTicketURL",
    worksIn: SUPERSEDE_ONLY,
  },
];

/** The text a user pastes into a template field, e.g. `{{ .Release.Tag }}`. */
export function templateVariableSyntax(path: string): string {
  return `{{ ${path} }}`;
}

export function defaultContentTemplates(): ContentTemplates {
  return {
    title: "",
    description: "",
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
    supersedeComment:
      typeof record.supersedeComment === "string"
        ? record.supersedeComment
        : "",
  };
}
