"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { TicketMetadataResponse } from "@/lib/query/types";

function metadataEnabled(
  integrationId: string | null | undefined,
  enabled: boolean,
): boolean {
  return enabled && Boolean(integrationId);
}

export function useTicketMetadataWorkspaces(
  integrationId: string | null | undefined,
  enabled = true,
) {
  return useQuery({
    queryKey: queryKeys.ticketMetadata(integrationId, "workspaces"),
    queryFn: () =>
      apiClient.get<TicketMetadataResponse>(
        `/integrations/${integrationId}/ticket-metadata/workspaces`,
      ),
    enabled: metadataEnabled(integrationId, enabled),
  });
}

export function useTicketMetadataProjects(
  integrationId: string | null | undefined,
  workspaceId: string | null | undefined,
  enabled = true,
) {
  const params = new URLSearchParams();
  if (workspaceId) {
    params.set("workspaceId", workspaceId);
  }
  const query = params.toString();

  return useQuery({
    queryKey: queryKeys.ticketMetadata(
      integrationId,
      "projects",
      workspaceId ?? null,
    ),
    queryFn: () =>
      apiClient.get<TicketMetadataResponse>(
        `/integrations/${integrationId}/ticket-metadata/projects${query ? `?${query}` : ""}`,
      ),
    enabled: metadataEnabled(integrationId, enabled),
  });
}

export function useTicketMetadataStatuses(
  integrationId: string | null | undefined,
  externalProjectId: string | null | undefined,
  enabled = true,
) {
  const params = new URLSearchParams();
  if (externalProjectId) {
    params.set("externalProjectId", externalProjectId);
  }
  const query = params.toString();

  return useQuery({
    queryKey: queryKeys.ticketMetadata(
      integrationId,
      "statuses",
      externalProjectId ?? null,
    ),
    queryFn: () =>
      apiClient.get<TicketMetadataResponse>(
        `/integrations/${integrationId}/ticket-metadata/statuses${query ? `?${query}` : ""}`,
      ),
    enabled:
      metadataEnabled(integrationId, enabled) && Boolean(externalProjectId),
  });
}

export function useTicketMetadataPriorities(
  integrationId: string | null | undefined,
  externalProjectId: string | null | undefined,
  enabled = true,
) {
  const params = new URLSearchParams();
  if (externalProjectId) {
    params.set("externalProjectId", externalProjectId);
  }
  const query = params.toString();

  return useQuery({
    queryKey: queryKeys.ticketMetadata(
      integrationId,
      "priorities",
      externalProjectId ?? null,
    ),
    queryFn: () =>
      apiClient.get<TicketMetadataResponse>(
        `/integrations/${integrationId}/ticket-metadata/priorities${query ? `?${query}` : ""}`,
      ),
    enabled:
      metadataEnabled(integrationId, enabled) && Boolean(externalProjectId),
  });
}

export function useTicketMetadataIssueTypes(
  integrationId: string | null | undefined,
  externalProjectId: string | null | undefined,
  enabled = true,
) {
  const params = new URLSearchParams();
  if (externalProjectId) {
    params.set("externalProjectId", externalProjectId);
  }
  const query = params.toString();

  return useQuery({
    queryKey: queryKeys.ticketMetadata(
      integrationId,
      "issue-types",
      externalProjectId ?? null,
    ),
    queryFn: () =>
      apiClient.get<TicketMetadataResponse>(
        `/integrations/${integrationId}/ticket-metadata/issue-types${query ? `?${query}` : ""}`,
      ),
    enabled:
      metadataEnabled(integrationId, enabled) && Boolean(externalProjectId),
  });
}
