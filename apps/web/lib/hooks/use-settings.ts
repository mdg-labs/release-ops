"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { Settings } from "@/lib/query/types";

export function useSettings() {
  const queryClient = useQueryClient();

  const settingsQuery = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => apiClient.get<Settings>("/settings"),
  });

  const updateMutation = useMutation({
    mutationFn: (input: Partial<Settings>) =>
      apiClient.patch<Settings>("/settings", input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.settings() });
      queryClient.invalidateQueries({ queryKey: queryKeys.status() });
    },
  });

  return {
    ...settingsQuery,
    updateSettings: updateMutation,
  };
}
