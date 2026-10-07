export const DEFAULT_PAGE_SIZE = 10;

export const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;

export type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];

export const MAX_PAGE_SIZE = 100;

export const REPO_STATUS_PAGE_SIZE_STORAGE_KEY =
  "release-ops:dashboard:repo-status-page-size";

export const POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY =
  "release-ops:dashboard:poll-run-history-page-size";
