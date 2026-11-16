"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { Installation, InstallationsResponse } from "@/lib/query/types";

/** The installation that is not removed, if the list has one. */
export function liveInstallation(
  data: InstallationsResponse | undefined,
): Installation | null {
  return data?.items.find((item) => item.status !== "removed") ?? null;
}

export function useInstallations() {
  return useQuery({
    queryKey: queryKeys.installations(),
    queryFn: () => apiClient.get<InstallationsResponse>("/installations"),
  });
}

/** PATCH /installations/{id}: a ticket project id, or null to clear the default. */
export function useSetDefaultTicketProject() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      id,
      defaultTicketProjectId,
    }: {
      id: string;
      defaultTicketProjectId: string | null;
    }) =>
      apiClient.patch<Installation>(`/installations/${id}`, {
        defaultTicketProjectId,
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.installations() });
      queryClient.invalidateQueries({ queryKey: queryKeys.githubApp() });
    },
  });
}
