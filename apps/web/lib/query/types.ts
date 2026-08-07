export type User = {
  id: string;
  email: string;
};

export type SessionResponse = {
  user: User | null;
};

export type LoginInput = {
  email: string;
  password: string;
};

export type LoginResponse = {
  user: User;
};

export type PollRunError = {
  repoId: string;
  message: string;
};

export type PollRunEvent = {
  id: string;
  pollRunId: string;
  monitoredRepoId: string | null;
  action: string;
  detail: string | null;
  createdAt: string;
};

export type PollRun = {
  id: string;
  startedAt: string;
  finishedAt: string | null;
  status: string;
  reposChecked: number;
  ticketsCreated: number;
  ticketsSuperseded: number;
  errors: PollRunError[];
  events?: PollRunEvent[];
};

export type StatusRepo = {
  id: string;
  sourceKind: string;
  projectPath: string;
  ticketProjectId: string;
  ticketProjectName: string;
  enabled: boolean;
  openTicketExternalId: string | null;
  openTicketTag: string | null;
  lastKnownTag: string | null;
  lastPolledAt: string | null;
  lastError: string | null;
};

export type StatusResponse = {
  pollIntervalMinutes: number;
  lastRun: PollRun | null;
  repos: StatusRepo[];
  isPolling: boolean;
};

export type Settings = {
  pollIntervalMinutes: number;
  inviteTokenExpiryHours: number;
  passwordResetTokenExpiryMinutes: number;
  smtpConfigured?: boolean;
};

export type SettingsPatch = Partial<Settings>;

export type UserListItem = {
  id: string;
  email: string;
  createdAt: string;
};

export type ListUsersResponse = {
  items: UserListItem[];
};

export type Invitation = {
  id: string;
  email: string;
  expiresAt: string;
  createdAt: string;
  invitedByUserId: string | null;
};

export type ListInvitationsResponse = {
  items: Invitation[];
};

export type CreateInvitationInput = {
  email: string;
};

export type CreateInvitationResponse = {
  id: string;
  email: string;
  expiresAt: string;
  inviteUrl: string;
};

export type EmailChangeRequestInput = {
  newEmail: string;
  currentPassword: string;
};

export type Integration = {
  id: string;
  kind: string;
  name: string;
  baseUrl: string | null;
  hasSecret: boolean;
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

export type TicketProject = {
  id: string;
  integrationId: string;
  externalProjectId: string;
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  onOpenTicketPolicy: string;
  createdAt: string;
  updatedAt: string;
};

export type CreateTicketProjectInput = {
  integrationId: string;
  externalProjectId: string;
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  onOpenTicketPolicy: string;
};

export type UpdateTicketProjectInput = {
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  onOpenTicketPolicy: string;
};

export type Repo = {
  id: string;
  sourceKind: string;
  projectPath: string;
  enabled: boolean;
  sourceIntegrationId: string | null;
  ticketProjectId: string;
  notificationTargetIds: string[];
  openTicketExternalId: string | null;
  openTicketTag: string | null;
  lastKnownTag: string | null;
  lastPolledAt: string | null;
  lastError: string | null;
  createdAt: string;
  updatedAt: string;
};

export type CreateRepoInput = {
  sourceKind: string;
  projectPath: string;
  enabled?: boolean;
  sourceIntegrationId?: string | null;
  ticketProjectId: string;
  notificationTargetIds?: string[];
};

export type UpdateRepoInput = {
  sourceKind: string;
  projectPath: string;
  enabled: boolean;
  sourceIntegrationId?: string | null;
  ticketProjectId: string;
  notificationTargetIds?: string[];
};

export type NotificationTarget = {
  id: string;
  name: string;
  hasSecret: boolean;
  events: string[];
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
};

export type CreateNotificationTargetInput = {
  name: string;
  shoutrrrUrl: string;
  events?: string[];
  enabled?: boolean;
};

export type UpdateNotificationTargetInput = {
  name: string;
  shoutrrrUrl?: string | null;
  events: string[];
  enabled: boolean;
};

export type TriggerPollResponse = {
  runId: string;
};

export type PollRunsParams = {
  limit?: number;
  offset?: number;
};
