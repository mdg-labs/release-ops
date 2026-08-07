"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  EmailChangeRequestInput,
  ListUsersResponse,
} from "@/lib/query/types";

export function useUsers() {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.users(),
    queryFn: () => apiClient.get<ListUsersResponse>("/users"),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.users() });
  };

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.delete<void>(`/users/${id}`),
    onSuccess: invalidate,
  });

  const emailChangeMutation = useMutation({
    mutationFn: (input: EmailChangeRequestInput) =>
      apiClient.post<void>("/users/me/email-change-request", input),
  });

  return {
    ...listQuery,
    users: listQuery.data?.items ?? [],
    deleteUser: deleteMutation,
    requestEmailChange: emailChangeMutation,
  };
}
