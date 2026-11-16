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

export type UpdatePrState = "open" | "merged" | "closed" | "superseded";

export type UpdatePrType =
  "major" | "minor" | "patch" | "digest" | "lockfile" | "other";

export type UpdatePrSort = "openedAt" | "-openedAt" | "mergedAt" | "-mergedAt";

export type UpdatePrTicketState =
  "pending" | "open" | "done" | "cancelled" | "superseded";

export type UpdatePrDependency = {
  name: string;
  from: string | null;
  to: string | null;
  updateType: UpdatePrType | null;
};

/** The ticket as Release Ops last recorded it (docs/specs.html §7.6). */
export type UpdatePrTicket = {
  state: UpdatePrTicketState;
  externalId: string | null;
  url: string | null;
  hasError: boolean;
};

export type UpdatePr = {
  id: string;
  repository: { id: string; fullName: string };
  number: number;
  url: string;
  title: string;
  state: UpdatePrState;
  openedAt: string | null;
  mergedAt: string | null;
  closedAt: string | null;
  supersededBy: { id: string; number: number; url: string } | null;
  updateTypes: UpdatePrType[];
  dependencies: UpdatePrDependency[];
  ticket: UpdatePrTicket | null;
};

export type UpdatePrsResponse = {
  items: UpdatePr[];
  page: number;
  pageSize: number;
  total: number;
};

export type RepositoryConfigState = "unknown" | "present" | "missing";

export type RepositoryJobState = "queued" | "running" | "succeeded" | "failed";

/** The repository's most recently created job (docs/specs.html §7.5). */
export type RepositoryJob = {
  id: string;
  state: RepositoryJobState;
  finishedAt: string | null;
};

/**
 * An installed repository (docs/specs.html §7.5). A viewer's response has no `ticketProject*`
 * fields; they are present, possibly null, in an admin's.
 */
export type Repository = {
  id: string;
  fullName: string;
  defaultBranch: string;
  enabled: boolean;
  configState: RepositoryConfigState;
  htmlUrl: string | null;
  dashboardUrl: string | null;
  openUpdatePrs: number;
  lastJob: RepositoryJob | null;
  ticketProjectId?: string | null;
  effectiveTicketProjectId?: string | null;
  effectiveTicketProjectName?: string | null;
};

export type RepositoriesResponse = {
  items: Repository[];
  page: number;
  pageSize: number;
  total: number;
};

export type RepositoryPatch = {
  enabled?: boolean;
  ticketProjectId?: string | null;
};

export type EnableRenovateResult = {
  pullRequestUrl: string;
  number: number;
};

export type JobState = "queued" | "running" | "succeeded" | "failed";

export type JobTrigger =
  | "dashboard_checkbox"
  | "pr_checkbox"
  | "pr_closed"
  | "push"
  | "repository_added"
  | "schedule"
  | "polling"
  | "run_now";

export type JobPriority = "high" | "normal";

/** A job as the Jobs page lists it (docs/specs.html §7.7). */
export type Job = {
  id: string;
  repository: { id: string; fullName: string };
  kind: string;
  state: JobState;
  priority: JobPriority;
  trigger: JobTrigger;
  matchedMarkers: string[];
  attempts: number;
  createdAt: string;
  startedAt: string | null;
  finishedAt: string | null;
  startLatencyMs: number | null;
};

export type JobsResponse = {
  items: Job[];
  page: number;
  pageSize: number;
  total: number;
};

export type JobRunOutcome =
  | "running"
  | "succeeded"
  | "no_config"
  | "failed"
  | "aborted"
  | "lease_expired";

/** One attempt of a job; `logExcerpt` is the stored, already redacted tail of Renovate's log. */
export type JobRun = {
  attempt: number;
  startedAt: string;
  finishedAt: string | null;
  outcome: JobRunOutcome;
  exitCode: number | null;
  errorCode: string | null;
  errorMessage: string | null;
  reportStatus: "ok" | "missing" | "unreadable" | null;
  logExcerpt: string | null;
};

export type JobEvent = {
  at: string;
  kind: string;
  message: string | null;
};

export type JobDetail = Job & {
  runs: JobRun[];
  events: JobEvent[];
};

export type ProblemKind =
  "job_failed" | "ticket_failed" | "rate_limited" | "permissions_lost";

/**
 * A problem of the Problems view (docs/specs.html §7.7). `repository` is null for the
 * installation-level kinds, which only an admin receives. A viewer's `ticket_failed` has no
 * `message`; `ref.updateTicketId` is given to an admin for a ticket whose pending marker may be
 * cleared.
 */
export type Problem = {
  kind: ProblemKind;
  repository: { id: string; fullName: string } | null;
  since: string;
  message: string | null;
  ref: {
    jobId?: string;
    updatePrId?: string;
    updatePrUrl?: string;
    updateTicketId?: string;
  } | null;
};

export type ProblemsResponse = {
  items: Problem[];
  page: number;
  pageSize: number;
  total: number;
};
