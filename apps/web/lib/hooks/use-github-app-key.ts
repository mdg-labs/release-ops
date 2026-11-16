"use client";

import { useMutation } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";

/** Replaces the GitHub App's private key. The key is sent once and never read back. */
export function useRotateGitHubAppKey() {
  return useMutation({
    mutationFn: (privateKey: string) =>
      apiClient.put<void>("/github-app/private-key", { privateKey }),
  });
}
