"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  CreateNotificationTargetInput,
  NotificationTarget,
  TestConnectionResponse,
  UpdateNotificationTargetInput,
} from "@/lib/query/types";

export function useNotificationTargets() {
  const queryClient = useQueryClient();

  const listQuery = useQuery({
    queryKey: queryKeys.notificationTargets(),
    queryFn: () => apiClient.get<NotificationTarget[]>("/notification-targets"),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({
      queryKey: queryKeys.notificationTargets(),
    });
  };

  const createMutation = useMutation({
    mutationFn: (input: CreateNotificationTargetInput) =>
      apiClient.post<NotificationTarget>("/notification-targets", input),
    onSuccess: invalidate,
  });

  const updateMutation = useMutation({
    mutationFn: ({
      id,
      input,
    }: {
      id: string;
      input: UpdateNotificationTargetInput;
    }) =>
      apiClient.patch<NotificationTarget>(`/notification-targets/${id}`, input),
    onSuccess: invalidate,
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.delete<void>(`/notification-targets/${id}`),
    onSuccess: invalidate,
  });

  const testMutation = useMutation({
    mutationFn: (id: string) =>
      apiClient.post<TestConnectionResponse>(
        `/notification-targets/${id}/test`,
      ),
  });

  return {
    ...listQuery,
    createNotificationTarget: createMutation,
    updateNotificationTarget: updateMutation,
    deleteNotificationTarget: deleteMutation,
    testNotificationTarget: testMutation,
  };
}
