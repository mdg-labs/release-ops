"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { SessionResponse, UserRole } from "@/lib/query/types";

export function useSession() {
  return useQuery({
    queryKey: queryKeys.session(),
    queryFn: () => apiClient.get<SessionResponse>("/auth/session"),
  });
}

export type SessionRole = {
  /** The role from the session, or null while it is unknown: loading, failed or no session. */
  role: UserRole | null;
  isLoading: boolean;
  isError: boolean;
};

/**
 * The signed-in user's role. TanStack Query keeps the last data after a failed refetch, so a
 * failed request reads as an unknown role, never as the last one. Only the Go API enforces a
 * role; the UI uses this to hide what the API would refuse.
 */
export function useSessionRole(): SessionRole {
  const { data, isLoading, isError } = useSession();
  const role = isError ? null : (data?.user?.role ?? null);
  return { role, isLoading, isError };
}
