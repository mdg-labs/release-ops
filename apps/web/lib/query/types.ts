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

export type SetupStatusResponse = {
  setupRequired: boolean;
};

export type SetupSessionResponse = {
  expiresAt: string;
};

export type SetupTokenInput = {
  token: string;
};

export type StatusResponse = {
  version: string;
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

export type PasswordChangeInput = {
  currentPassword: string;
  newPassword: string;
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
  supersedeComment: string;
};

export type TicketProject = {
  id: string;
  integrationId: string;
  externalProjectId: string;
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  contentTemplates: ContentTemplates;
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
  contentTemplates: ContentTemplates;
  onOpenTicketPolicy: string;
};

export type UpdateTicketProjectInput = {
  name: string;
  createConfig: Record<string, unknown>;
  statusMapping: Record<string, unknown>;
  contentTemplates: ContentTemplates;
  onOpenTicketPolicy: string;
};
