"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  CreateTicketProjectInput,
  TicketProject,
  UpdateTicketProjectInput,
} from "@/lib/query/types";

function ticketProjectsPath(integrationId?: string): string {
  if (integrationId) {
    return `/ticket-projects?integrationId=${encodeURIComponent(integrationId)}`;
  }
  return "/ticket-projects";
}

export function useTicketProjects(integrationId?: string) {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.ticketProjects(integrationId),
    queryFn: () =>
      apiClient.get<TicketProject[]>(ticketProjectsPath(integrationId)),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["ticket-projects"] });
    queryClient.invalidateQueries({ queryKey: queryKeys.status() });
  };

  const createMutation = useMutation({
    mutationFn: (input: CreateTicketProjectInput) =>
      apiClient.post<TicketProject>("/ticket-projects", input),
    onSuccess: invalidate,
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateTicketProjectInput;
    }) => apiClient.patch<TicketProject>(`/ticket-projects/${id}`, input),
    onSuccess: invalidate,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.delete<void>(`/ticket-projects/${id}`),
    onSuccess: invalidate,
  });

  return {
    ...listQuery,
    createTicketProject: createMutation,
    updateTicketProject: updateMutation,
    deleteTicketProject: deleteMutation,
  };
}
