/** The role GitHub gives a user (docs/specs.html §3.7); the Go API enforces it. */
export type UserRole = "admin" | "viewer";

export type User = {
  id: string | number;
  login?: string;
  role?: UserRole;
  email?: string;
};

export type SessionResponse = {
  user: User | null;
};

export type SetupStatusResponse = {
  setupRequired: boolean;
  /** The App's installation page, while an App exists and has no installation. */
  installUrl?: string;
};

export type SetupSessionResponse = {
  expiresAt: string;
};

export type SetupTokenInput = {
  token: string;
};

export type SetupOwnerType = "user" | "organization";

export type SetupManifestInput = {
  owner: { type: SetupOwnerType; login?: string };
  name?: string;
};

export type SetupManifestResponse = {
  url: string;
  manifest: Record<string, unknown>;
};

export type StatusResponse = {
  version: string;
};

/** Where the worker reads the global Renovate config from (docs/specs.html §7.2). */
export type GlobalConfigOrigin = {
  source: "repository" | "mounted" | null;
  repository: string | null;
  ref: string | null;
};

export type Settings = {
  concurrencyCap: number;
  mode: "webhook" | "polling";
  presetRepositories: string[];
  globalConfig: GlobalConfigOrigin;
};

export type SettingsPatch = { concurrencyCap?: number };

/** The global Renovate config as the worker last used it: the raw text, never parsed. */
export type GlobalConfig = GlobalConfigOrigin & {
  capturedAt: string | null;
  content: string | null;
};

export type Installation = {
  id: string;
  accountLogin: string;
  accountType: "User" | "Organization";
  status: "active" | "suspended" | "removed";
  repositorySelection: "all" | "selected";
  repositoryCount: number;
  permissions: Record<string, string>;
  missingPermissions: string[];
  rateLimit: {
    remaining: number | null;
    limit: number | null;
    resetAt: string | null;
  } | null;
  defaultTicketProjectId: string | null;
};

export type InstallationsResponse = { items: Installation[] };

export type RoleMappingProblem = {
  variable: "ADMIN_TEAM" | "VIEWER_TEAM";
  code: "team_of_another_organization" | "ignored_on_personal_account";
};

/** The App without any secret: only whether each one is stored. */
export type GitHubAppResponse = {
  app: {
    appId: number;
    slug: string;
    clientId: string;
    htmlUrl: string;
    ownerLogin: string;
    ownerType: "User" | "Organization";
    webhookActive: boolean;
    hasPrivateKey: boolean;
    hasClientSecret: boolean;
    hasWebhookSecret: boolean;
  };
  installation: Installation | null;
  roleMapping: {
    adminTeam: string | null;
    viewerTeam: string | null;
    problems: RoleMappingProblem[];
  };
};

export type AboutResponse = {
  version: string;
  renovate: { version: string; license: string; sourceUrl: string };
};

export type Integration = {
  id: string;
  kind: string;
  name: string;
  baseUrl: string | null;
  hasSecret: boolean;
  isDefault: boolean;
  createdAt: string;
  updatedAt: string;
};

export type CreateIntegrationInput = {
  kind: string;
  name: string;
  baseUrl?: string | null;
  secret: string;
};

export type UpdateIntegrationInput = {
  name: string;
  baseUrl?: string | null;
  secret?: string | null;
};

export type TestConnectionResponse = {
  success: boolean;
  message?: string;
};

export type TicketMetadataItem = {
  id: string;
  name: string;
  label?: string;
};

export type TicketMetadataResponse = {
  items: TicketMetadataItem[];
  message?: string;
};

export type ContentTemplates = {
  title: string;
  description: string;
  updateComment: string;
  supersedeComment: string;
};

export type FilterUpdateType =
  "major" | "minor" | "patch" | "digest" | "lockfile";

/** Which update PRs get a ticket in a ticket project (docs/specs.html §5.2). */
export type TicketProjectFilter = {
  /** Empty lets every update type through. */
  updateTypes: FilterUpdateType[];
  /** Empty is no restriction; otherwise a PR needs one of them. */
  labels: string[];
  skipAutomerged: boolean;
};

export type TicketProject = {
  id: string;
  integrationId: string;
  externalProjectId: string;
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  contentTemplates: ContentTemplates;
  filter: TicketProjectFilter;
  createdAt: string;
  updatedAt: string;
};

export type CreateTicketProjectInput = {
  integrationId: string;
  externalProjectId: string;
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  contentTemplates: ContentTemplates;
  filter: TicketProjectFilter;
};

export type UpdateTicketProjectInput = {
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  contentTemplates: ContentTemplates;
  filter: TicketProjectFilter;
};
