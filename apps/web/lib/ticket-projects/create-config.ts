import type { TicketIntegrationKind } from "@/lib/integrations/kinds";

export type PhasicalCreateConfig = {
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
    case "phasical":
      return { status: "ready", priority: "medium" };
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
