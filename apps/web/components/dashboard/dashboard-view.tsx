"use client";

import { LayoutDashboardIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { PageHeader } from "@/components/common/page-header";
import {
  type RepositoryOption,
  UpdatePrFilters,
} from "@/components/dashboard/update-pr-filters";
import {
  UpdatePrTable,
  type UpdatePrTimeColumn,
} from "@/components/dashboard/update-pr-table";
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
import { usePersistedPageSize } from "@/lib/hooks/use-persisted-page-size";
import { TablePaginationControls } from "@/lib/pagination/table-pagination-controls";
import { PAGE_SIZE_OPTIONS } from "@/lib/pagination/constants";
import type { UpdatePr, UpdatePrType } from "@/lib/query/types";
import { type UpdatePrsParams, useUpdatePrs } from "@/lib/query/update-prs";

/** The recently merged list is the ten most recent merges (docs/specs.html §8.3). */
const MERGED_LIMIT = 10;

type ListSectionProps = {
  title: string;
  emptyText: string;
  timeColumn: UpdatePrTimeColumn;
  params: UpdatePrsParams;
  paginated?: {
    onPageChange: (page: number) => void;
    onPageSizeChange: (size: number) => void;
  };
  onItems: (items: UpdatePr[]) => void;
  onTotal: (total: number | null) => void;
};

function ListSection({
  title,
  emptyText,
  timeColumn,
  params,
  paginated,
  onItems,
  onTotal,
}: ListSectionProps): React.ReactElement {
  const t = useTranslations("dashboard");
  const query = useUpdatePrs(params);
  const { data, isPending, isError, isPlaceholderData, refetch } = query;

  useEffect(() => {
    if (data) {
      onItems(data.items);
    }
    onTotal(data && !isError ? data.total : null);
  }, [data, isError, onItems, onTotal]);

  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  const from = total === 0 ? 0 : (params.page - 1) * params.pageSize + 1;
  const to = Math.min(params.page * params.pageSize, total);

  return (
    <section aria-labelledby={`${timeColumn}-title`} className="space-y-3">
      <h2 className="font-semibold text-lg" id={`${timeColumn}-title`}>
        {title}
      </h2>
      {isError ? (
        <Alert variant="error">
          <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
            <span>{t("loadFailed")}</span>
            <Button onClick={() => void refetch()} size="sm" variant="outline">
              {t("retry")}
            </Button>
          </AlertDescription>
        </Alert>
      ) : null}
      {isPending ? (
        <Frame>
          <FramePanel className="space-y-2 p-4">
            {Array.from({ length: 3 }, (_, index) => (
              <Skeleton
                className="h-8 w-full"
                data-testid="dashboard-skeleton"
                key={`skeleton-${index}`}
              />
            ))}
          </FramePanel>
        </Frame>
      ) : data && items.length === 0 ? (
        <p className="text-muted-foreground text-sm">{emptyText}</p>
      ) : data ? (
        <Frame>
          <FramePanel
            aria-busy={isPlaceholderData}
            className={isPlaceholderData ? "p-0 opacity-60" : "p-0"}
          >
            <UpdatePrTable items={items} timeColumn={timeColumn} />
          </FramePanel>
        </Frame>
      ) : null}
      {paginated && data && total > 0 ? (
        <TablePaginationControls
          canNextPage={params.page * params.pageSize < total}
          canPreviousPage={params.page > 1}
          nextLabel={t("pagination.next")}
          onNextPage={() => paginated.onPageChange(params.page + 1)}
          onPageSizeChange={paginated.onPageSizeChange}
          onPreviousPage={() => paginated.onPageChange(params.page - 1)}
          pageSize={params.pageSize}
          pageSizeItems={PAGE_SIZE_OPTIONS.map((size) => ({
            label: t("pagination.pageSizeOption", { count: size }),
            value: size,
          }))}
          pageSizeLabel={t("pagination.pageSize")}
          previousLabel={t("pagination.previous")}
          summary={t("pagination.summary", { from, to, total })}
        />
      ) : null}
    </section>
  );
}

export function DashboardView(): React.ReactElement {
  const t = useTranslations("dashboard");
  const [repositoryId, setRepositoryId] = useState("");
  const [updateType, setUpdateType] = useState<UpdatePrType | "">("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePersistedPageSize("dashboard-page-size");
  // The repositories seen so far, so the filter keeps offering them after it narrows the list.
  const [repositories, setRepositories] = useState<RepositoryOption[]>([]);
  const [openTotal, setOpenTotal] = useState<number | null>(null);
  const [mergedTotal, setMergedTotal] = useState<number | null>(null);

  const filters = {
    ...(repositoryId ? { repositoryId } : {}),
    ...(updateType ? { updateType } : {}),
  };
  const filtered = repositoryId !== "" || updateType !== "";

  const learnRepositories = useCallback((items: UpdatePr[]): void => {
    setRepositories((known) => {
      const missing = items
        .map((pr) => pr.repository)
        .filter((repo) => !known.some((k) => k.id === repo.id));
      if (missing.length === 0) {
        return known;
      }
      const next = [...known];
      for (const repo of missing) {
        if (!next.some((k) => k.id === repo.id)) {
          next.push(repo);
        }
      }
      return next.sort((a, b) => a.fullName.localeCompare(b.fullName));
    });
  }, []);

  const nothingYet = !filtered && openTotal === 0 && mergedTotal === 0;

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <PageHeader description={t("description")} title={t("title")} />
      <UpdatePrFilters
        onRepositoryChange={(id) => {
          setRepositoryId(id);
          setPage(1);
        }}
        onUpdateTypeChange={(type) => {
          setUpdateType(type);
          setPage(1);
        }}
        repositories={repositories}
        repositoryId={repositoryId}
        updateType={updateType}
      />
      {nothingYet ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LayoutDashboardIcon aria-hidden="true" />
            </EmptyMedia>
            <EmptyTitle>{t("emptyTitle")}</EmptyTitle>
            <EmptyDescription>{t("emptyDescription")}</EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button render={<Link href="/repositories" />}>
              {t("emptyAction")}
            </Button>
          </EmptyContent>
        </Empty>
      ) : null}
      <div className={nothingYet ? "hidden" : "flex flex-col gap-8"}>
        <ListSection
          emptyText={filtered ? t("open.emptyFiltered") : t("open.empty")}
          onItems={learnRepositories}
          onTotal={setOpenTotal}
          paginated={{
            onPageChange: setPage,
            onPageSizeChange: (size) => {
              setPageSize(size);
              setPage(1);
            },
          }}
          params={{
            state: "open",
            sort: "-openedAt",
            page,
            pageSize,
            ...filters,
          }}
          timeColumn="age"
          title={t("open.title")}
        />
        <ListSection
          emptyText={filtered ? t("merged.emptyFiltered") : t("merged.empty")}
          onItems={learnRepositories}
          onTotal={setMergedTotal}
          params={{
            state: "merged",
            sort: "-mergedAt",
            page: 1,
            pageSize: MERGED_LIMIT,
            ...filters,
          }}
          timeColumn="mergedAt"
          title={t("merged.title")}
        />
      </div>
    </div>
  );
}
