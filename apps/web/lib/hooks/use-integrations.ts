"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  CreateIntegrationInput,
  Integration,
  TestConnectionResponse,
  UpdateIntegrationInput,
} from "@/lib/query/types";

export function useIntegrations() {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.integrations(),
    queryFn: () => apiClient.get<Integration[]>("/integrations"),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: queryKeys.integrations() });
  };

  const createMutation = useMutation({
    mutationFn: (input: CreateIntegrationInput) =>
      apiClient.post<Integration>("/integrations", input),
    onSuccess: invalidate,
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateIntegrationInput;
    }) => apiClient.patch<Integration>(`/integrations/${id}`, input),
    onSuccess: invalidate,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => apiClient.delete<void>(`/integrations/${id}`),
    onSuccess: invalidate,
  });

  const testMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.post<TestConnectionResponse>(`/integrations/${id}/test`),
  });

  return {
    ...listQuery,
    createIntegration: createMutation,
    updateIntegration: updateMutation,
    deleteIntegration: deleteMutation,
    testIntegration: testMutation,
  };
}
