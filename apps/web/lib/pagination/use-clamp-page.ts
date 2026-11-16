"use client";

import { useEffect } from "react";

type ClampPageOptions = {
  page: number;
  pageSize: number;
  /** The rows and the total of the current page's own answer; null while there is none. */
  result: { itemCount: number; total: number } | null;
  onPageChange: (page: number) => void;
};

/**
 * Moves to the last page that exists when the current one answers with no rows although rows
 * remain, which happens when the list shrinks while the reader is on a later page.
 */
export function useClampPage({
  page,
  pageSize,
  result,
  onPageChange,
}: ClampPageOptions): void {
  const itemCount = result?.itemCount;
  const total = result?.total;

  useEffect(() => {
    if (itemCount !== 0 || total === undefined || total <= 0 || page <= 1) {
      return;
    }
    const lastPage = Math.max(1, Math.ceil(total / pageSize));
    if (lastPage !== page) {
      onPageChange(lastPage);
    }
  }, [itemCount, total, page, pageSize, onPageChange]);
}
