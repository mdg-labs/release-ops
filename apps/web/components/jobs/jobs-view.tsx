"use client";

import { ListChecksIcon, ShieldCheckIcon } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { PageHeader } from "@/components/common/page-header";
import { ClearMarkerDialog } from "@/components/jobs/clear-marker-dialog";
import { JobDetailDialog } from "@/components/jobs/job-detail-dialog";
import { JobFilters } from "@/components/jobs/job-filters";
import { JobsTable } from "@/components/jobs/jobs-table";
import { ProblemsTable } from "@/components/jobs/problems-table";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { usePersistedPageSize } from "@/lib/hooks/use-persisted-page-size";
import { useSessionRole } from "@/lib/hooks/use-session";
import { PAGE_SIZE_OPTIONS } from "@/lib/pagination/constants";
import { TablePaginationControls } from "@/lib/pagination/table-pagination-controls";
import { useClampPage } from "@/lib/pagination/use-clamp-page";
import { useJobs, useProblems } from "@/lib/query/jobs";
import { useRepositoryOptions } from "@/lib/query/repositories";
import type { JobState } from "@/lib/query/types";

type View = "jobs" | "problems";

function LoadFailed({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}): React.ReactElement {
  const t = useTranslations("jobs");
  return (
    <Alert variant="error">
      <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
        <span>{message}</span>
        <Button onClick={onRetry} size="sm" variant="outline">
          {t("retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

function Skeletons({ testId }: { testId: string }): React.ReactElement {
  return (
    <Frame>
      <FramePanel className="space-y-2 p-4">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton
            className="h-8 w-full"
            data-testid={testId}
            key={`skeleton-${index}`}
          />
        ))}
      </FramePanel>
    </Frame>
  );
}

function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
}: {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
}): React.ReactElement {
  const t = useTranslations("jobs.pagination");
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  return (
    <TablePaginationControls
      canNextPage={page * pageSize < total}
      canPreviousPage={page > 1}
      nextLabel={t("next")}
      onNextPage={() => onPageChange(page + 1)}
      onPageSizeChange={onPageSizeChange}
      onPreviousPage={() => onPageChange(page - 1)}
      pageSize={pageSize}
      pageSizeItems={PAGE_SIZE_OPTIONS.map((size) => ({
        label: t("pageSizeOption", { count: size }),
        value: size,
      }))}
      pageSizeLabel={t("pageSize")}
      previousLabel={t("previous")}
      summary={t("summary", { from, to, total })}
    />
  );
}

function JobsSection({
  onOpenJob,
}: {
  onOpenJob: (id: string) => void;
}): React.ReactElement {
  const t = useTranslations("jobs");
  const [repositoryId, setRepositoryId] = useState("");
  const [state, setState] = useState<JobState | "">("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePersistedPageSize("jobs-page-size");
  const repositories = useRepositoryOptions();
  const query = useJobs({
    page,
    pageSize,
    ...(repositoryId ? { repositoryId } : {}),
    ...(state ? { state } : {}),
  });
  const { data, isPending, isError, isPlaceholderData, refetch } = query;

  const filtered = repositoryId !== "" || state !== "";
  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  useClampPage({
    page,
    pageSize,
    result:
      data && !isPlaceholderData ? { itemCount: items.length, total } : null,
    onPageChange: setPage,
  });

  let body: React.ReactElement | null = null;
  if (isError) {
    body = (
      <LoadFailed message={t("loadFailed")} onRetry={() => void refetch()} />
    );
  } else if (isPending) {
    body = <Skeletons testId="jobs-skeleton" />;
  } else if (data && items.length === 0 && !filtered) {
    body = (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ListChecksIcon aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{t("empty.title")}</EmptyTitle>
          <EmptyDescription>{t("empty.description")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button render={<Link href="/repositories" />}>
            {t("empty.action")}
          </Button>
        </EmptyContent>
      </Empty>
    );
  } else if (data && items.length === 0) {
    body = (
      <p className="text-muted-foreground text-sm">{t("empty.filtered")}</p>
    );
  } else if (data) {
    body = (
      <>
        <Frame>
          <FramePanel
            aria-busy={isPlaceholderData}
            className={isPlaceholderData ? "p-0 opacity-60" : "p-0"}
          >
            <JobsTable items={items} onOpenJob={onOpenJob} />
          </FramePanel>
        </Frame>
        <Pagination
          onPageChange={setPage}
          onPageSizeChange={(size) => {
            setPageSize(size);
            setPage(1);
          }}
          page={page}
          pageSize={pageSize}
          total={total}
        />
      </>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <JobFilters
        onRepositoryChange={(id) => {
          setRepositoryId(id);
          setPage(1);
        }}
        onStateChange={(next) => {
          setState(next);
          setPage(1);
        }}
        repositories={repositories.isError ? [] : (repositories.data ?? [])}
        repositoryId={repositoryId}
        state={state}
      />
      {body}
    </div>
  );
}

function ProblemsSection({
  isAdmin,
  onOpenJob,
}: {
  isAdmin: boolean;
  onOpenJob: (id: string) => void;
}): React.ReactElement {
  const t = useTranslations("jobs");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePersistedPageSize("problems-page-size");
  const [clearing, setClearing] = useState<{
    updateTicketId: string;
    repository: string;
  } | null>(null);
  const query = useProblems({ page, pageSize });
  const { data, isPending, isError, isPlaceholderData, refetch } = query;

  const items = data?.items ?? [];
  const total = data?.total ?? 0;

  let body: React.ReactElement | null = null;
  if (isError) {
    body = (
      <LoadFailed
        message={t("problems.loadFailed")}
        onRetry={() => void refetch()}
      />
    );
  } else if (isPending) {
    body = <Skeletons testId="problems-skeleton" />;
  } else if (data && total === 0 && page === 1) {
    body = (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <ShieldCheckIcon aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{t("problems.emptyTitle")}</EmptyTitle>
          <EmptyDescription>{t("problems.emptyDescription")}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  } else if (data) {
    body = (
      <>
        <Frame>
          <FramePanel
            aria-busy={isPlaceholderData}
            className={isPlaceholderData ? "p-0 opacity-60" : "p-0"}
          >
            <ProblemsTable
              isAdmin={isAdmin}
              items={items}
              onClearMarker={(updateTicketId, repository) =>
                setClearing({ updateTicketId, repository })
              }
              onOpenJob={onOpenJob}
            />
          </FramePanel>
        </Frame>
        <Pagination
          onPageChange={setPage}
          onPageSizeChange={(size) => {
            setPageSize(size);
            setPage(1);
          }}
          page={page}
          pageSize={pageSize}
          total={total}
        />
      </>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {body}
      <ClearMarkerDialog
        onClose={() => setClearing(null)}
        repository={clearing?.repository ?? ""}
        updateTicketId={clearing?.updateTicketId ?? null}
      />
    </div>
  );
}

export function JobsView(): React.ReactElement {
  const t = useTranslations("jobs");
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { role } = useSessionRole();
  // The address decides the view and the open job, so a link to a job or to the Problems view
  // lands there and a link to /jobs without parameters goes back to the job list.
  const view: View =
    searchParams.get("view") === "problems" ? "problems" : "jobs";
  const jobId = searchParams.get("job") || null;

  function sync(nextView: View, nextJob: string | null): void {
    const search = new URLSearchParams();
    if (nextView === "problems") {
      search.set("view", "problems");
    }
    if (nextJob) {
      search.set("job", nextJob);
    }
    const query = search.toString();
    router.replace(query ? `${pathname}?${query}` : pathname, {
      scroll: false,
    });
  }

  function changeView(next: View): void {
    sync(next, jobId);
  }

  function openJob(id: string | null): void {
    sync(view, id);
  }

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <PageHeader description={t("description")} title={t("title")} />
      <Tabs onValueChange={(value) => changeView(value as View)} value={view}>
        <TabsList aria-label={t("tabsLabel")} variant="underline">
          <TabsTab value="jobs">{t("tabs.jobs")}</TabsTab>
          <TabsTab value="problems">{t("tabs.problems")}</TabsTab>
        </TabsList>
        <TabsPanel className="pt-2" value="jobs">
          <JobsSection onOpenJob={openJob} />
        </TabsPanel>
        <TabsPanel className="pt-2" value="problems">
          <ProblemsSection isAdmin={role === "admin"} onOpenJob={openJob} />
        </TabsPanel>
      </Tabs>
      <JobDetailDialog jobId={jobId} onClose={() => openJob(null)} />
    </div>
  );
}
