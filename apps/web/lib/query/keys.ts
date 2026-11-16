export const queryKeys = {
  session: () => ["session"] as const,
  status: () => ["status"] as const,
  setupStatus: () => ["setup-status"] as const,
  setupSession: () => ["setup-session"] as const,
  integrations: () => ["integrations"] as const,
  ticketProjects: (integrationId?: string) =>
    ["ticket-projects", integrationId ?? null] as const,
  settings: () => ["settings"] as const,
  globalConfig: () => ["global-config"] as const,
  githubApp: () => ["github-app"] as const,
  installations: () => ["installations"] as const,
  about: () => ["about"] as const,
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
