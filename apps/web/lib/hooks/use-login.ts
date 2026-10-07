"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { LoginInput, LoginResponse } from "@/lib/query/types";

export function useLogin() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (input: LoginInput) =>
      apiClient.post<LoginResponse>("/auth/login", input),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.session() });
    },
  });
}
