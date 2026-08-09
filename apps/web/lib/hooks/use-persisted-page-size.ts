"use client";

import { useCallback, useEffect, useState } from "react";
import {
  DEFAULT_PAGE_SIZE,
  MAX_PAGE_SIZE,
  PAGE_SIZE_OPTIONS,
  type PageSize,
} from "@/lib/pagination/constants";

export type UsePersistedPageSizeOptions = {
  options?: readonly number[];
  max?: number;
};

function isAllowedPageSize(
  value: number,
  options: readonly number[],
  max: number,
): boolean {
  return (
    Number.isInteger(value) &&
    value > 0 &&
    value <= max &&
    options.includes(value)
  );
}

function readStoredPageSize(
  storageKey: string,
  defaultSize: number,
  options: readonly number[],
  max: number,
): number {
  if (typeof window === "undefined") {
    return defaultSize;
  }

  try {
    const stored = window.localStorage.getItem(storageKey);
    if (stored === null) {
      return defaultSize;
    }

    const parsed = Number.parseInt(stored, 10);
    if (isAllowedPageSize(parsed, options, max)) {
      return parsed;
    }
  } catch {
    // Ignore storage read failures.
  }

  return defaultSize;
}

export function usePersistedPageSize(
  storageKey: string,
  defaultSize: number = DEFAULT_PAGE_SIZE,
  hookOptions?: UsePersistedPageSizeOptions,
): [number, (size: number) => void] {
  const options = hookOptions?.options ?? PAGE_SIZE_OPTIONS;
  const max = hookOptions?.max ?? MAX_PAGE_SIZE;
  const resolvedDefault = isAllowedPageSize(defaultSize, options, max)
    ? defaultSize
    : DEFAULT_PAGE_SIZE;

  const [pageSize, setPageSizeState] = useState(resolvedDefault);

  useEffect(() => {
    setPageSizeState(
      readStoredPageSize(storageKey, resolvedDefault, options, max),
    );
  }, [storageKey, resolvedDefault, options, max]);

  const setPageSize = useCallback(
    (size: number) => {
      if (!isAllowedPageSize(size, options, max)) {
        return;
      }

      setPageSizeState(size);

      try {
        window.localStorage.setItem(storageKey, String(size));
      } catch {
        // Ignore storage write failures.
      }
    },
    [storageKey, options, max],
  );

  return [pageSize, setPageSize];
}

export function isPageSize(value: number): value is PageSize {
  return PAGE_SIZE_OPTIONS.includes(value as PageSize);
}
