"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { CreateRepoInput, Repo, UpdateRepoInput } from "@/lib/query/types";

export function useRepos() {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.repos(),
    queryFn: () => apiClient.get<Repo[]>("/repos"),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.repos() });
    queryClient.invalidateQueries({ queryKey: queryKeys.status() });
  };

  const createMutation = useMutation({
    mutationFn: (input: CreateRepoInput) =>
      apiClient.post<Repo>("/repos", input),
    onSuccess: invalidate,
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, input }: { id: string; input: UpdateRepoInput }) =>
      apiClient.patch<Repo>(`/repos/${id}`, input),
    onSuccess: invalidate,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.delete<void>(`/repos/${id}`),
    onSuccess: invalidate,
  });

  return {
    ...listQuery,
    createRepo: createMutation,
    updateRepo: updateMutation,
    deleteRepo: deleteMutation,
  };
}
