"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { Settings, SettingsPatch } from "@/lib/query/types";

export function useSettings() {
  return useQuery({
    queryKey: queryKeys.settings(),
    queryFn: () => apiClient.get<Settings>("/settings"),
  });
}

/** PATCH /settings; the answer is the settings as they are afterwards. */
export function useUpdateSettings() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: SettingsPatch) =>
      apiClient.patch<Settings>("/settings", input),
    onSuccess: (settings) => {
      queryClient.setQueryData(queryKeys.settings(), settings);
    },
  });
}
