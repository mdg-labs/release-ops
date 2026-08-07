"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  CreateInvitationInput,
  CreateInvitationResponse,
  ListInvitationsResponse,
} from "@/lib/query/types";

export function useInvitations() {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.invitations(),
    queryFn: () => apiClient.get<ListInvitationsResponse>("/users/invitations"),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.invitations() });
  };

  const createMutation = useMutation({
    mutationFn: (input: CreateInvitationInput) =>
      apiClient.post<CreateInvitationResponse>("/users/invitations", input),
    onSuccess: invalidate,
  });

  const revokeMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.delete<void>(`/users/invitations/${id}`),
    onSuccess: invalidate,
  });

  const sendEmailMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.post<void>(`/users/invitations/${id}/send-email`),
    onSuccess: invalidate,
  });

  return {
    ...listQuery,
    invitations: listQuery.data?.items ?? [],
    createInvitation: createMutation,
    revokeInvitation: revokeMutation,
    sendInvitationEmail: sendEmailMutation,
  };
}
