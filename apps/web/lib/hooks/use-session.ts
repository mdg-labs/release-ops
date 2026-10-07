"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { SessionResponse } from "@/lib/query/types";

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session(),
    queryFn: () => apiClient.get<SessionResponse>("/auth/session"),
  });
}
