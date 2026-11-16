"use client";

import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type { AboutResponse } from "@/lib/query/types";

export function useAbout() {
  return useQuery({
    queryKey: queryKeys.about(),
    queryFn: () => apiClient.get<AboutResponse>("/about"),
  });
}
