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

export type RepositoryOption = { id: string; fullName: string };

/** The API's largest page (docs/specs.html §7 conventions). */
const OPTIONS_PAGE_SIZE = 100;

/** Stops the walk if `total` keeps growing; a list that is not complete is an error. */
const OPTIONS_MAX_PAGES = 100;

/**
 * Every repository the caller can see, for a filter: GET /repositories read page after page
 * until `total` rows are in. A failed page fails the query, so a partial list is never offered
 * as complete.
 */
export function useRepositoryOptions() {
  return useQuery({
    queryKey: queryKeys.repositoryOptions(),
    queryFn: async ({ signal }): Promise<RepositoryOption[]> => {
      const options: RepositoryOption[] = [];
      for (let page = 1; page <= OPTIONS_MAX_PAGES; page++) {
        const response = await apiClient.get<RepositoriesResponse>(
          repositoriesPath({ page, pageSize: OPTIONS_PAGE_SIZE }),
          { signal },
        );
        for (const repository of response.items) {
          options.push({ id: repository.id, fullName: repository.fullName });
        }
        if (response.items.length === 0 || options.length >= response.total) {
          return options.sort((a, b) => a.fullName.localeCompare(b.fullName));
        }
      }
      throw new Error("The repository list is longer than it can be read.");
    },
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
