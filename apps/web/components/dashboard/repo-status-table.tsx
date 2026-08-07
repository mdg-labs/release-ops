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
import { useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import type { StatusRepo } from "@/lib/query/types";

const PAGE_SIZE = 10;

type RepoStatusTableProps = {
  repos: StatusRepo[];
  isLoading: boolean;
};

function ReposEmptyState(): React.ReactElement {
  const t = useTranslations("dashboard");

  return (
    <div
      className="flex min-w-0 flex-1 flex-col items-center justify-center gap-6 px-6 py-12 text-center text-balance md:py-20"
      data-slot="empty"
    >
      <div
        className="flex max-w-sm flex-col items-center text-center"
        data-slot="empty-header"
      >
        <div
          className="relative mb-6"
          data-slot="empty-media"
          data-variant="icon"
        >
          <div
            aria-hidden="true"
            className="pointer-events-none absolute bottom-px origin-bottom-left -translate-x-0.5 -rotate-10 scale-84 border bg-card shadow-none before:pointer-events-none before:absolute before:inset-0 before:rounded-[calc(var(--radius-md)-1px)] before:shadow-[0_1px_--theme(--color-black/4%)] flex size-9 shrink-0 items-center justify-center rounded-md not-dark:bg-clip-padding text-foreground [&_svg:not([class*='size-'])]:size-4.5"
          />
          <div
            aria-hidden="true"
            className="pointer-events-none absolute bottom-px origin-bottom-right translate-x-0.5 rotate-10 scale-84 border bg-card shadow-none before:pointer-events-none before:absolute before:inset-0 before:rounded-[calc(var(--radius-md)-1px)] before:shadow-[0_1px_--theme(--color-black/4%)] flex size-9 shrink-0 items-center justify-center rounded-md not-dark:bg-clip-padding text-foreground [&_svg:not([class*='size-'])]:size-4.5"
          />
          <div className="relative flex size-9 shrink-0 items-center justify-center rounded-md border bg-card not-dark:bg-clip-padding text-foreground shadow-sm/5 before:pointer-events-none before:absolute before:inset-0 before:rounded-[calc(var(--radius-md)-1px)] before:shadow-[0_1px_--theme(--color-black/4%)] [&_svg:not([class*='size-'])]:size-4.5">
            <FolderGit2Icon />
          </div>
        </div>
        <div
          className="font-heading font-semibold text-xl"
          data-slot="empty-title"
        >
          {t("reposEmptyTitle")}
        </div>
        <div
          className="text-muted-foreground text-sm [[data-slot=empty-title]+&]:mt-1"
          data-slot="empty-description"
        >
          {t("reposEmptyDescription")}
        </div>
      </div>
      <div
        className="flex w-full min-w-0 max-w-sm flex-col items-center gap-4 text-balance text-sm"
        data-slot="empty-content"
      >
        <Button render={<Link href="/repos" />}>{t("reposEmptyCta")}</Button>
      </div>
    </div>
  );
}

export function RepoStatusTable({
  repos,
  isLoading,
}: RepoStatusTableProps): React.ReactElement {
  const t = useTranslations("dashboard");
  const tIntegrations = useTranslations("integrations");
  const [sorting, setSorting] = useState<SortingState>([
    { desc: false, id: "projectPath" },
  ]);

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
    ],
    [t, tIntegrations],
  );

  const table = useReactTable({
    columns,
    data: repos,
    enableSortingRemoval: false,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getSortedRowModel: getSortedRowModel(),
    initialState: {
      pagination: {
        pageIndex: 0,
        pageSize: PAGE_SIZE,
      },
    },
    onSortingChange: setSorting,
    state: {
      sorting,
    },
  });

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

      {!isLoading && table.getPageCount() > 1 ? (
        <FrameFooter className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-muted-foreground text-sm">
            {t("paginationSummary", {
              end: Math.min(
                (table.getState().pagination.pageIndex + 1) * PAGE_SIZE,
                table.getRowCount(),
              ),
              start: table.getState().pagination.pageIndex * PAGE_SIZE + 1,
              total: table.getRowCount(),
            })}
          </p>
          <div className="flex gap-2">
            <Button
              disabled={!table.getCanPreviousPage()}
              onClick={() => table.previousPage()}
              size="sm"
              variant="outline"
            >
              {t("paginationPrevious")}
            </Button>
            <Button
              disabled={!table.getCanNextPage()}
              onClick={() => table.nextPage()}
              size="sm"
              variant="outline"
            >
              {t("paginationNext")}
            </Button>
          </div>
        </FrameFooter>
      ) : null}
    </Frame>
  );
}
