export const SOURCE_KINDS = [
  "github",
  "gitlab",
  "gitea",
  "forgejo",
  "codeberg",
] as const;

export type SourceKind = (typeof SOURCE_KINDS)[number];

const SOURCE_KINDS_REQUIRING_INTEGRATION = new Set<SourceKind>([
  "gitlab",
  "gitea",
  "forgejo",
]);

export function sourceKindRequiresIntegration(kind: string): boolean {
  return SOURCE_KINDS_REQUIRING_INTEGRATION.has(kind as SourceKind);
}
