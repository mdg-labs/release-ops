import type { TicketIntegrationKind } from "@/lib/integrations/kinds";

/** Kaneo create defaults; workspaceId is used to build task web links (specs §6.4). */
export type KaneoCreateConfig = {
  workspaceId: string;
  status: string;
  priority: string;
};

export type JiraCreateConfig = {
  issueType: string;
  priority: string;
  initialStatus: string;
};

export type LinearCreateConfig = {
  priority: string;
  stateId: string;
};

export function defaultCreateConfig(
  kind: TicketIntegrationKind,
): Record<string, unknown> {
  switch (kind) {
    case "kaneo":
      return { workspaceId: "", status: "ready", priority: "medium" };
    case "jira":
      return {
        issueType: "Task",
        priority: "Medium",
        initialStatus: "To Do",
      };
    case "linear":
      return { priority: "2", stateId: "" };
    default:
      return {};
  }
}

export function createConfigFromRecord(
  kind: TicketIntegrationKind,
  record: Record<string, unknown>,
): Record<string, unknown> {
  const defaults = defaultCreateConfig(kind);
  return { ...defaults, ...record };
}

/**
 * Serialize create_config for the API. Kaneo persists the selected workspace
 * (`workspaceId`) so the dashboard can link to the Kaneo task page.
 */
export function serializeCreateConfig(
  kind: TicketIntegrationKind,
  values: Record<string, unknown>,
  workspaceId = "",
): Record<string, unknown> {
  if (kind === "kaneo") {
    const selectedWorkspaceId =
      workspaceId.trim() || String(values.workspaceId ?? "").trim();
    return {
      ...createConfigFromRecord(kind, values),
      workspaceId: selectedWorkspaceId,
    };
  }

  if (kind === "linear") {
    const priority = Number(values.priority);
    return {
      priority: Number.isFinite(priority) ? priority : 2,
      stateId: String(values.stateId ?? ""),
    };
  }

  return createConfigFromRecord(kind, values);
}
