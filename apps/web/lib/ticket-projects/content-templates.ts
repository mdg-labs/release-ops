import type { ContentTemplates } from "@/lib/query/types";

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
