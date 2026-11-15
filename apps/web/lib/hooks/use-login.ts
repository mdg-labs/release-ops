"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";

export type LoginLinkResponse = {
  user: { id: number; login: string; role: string };
};

/** Redeems the break-glass link printed by `release-ops admin login-link`. */
export function useRedeemLoginLink() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: { token: string }) =>
      apiClient.post<LoginLinkResponse>("/auth/login-link", input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.session() });
    },
  });
}
