"use client";

import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

type PageSizeItem = {
  label: string;
  value: number;
};

type TablePaginationControlsProps = {
  summary: ReactNode;
  pageSize: number;
  pageSizeItems: PageSizeItem[];
  pageSizeLabel: string;
  onPageSizeChange: (size: number) => void;
  canPreviousPage: boolean;
  canNextPage: boolean;
  onPreviousPage: () => void;
  onNextPage: () => void;
  previousLabel: string;
  nextLabel: string;
};

export function TablePaginationControls({
  summary,
  pageSize,
  pageSizeItems,
  pageSizeLabel,
  onPageSizeChange,
  canPreviousPage,
  canNextPage,
  onPreviousPage,
  onNextPage,
  previousLabel,
  nextLabel,
}: TablePaginationControlsProps): React.ReactElement {
  const selectedItem =
    pageSizeItems.find((item) => item.value === pageSize) ?? null;

  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-muted-foreground text-sm">{summary}</p>
      <div className="flex flex-wrap items-center gap-2">
        <Select
          itemToStringValue={(item) => String(item.value)}
          items={pageSizeItems}
          onValueChange={(value) => {
            if (value) {
              onPageSizeChange(value.value);
            }
          }}
          value={selectedItem}
        >
          <SelectTrigger
            aria-label={pageSizeLabel}
            className="w-fit min-w-0"
            size="sm"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectPopup>
            {pageSizeItems.map((item) => (
              <SelectItem key={item.value} value={item}>
                {item.label}
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
        <Button
          disabled={!canPreviousPage}
          onClick={onPreviousPage}
          size="sm"
          variant="outline"
        >
          {previousLabel}
        </Button>
        <Button
          disabled={!canNextPage}
          onClick={onNextPage}
          size="sm"
          variant="outline"
        >
          {nextLabel}
        </Button>
      </div>
    </div>
  );
}
