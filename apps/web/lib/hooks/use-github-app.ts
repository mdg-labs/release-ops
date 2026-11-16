"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { GitHubAppResponse } from "@/lib/query/types";

/** The App's metadata, its installation and the role mapping; admin only, no secret. */
export function useGitHubApp() {
  return useQuery({
    queryKey: queryKeys.githubApp(),
    queryFn: () => apiClient.get<GitHubAppResponse>("/github-app"),
  });
}
