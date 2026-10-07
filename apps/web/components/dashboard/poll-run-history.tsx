"use client";

import { HistoryIcon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { useEffect, useMemo, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Frame, FrameFooter, FramePanel } from "@/components/ui/frame";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Sheet,
  SheetClose,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetPanel,
  SheetPopup,
  SheetTitle,
} from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatPollDiagnosticDateTime } from "@/lib/format/datetime";
import { usePersistedPageSize } from "@/lib/hooks/use-persisted-page-size";
import { usePollRun, usePollRuns } from "@/lib/hooks/use-poll";
import {
  PAGE_SIZE_OPTIONS,
  POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY,
} from "@/lib/pagination/constants";
import { TablePaginationControls } from "@/lib/pagination/table-pagination-controls";
import { isPollEventAction } from "@/lib/poll/actions";
import type { PollRun, PollRunEvent } from "@/lib/query/types";

function runStatusVariant(
  status: string,
): "success" | "error" | "warning" | "info" | "secondary" {
  switch (status) {
    case "success":
      return "success";
    case "failed":
      return "error";
    case "partial":
      return "warning";
    case "running":
      return "info";
    default:
      return "secondary";
  }
}

function PollRunsEmptyState(): React.ReactElement {
  const t = useTranslations("dashboard");

  return (
    <Empty>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <HistoryIcon />
        </EmptyMedia>
        <EmptyTitle>{t("pollHistoryEmptyTitle")}</EmptyTitle>
        <EmptyDescription>{t("pollHistoryEmptyDescription")}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}

function RunTriggerBadge({
  triggerSource,
}: {
  triggerSource: string;
}): React.ReactElement {
  const t = useTranslations("dashboard");

  function triggerLabel(): string {
    switch (triggerSource) {
      case "manual":
        return t("runTrigger.manual");
      case "scheduled":
        return t("runTrigger.scheduled");
      default:
        return triggerSource;
    }
  }

  return <Badge variant="outline">{triggerLabel()}</Badge>;
}

function RunStatusBadge({ status }: { status: string }): React.ReactElement {
  const t = useTranslations("dashboard");

  function statusLabel(): string {
    switch (status) {
      case "success":
        return t("runStatus.success");
      case "failed":
        return t("runStatus.failed");
      case "partial":
        return t("runStatus.partial");
      case "running":
        return t("runStatus.running");
      default:
        return status;
    }
  }

  return <Badge variant={runStatusVariant(status)}>{statusLabel()}</Badge>;
}

function PollRunEventsList({
  events,
}: {
  events: PollRunEvent[];
}): React.ReactElement {
  const t = useTranslations("dashboard");
  const format = useFormatter();

  if (events.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        {t("pollHistoryNoEvents")}
      </p>
    );
  }

  return (
    <Table variant="card">
      <TableHeader>
        <TableRow>
          <TableHead>{t("pollHistoryEventColumns.time")}</TableHead>
          <TableHead>{t("pollHistoryEventColumns.action")}</TableHead>
          <TableHead>{t("pollHistoryEventColumns.detail")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {events.map((event) => {
          const actionLabel = isPollEventAction(event.action)
            ? t(`eventActions.${event.action}`)
            : event.action;

          return (
            <TableRow key={event.id}>
              <TableCell className="text-muted-foreground text-sm">
                {formatPollDiagnosticDateTime(
                  format,
                  event.createdAt,
                  t("noEventDetail"),
                )}
              </TableCell>
              <TableCell>
                <Badge variant="outline">{actionLabel}</Badge>
              </TableCell>
              <TableCell className="max-w-xs truncate text-sm">
                {event.detail ?? t("noEventDetail")}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function PollRunDetailDrawer({
  runId,
  open,
  onOpenChange,
}: {
  runId: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): React.ReactElement {
  const t = useTranslations("dashboard");
  const tCommon = useTranslations("common");
  const format = useFormatter();
  const { data: run, isLoading, isError } = usePollRun(runId ?? undefined);

  return (
    <Sheet onOpenChange={onOpenChange} open={open}>
      <SheetPopup side="right">
        <SheetHeader>
          <SheetTitle>{t("pollHistoryDetailTitle")}</SheetTitle>
          <SheetDescription>
            {t("pollHistoryDetailDescription")}
          </SheetDescription>
        </SheetHeader>
        <SheetPanel className="flex min-h-0 flex-1 flex-col gap-4">
          {isLoading ? (
            <div className="flex flex-col gap-3">
              <Skeleton className="h-6 w-32" />
              <Skeleton className="h-24 w-full" />
              <Skeleton className="h-48 w-full" />
            </div>
          ) : null}

          {isError ? (
            <p className="text-destructive-foreground text-sm" role="alert">
              {t("pollHistoryDetailLoadFailed")}
            </p>
          ) : null}

          {!isLoading && !isError && run ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <RunStatusBadge status={run.status} />
                <RunTriggerBadge triggerSource={run.triggerSource} />
              </div>
              <dl className="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">
                    {t("pollHistoryColumns.trigger")}
                  </dt>
                  <dd className="font-medium">
                    <RunTriggerBadge triggerSource={run.triggerSource} />
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t("pollHistoryColumns.startedAt")}
                  </dt>
                  <dd className="font-medium">
                    {formatPollDiagnosticDateTime(
                      format,
                      run.startedAt,
                      t("lastRunNever"),
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t("pollHistoryColumns.finishedAt")}
                  </dt>
                  <dd className="font-medium">
                    {formatPollDiagnosticDateTime(
                      format,
                      run.finishedAt,
                      t("lastRunInProgress"),
                    )}
                  </dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t("lastRunReposChecked")}
                  </dt>
                  <dd className="font-medium">{run.reposChecked}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">
                    {t("lastRunTicketsCreated")}
                  </dt>
                  <dd className="font-medium">{run.ticketsCreated}</dd>
                </div>
              </dl>

              {run.errors.length > 0 ? (
                <div className="flex flex-col gap-2">
                  <h3 className="font-medium text-sm">
                    {t("pollHistoryErrorsTitle")}
                  </h3>
                  <ul className="list-disc ps-4 text-sm">
                    {run.errors.map((error) => (
                      <li key={`${error.repoId}-${error.message}`}>
                        <span className="font-mono">{error.repoId}</span>
                        {": "}
                        {error.message}
                      </li>
                    ))}
                  </ul>
                </div>
              ) : null}

              <div className="flex min-h-0 flex-1 flex-col gap-2">
                <h3 className="font-medium text-sm">
                  {t("pollHistoryEventsTitle")}
                </h3>
                <ScrollArea className="max-h-80">
                  <PollRunEventsList events={run.events ?? []} />
                </ScrollArea>
              </div>
            </>
          ) : null}
        </SheetPanel>
        <SheetFooter variant="bare">
          <SheetClose render={<Button variant="ghost" type="button" />}>
            {tCommon("cancel")}
          </SheetClose>
        </SheetFooter>
      </SheetPopup>
    </Sheet>
  );
}

export function PollRunHistory(): React.ReactElement {
  const t = useTranslations("dashboard");
  const format = useFormatter();
  const [pageSize, setPageSize] = usePersistedPageSize(
    POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY,
  );
  const [offset, setOffset] = useState(0);
  const [selectedRunId, setSelectedRunId] = useState<string | null>(null);

  useEffect(() => {
    setOffset(0);
  }, [pageSize]);

  const pageSizeItems = useMemo(
    () =>
      PAGE_SIZE_OPTIONS.map((size) => ({
        label: t("paginationPageSizeOption", { count: size }),
        value: size,
      })),
    [t],
  );

  const {
    data: runs = [],
    isLoading,
    isError,
  } = usePollRuns({
    limit: pageSize,
    offset,
  });

  const hasPreviousPage = offset > 0;
  const hasNextPage = runs.length === pageSize;
  const pageStart = runs.length === 0 ? 0 : offset + 1;
  const pageEnd = offset + runs.length;

  function handlePageSizeChange(size: number): void {
    setPageSize(size);
    setOffset(0);
  }

  function openRun(run: PollRun): void {
    setSelectedRunId(run.id);
  }

  if (!isLoading && !isError && runs.length === 0 && offset === 0) {
    return <PollRunsEmptyState />;
  }

  return (
    <>
      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("pollHistoryLoadFailed")}
        </p>
      ) : null}

      <Frame>
        <FramePanel className="p-0">
          <Table variant="card">
            <TableHeader>
              <TableRow>
                <TableHead>{t("pollHistoryColumns.startedAt")}</TableHead>
                <TableHead>{t("pollHistoryColumns.finishedAt")}</TableHead>
                <TableHead>{t("pollHistoryColumns.status")}</TableHead>
                <TableHead>{t("pollHistoryColumns.trigger")}</TableHead>
                <TableHead>{t("pollHistoryColumns.reposChecked")}</TableHead>
                <TableHead>{t("pollHistoryColumns.ticketsCreated")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading
                ? Array.from({ length: 3 }, (_, index) => (
                    <TableRow key={`skeleton-${index}`}>
                      <TableCell colSpan={6}>
                        <Skeleton className="h-8 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                : null}

              {!isLoading
                ? runs.map((run) => (
                    <TableRow
                      className="cursor-pointer"
                      key={run.id}
                      onClick={() => openRun(run)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          openRun(run);
                        }
                      }}
                      role="button"
                      tabIndex={0}
                    >
                      <TableCell className="text-sm">
                        {formatPollDiagnosticDateTime(
                          format,
                          run.startedAt,
                          t("lastRunNever"),
                        )}
                      </TableCell>
                      <TableCell className="text-muted-foreground text-sm">
                        {formatPollDiagnosticDateTime(
                          format,
                          run.finishedAt,
                          t("lastRunInProgress"),
                        )}
                      </TableCell>
                      <TableCell>
                        <RunStatusBadge status={run.status} />
                      </TableCell>
                      <TableCell>
                        <RunTriggerBadge triggerSource={run.triggerSource} />
                      </TableCell>
                      <TableCell>{run.reposChecked}</TableCell>
                      <TableCell>{run.ticketsCreated}</TableCell>
                    </TableRow>
                  ))
                : null}
            </TableBody>
          </Table>
        </FramePanel>

        {!isLoading && (runs.length > 0 || hasPreviousPage) ? (
          <FrameFooter>
            <TablePaginationControls
              canNextPage={hasNextPage}
              canPreviousPage={hasPreviousPage}
              nextLabel={t("paginationNext")}
              onNextPage={() => setOffset((current) => current + pageSize)}
              onPageSizeChange={handlePageSizeChange}
              onPreviousPage={() =>
                setOffset((current) => Math.max(0, current - pageSize))
              }
              pageSize={pageSize}
              pageSizeItems={pageSizeItems}
              pageSizeLabel={t("paginationPageSize")}
              previousLabel={t("paginationPrevious")}
              summary={t("paginationRange", {
                end: pageEnd,
                start: pageStart,
              })}
            />
          </FrameFooter>
        ) : null}
      </Frame>

      <PollRunDetailDrawer
        onOpenChange={(open) => {
          if (!open) {
            setSelectedRunId(null);
          }
        }}
        open={selectedRunId !== null}
        runId={selectedRunId}
      />
    </>
  );
}
