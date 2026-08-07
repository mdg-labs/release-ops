import type { PollRunsParams } from "./types";

export const queryKeys = {
  session: () => ["session"] as const,
  status: () => ["status"] as const,
  repos: () => ["repos"] as const,
  integrations: () => ["integrations"] as const,
  ticketProjects: (integrationId?: string) =>
    ["ticket-projects", integrationId ?? null] as const,
  notificationTargets: () => ["notification-targets"] as const,
  settings: () => ["settings"] as const,
  users: () => ["users"] as const,
  invitations: () => ["invitations"] as const,
  pollRuns: (params?: PollRunsParams) => ["poll-runs", params ?? {}] as const,
  pollRun: (id: string) => ["poll-run", id] as const,
  ticketMetadata: (
    integrationId: string | null | undefined,
    resource: string,
    scope?: string | null,
  ) =>
    [
      "ticket-metadata",
      integrationId ?? null,
      resource,
      scope ?? null,
    ] as const,
};
