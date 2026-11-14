export type User = {
  id: string | number;
  login?: string;
  role?: string;
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

export type Settings = {
  pollIntervalMinutes: number;
};

export type SettingsPatch = Partial<Settings>;

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
