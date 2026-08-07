export const INTEGRATION_KINDS = [
  "github",
  "gitlab",
  "gitea",
  "forgejo",
  "codeberg",
  "phasical",
  "jira",
  "linear",
] as const;

export type IntegrationKind = (typeof INTEGRATION_KINDS)[number];

const BASE_URL_KINDS = new Set<IntegrationKind>([
  "gitlab",
  "gitea",
  "forgejo",
  "phasical",
  "jira",
]);

export function kindRequiresBaseUrl(kind: string): boolean {
  return BASE_URL_KINDS.has(kind as IntegrationKind);
}

export function kindUsesApiKeyLabel(kind: string): boolean {
  return kind === "phasical" || kind === "linear";
}

export function kindIsJira(kind: string): boolean {
  return kind === "jira";
}

export const TICKET_INTEGRATION_KINDS = ["phasical", "jira", "linear"] as const;

export type TicketIntegrationKind = (typeof TICKET_INTEGRATION_KINDS)[number];

export function kindIsTicket(kind: string): boolean {
  return TICKET_INTEGRATION_KINDS.includes(kind as TicketIntegrationKind);
}
