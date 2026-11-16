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
  JobDetail,
  JobState,
  JobsResponse,
  ProblemsResponse,
} from "@/lib/query/types";

export type JobsParams = {
  page: number;
  pageSize: number;
  repositoryId?: string;
  state?: JobState;
};

export function jobsPath(params: JobsParams): string {
  const search = new URLSearchParams({
    page: String(params.page),
    pageSize: String(params.pageSize),
  });
  if (params.repositoryId) {
    search.set("repositoryId", params.repositoryId);
  }
  if (params.state) {
    search.set("state", params.state);
  }
  return `/jobs?${search.toString()}`;
}

/** GET /jobs; the rows of the previous filter stay while the next ones load. */
export function useJobs(params: JobsParams) {
  return useQuery({
    queryKey: queryKeys.jobs(params),
    queryFn: ({ signal }) =>
      apiClient.get<JobsResponse>(jobsPath(params), { signal }),
    placeholderData: keepPreviousData,
  });
}

/** GET /jobs/{id}: the job with its attempts and events; nothing is read while id is null. */
export function useJob(id: string | null) {
  return useQuery({
    queryKey: queryKeys.job(id ?? ""),
    queryFn: ({ signal }) =>
      apiClient.get<JobDetail>(`/jobs/${encodeURIComponent(id ?? "")}`, {
        signal,
      }),
    enabled: id !== null,
    retry: false,
  });
}

export type ProblemsParams = {
  page: number;
  pageSize: number;
};

/** GET /problems; the rows of the previous page stay while the next ones load. */
export function useProblems(params: ProblemsParams) {
  return useQuery({
    queryKey: queryKeys.problems(params),
    queryFn: ({ signal }) =>
      apiClient.get<ProblemsResponse>(
        `/problems?page=${params.page}&pageSize=${params.pageSize}`,
        { signal },
      ),
    placeholderData: keepPreviousData,
  });
}

/**
 * POST /update-tickets/{id}/clear-pending-marker (admin): drops the pending marker of a ticket
 * that was never stored, so the next event of its pull request creates one.
 */
export function useClearPendingMarker() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (updateTicketId: string) =>
      apiClient.post<{ id: string }>(
        `/update-tickets/${encodeURIComponent(updateTicketId)}/clear-pending-marker`,
      ),
    onSettled: () =>
      queryClient.invalidateQueries({ queryKey: queryKeys.problemsAll() }),
  });
}
