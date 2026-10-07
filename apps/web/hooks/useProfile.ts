"use client";

import { useMutation } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import type {
  EmailChangeRequestInput,
  PasswordChangeInput,
} from "@/lib/query/types";

export function useProfile() {
  const emailChangeMutation = useMutation({
    mutationFn: (input: EmailChangeRequestInput) =>
      apiClient.post<void>("/users/me/email-change-request", input),
  });

  const passwordChangeMutation = useMutation({
    mutationFn: (input: PasswordChangeInput) =>
      apiClient.post<void>("/users/me/password-change", input),
  });

  return {
    requestEmailChange: emailChangeMutation,
    changePassword: passwordChangeMutation,
  };
}
