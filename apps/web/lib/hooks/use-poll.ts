"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  PollRun,
  PollRunsParams,
  TriggerPollResponse,
} from "@/lib/query/types";

function pollRunsPath(params?: PollRunsParams): string {
  const search = new URLSearchParams();
  if (params?.limit !== undefined) {
    search.set("limit", String(params.limit));
  }
  if (params?.offset !== undefined) {
    search.set("offset", String(params.offset));
  }
  const query = search.toString();
  return query ? `/poll/runs?${query}` : "/poll/runs";
}

export function usePollRuns(params?: PollRunsParams) {
  return useQuery({
    queryKey: queryKeys.pollRuns(params),
    queryFn: () => apiClient.get<PollRun[]>(pollRunsPath(params)),
  });
}

export function usePollRun(id: string | undefined) {
  return useQuery({
    queryKey: queryKeys.pollRun(id ?? ""),
    queryFn: () => apiClient.get<PollRun>(`/poll/runs/${id}`),
    enabled: Boolean(id),
  });
}

export function useTriggerPoll() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: () => apiClient.post<TriggerPollResponse>("/poll/trigger"),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["poll-runs"] });
      queryClient.invalidateQueries({ queryKey: queryKeys.status() });
    },
  });
}
