"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { GlobalConfig } from "@/lib/query/types";

/** The global Renovate config as the worker last used it. Read-only: there is no write. */
export function useGlobalConfig() {
  return useQuery({
    queryKey: queryKeys.globalConfig(),
    queryFn: () => apiClient.get<GlobalConfig>("/settings/global-config"),
  });
}
