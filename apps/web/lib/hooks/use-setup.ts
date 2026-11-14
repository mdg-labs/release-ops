"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  SetupSessionResponse,
  SetupStatusResponse,
  SetupTokenInput,
} from "@/lib/query/types";

export function useSetupStatus() {
  return useQuery({
    queryKey: queryKeys.setupStatus(),
    queryFn: ({ signal }) =>
      apiClient.get<SetupStatusResponse>("/setup/status", { signal }),
    retry: false,
  });
}

/**
 * The current setup session, or null when there is none (401). Any other failure
 * is an error, never "no session".
 */
export function useSetupSession(enabled: boolean) {
  return useQuery({
    queryKey: queryKeys.setupSession(),
    enabled,
    retry: false,
    queryFn: async ({ signal }): Promise<SetupSessionResponse | null> => {
      try {
        return await apiClient.get<SetupSessionResponse>("/setup/session", {
          signal,
        });
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          return null;
        }
        throw error;
      }
    },
  });
}

export function useRedeemSetupToken() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: SetupTokenInput) =>
      apiClient.post<void>("/setup/session", input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({
        queryKey: queryKeys.setupSession(),
      });
    },
  });
}
