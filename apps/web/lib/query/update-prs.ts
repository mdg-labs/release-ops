"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/query/keys";
import type {
  UpdatePrSort,
  UpdatePrsResponse,
  UpdatePrState,
  UpdatePrType,
} from "@/lib/query/types";

export type UpdatePrsParams = {
  state: UpdatePrState;
  sort: UpdatePrSort;
  page: number;
  pageSize: number;
  repositoryId?: string;
  updateType?: UpdatePrType;
};

export function updatePrsPath(params: UpdatePrsParams): string {
  const search = new URLSearchParams({
    state: params.state,
    sort: params.sort,
    page: String(params.page),
    pageSize: String(params.pageSize),
  });
  if (params.repositoryId) {
    search.set("repositoryId", params.repositoryId);
  }
  if (params.updateType) {
    search.set("updateType", params.updateType);
  }
  return `/update-prs?${search.toString()}`;
}

/** GET /update-prs; the rows of the previous filter stay while the next ones load. */
export function useUpdatePrs(params: UpdatePrsParams) {
  return useQuery({
    queryKey: queryKeys.updatePrs(params),
    queryFn: ({ signal }) =>
      apiClient.get<UpdatePrsResponse>(updatePrsPath(params), { signal }),
    placeholderData: keepPreviousData,
  });
}
