"use client";

import {
  type ColumnDef,
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  getSortedRowModel,
  type SortingState,
  useReactTable,
} from "@tanstack/react-table";
import { ChevronDownIcon, ChevronUpIcon, FolderGit2Icon } from "lucide-react";
import Link from "next/link";
import { useFormatter, useTranslations } from "next-intl";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Frame, FrameFooter, FramePanel } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { usePersistedPageSize } from "@/lib/hooks/use-persisted-page-size";
import { formatAppDateTime } from "@/lib/format/datetime";
import {
  PAGE_SIZE_OPTIONS,
  REPO_STATUS_PAGE_SIZE_STORAGE_KEY,
} from "@/lib/pagination/constants";
import { TablePaginationControls } from "@/lib/pagination/table-pagination-controls";
import type { StatusRepo } from "@/lib/query/types";

type RepoStatusTableProps = {
  repos: StatusRepo[];
  isLoading: boolean;
};

function ReposEmptyState(): React.ReactElement {
  const t = useTranslations("dashboard");

  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <FolderGit2Icon />
        </EmptyMedia>
        <EmptyTitle>{t("reposEmptyTitle")}</EmptyTitle>
        <EmptyDescription>{t("reposEmptyDescription")}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button render={<Link href="/repos" />}>{t("reposEmptyCta")}</Button>
      </EmptyContent>
    </Empty>
  );
}

export function RepoStatusTable({
  repos,
  isLoading,
}: RepoStatusTableProps): React.ReactElement {
  const t = useTranslations("dashboard");
  const tRepos = useTranslations("repos");
  const tIntegrations = useTranslations("integrations");
  const format = useFormatter();
  const [pageSize, setPageSize] = usePersistedPageSize(
    REPO_STATUS_PAGE_SIZE_STORAGE_KEY,
  );
  const [pagination, setPagination] = useState({
    pageIndex: 0,
    pageSize,
  });
  const [sorting, setSorting] = useState<SortingState>([
    { desc: false, id: "projectPath" },
  ]);

  useEffect(() => {
    setPagination((current) =>
      current.pageSize === pageSize ? current : { pageIndex: 0, pageSize },
    );
  }, [pageSize]);

  const pageSizeItems = useMemo(
    () =>
      PAGE_SIZE_OPTIONS.map((size) => ({
        label: t("paginationPageSizeOption", { count: size }),
        value: size,
      })),
    [t],
  );

  const formatLastPolledAt = useCallback(
    (value: string | null): string => {
      if (!value) {
        return tRepos("lastPolledNever");
      }

      return formatAppDateTime(format, value, tRepos("lastPolledNever"));
    },
    [format, tRepos],
  );

  const columns = useMemo<ColumnDef<StatusRepo>[]>(
    () => [
      {
        accessorKey: "sourceKind",
        cell: ({ row }) => (
          <Badge variant="outline">
            {tIntegrations(`kinds.${row.original.sourceKind}`)}
          </Badge>
        ),
        header: t("columns.source"),
        size: 120,
      },
      {
        accessorKey: "projectPath",
        cell: ({ row }) => (
          <span className="font-medium font-mono text-sm">
            {row.original.projectPath}
          </span>
        ),
        header: t("columns.path"),
        size: 220,
      },
      {
        accessorKey: "lastKnownTag",
        cell: ({ row }) => (
          <span className="text-muted-foreground">
            {row.original.lastKnownTag ?? t("noTag")}
          </span>
        ),
        header: t("columns.lastTag"),
        size: 120,
      },
      {
        accessorKey: "openTicketExternalId",
        cell: ({ row }) => {
          const ticketId = row.original.openTicketExternalId;
          const ticketTag = row.original.openTicketTag;

          if (!ticketId) {
            return (
              <span className="text-muted-foreground">{t("noTicket")}</span>
            );
          }

          return (
            <div className="flex flex-col gap-0.5">
              <span className="font-mono text-sm">{ticketId}</span>
              {ticketTag ? (
                <span className="text-muted-foreground text-xs">
                  {ticketTag}
                </span>
              ) : null}
            </div>
          );
        },
        header: t("columns.openTicket"),
        size: 180,
      },
      {
        accessorKey: "lastPolledAt",
        cell: ({ row }) => (
          <span className="text-muted-foreground text-sm">
            {formatLastPolledAt(row.original.lastPolledAt)}
          </span>
        ),
        header: t("columns.lastPolledAt"),
        size: 160,
        sortingFn: (rowA, rowB, columnId) => {
          const left = rowA.getValue(columnId) as string | null;
          const right = rowB.getValue(columnId) as string | null;

          if (!left && !right) {
            return 0;
          }
          if (!left) {
            return 1;
          }
          if (!right) {
            return -1;
          }

          return new Date(left).getTime() - new Date(right).getTime();
        },
      },
    ],
    [formatLastPolledAt, t, tIntegrations],
  );

  const table = useReactTable({
    columns,
    data: repos,
    enableSortingRemoval: false,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    onPaginationChange: setPagination,
    onSortingChange: setSorting,
    state: {
      pagination,
      sorting,
    },
  });

  function handlePageSizeChange(size: number): void {
    setPageSize(size);
    setPagination({ pageIndex: 0, pageSize: size });
  }

  if (!isLoading && repos.length === 0) {
    return <ReposEmptyState />;
  }

  return (
    <Frame>
      <FramePanel className="p-0">
        <Table variant="card">
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const columnSize = header.column.getSize();
                  return (
                    <TableHead
                      key={header.id}
                      style={
                        columnSize ? { width: `${columnSize}px` } : undefined
                      }
                    >
                      {header.isPlaceholder ? null : header.column.getCanSort() ? (
                        <div
                          className="flex h-full cursor-pointer select-none items-center justify-between gap-2"
                          onClick={header.column.getToggleSortingHandler()}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              header.column.getToggleSortingHandler()?.(event);
                            }
                          }}
                          role="button"
                          tabIndex={0}
                        >
                          {flexRender(
                            header.column.columnDef.header,
                            header.getContext(),
                          )}
                          {{
                            asc: (
                              <ChevronUpIcon
                                aria-hidden="true"
                                className="size-4 shrink-0 opacity-80"
                              />
                            ),
                            desc: (
                              <ChevronDownIcon
                                aria-hidden="true"
                                className="size-4 shrink-0 opacity-80"
                              />
                            ),
                          }[header.column.getIsSorted() as string] ?? null}
                        </div>
                      ) : (
                        flexRender(
                          header.column.columnDef.header,
                          header.getContext(),
                        )
                      )}
                    </TableHead>
                  );
                })}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {isLoading
              ? Array.from({ length: 3 }, (_, index) => (
                  <TableRow key={`skeleton-${index}`}>
                    <TableCell colSpan={columns.length}>
                      <Skeleton className="h-8 w-full" />
                    </TableCell>
                  </TableRow>
                ))
              : null}

            {!isLoading && table.getRowModel().rows.length > 0
              ? table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext(),
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              : null}

            {!isLoading && table.getRowModel().rows.length === 0 ? (
              <TableRow>
                <TableCell
                  className="text-muted-foreground"
                  colSpan={columns.length}
                >
                  {t("noResults")}
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </FramePanel>

      {!isLoading && repos.length > 0 ? (
        <FrameFooter>
          <TablePaginationControls
            canNextPage={table.getCanNextPage()}
            canPreviousPage={table.getCanPreviousPage()}
            nextLabel={t("paginationNext")}
            onNextPage={() => table.nextPage()}
            onPageSizeChange={handlePageSizeChange}
            onPreviousPage={() => table.previousPage()}
            pageSize={pageSize}
            pageSizeItems={pageSizeItems}
            pageSizeLabel={t("paginationPageSize")}
            previousLabel={t("paginationPrevious")}
            summary={t("paginationSummary", {
              end: Math.min(
                (table.getState().pagination.pageIndex + 1) * pageSize,
                table.getRowCount(),
              ),
              start: table.getState().pagination.pageIndex * pageSize + 1,
              total: table.getRowCount(),
            })}
          />
        </FrameFooter>
      ) : null}
    </Frame>
  );
}
