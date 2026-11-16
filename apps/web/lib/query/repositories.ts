"use client";

import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  EnableRenovateResult,
  RepositoriesResponse,
  Repository,
  RepositoryPatch,
} from "@/lib/query/types";

export type RepositoriesParams = {
  page: number;
  pageSize: number;
  search?: string;
};

export function repositoriesPath(params: RepositoriesParams): string {
  const search = new URLSearchParams({
    page: String(params.page),
    pageSize: String(params.pageSize),
  });
  if (params.search) {
    search.set("search", params.search);
  }
  return `/repositories?${search.toString()}`;
}

/** GET /repositories; the rows of the previous page or search stay while the next ones load. */
export function useRepositories(params: RepositoriesParams) {
  return useQuery({
    queryKey: queryKeys.repositories(params),
    queryFn: ({ signal }) =>
      apiClient.get<RepositoriesResponse>(repositoriesPath(params), { signal }),
    placeholderData: keepPreviousData,
  });
}

/** PATCH /repositories/{id}: pause or enable, and set or clear the ticket project override. */
export function useUpdateRepository() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, patch }: { id: string; patch: RepositoryPatch }) =>
      apiClient.patch<Repository>(
        `/repositories/${encodeURIComponent(id)}`,
        patch,
      ),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.repositoriesAll() }),
  });
}

/** POST /repositories/{id}/run: run now; the answer is the job that was queued or is running. */
export function useRunRepository() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiClient.post<{ jobId: string }>(
        `/repositories/${encodeURIComponent(id)}/run`,
      ),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.repositoriesAll() }),
  });
}

/** POST /repositories/{id}/enable-renovate: opens the pull request that adds renovate.json. */
export function useEnableRenovate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      apiClient.post<EnableRenovateResult>(
        `/repositories/${encodeURIComponent(id)}/enable-renovate`,
      ),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.repositoriesAll() }),
  });
}
