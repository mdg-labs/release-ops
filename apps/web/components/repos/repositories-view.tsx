"use client";

import { FolderGit2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useEffect, useState } from "react";
import { PageHeader } from "@/components/common/page-header";
import {
  type AdminTools,
  RepositoryTable,
} from "@/components/repos/repository-table";
import { enableRepoPreset } from "@/components/repos/preset";
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
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useGitHubApp } from "@/lib/hooks/use-github-app";
import {
  liveInstallation,
  useInstallations,
} from "@/lib/hooks/use-installations";
import { usePersistedPageSize } from "@/lib/hooks/use-persisted-page-size";
import { useSessionRole } from "@/lib/hooks/use-session";
import { useSettings } from "@/lib/hooks/use-settings";
import { useTicketProjects } from "@/lib/hooks/use-ticket-projects";
import { PAGE_SIZE_OPTIONS } from "@/lib/pagination/constants";
import { TablePaginationControls } from "@/lib/pagination/table-pagination-controls";
import { useClampPage } from "@/lib/pagination/use-clamp-page";
import { useRepositories } from "@/lib/query/repositories";

const SEARCH_DEBOUNCE_MS = 250;

/** The admin-only reads: ticket projects, the installation default and the preset. */
function useAdminTools(): AdminTools {
  const projects = useTicketProjects();
  const installations = useInstallations();
  const settings = useSettings();

  const projectList = projects.isError ? null : (projects.data ?? null);
  const defaultId =
    installations.isError || projectList === null
      ? null
      : (liveInstallation(installations.data)?.defaultTicketProjectId ?? null);
  return {
    projects: projectList,
    projectsLoading: projects.isLoading,
    defaultProjectName:
      projectList?.find((project) => project.id === defaultId)?.name ?? null,
    preset:
      settings.isError || !settings.data
        ? null
        : enableRepoPreset(settings.data.presetRepositories),
  };
}

function AdminRepositoryTable({
  items,
}: {
  items: React.ComponentProps<typeof RepositoryTable>["items"];
}): React.ReactElement {
  const tools = useAdminTools();
  return <RepositoryTable items={items} tools={tools} />;
}

function InstallLink(): React.ReactElement | null {
  const t = useTranslations("repositories");
  const app = useGitHubApp();
  const htmlUrl = app.isError ? null : (app.data?.app.htmlUrl ?? null);

  if (!htmlUrl) {
    return null;
  }
  return (
    <Button
      render={
        <a
          href={`${htmlUrl}/installations/new`}
          rel="noopener noreferrer"
          target="_blank"
        />
      }
    >
      {t("emptyAction")}
    </Button>
  );
}

export function RepositoriesView(): React.ReactElement {
  const t = useTranslations("repositories");
  const { role } = useSessionRole();
  const isAdmin = role === "admin";
  const [searchInput, setSearchInput] = useState("");
  const [search, setSearch] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = usePersistedPageSize(
    "repositories-page-size",
  );

  useEffect(() => {
    const next = searchInput.trim();
    if (next === search) {
      return;
    }
    const timer = setTimeout(() => {
      setSearch(next);
      setPage(1);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [searchInput, search]);

  const query = useRepositories({ page, pageSize, search });
  const { data, isPending, isError, isPlaceholderData, refetch } = query;

  const items = data?.items ?? [];
  const total = data?.total ?? 0;
  useClampPage({
    page,
    pageSize,
    result:
      data && !isPlaceholderData ? { itemCount: items.length, total } : null,
    onPageChange: setPage,
  });
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  const nothingInstalled = !isError && data && total === 0 && search === "";

  let body: React.ReactElement | null = null;
  if (isError) {
    body = (
      <Alert variant="error">
        <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
          <span>{t("loadFailed")}</span>
          <Button onClick={() => void refetch()} size="sm" variant="outline">
            {t("retry")}
          </Button>
        </AlertDescription>
      </Alert>
    );
  } else if (isPending) {
    body = (
      <Frame>
        <FramePanel className="space-y-2 p-4">
          {Array.from({ length: 3 }, (_, index) => (
            <Skeleton
              className="h-8 w-full"
              data-testid="repositories-skeleton"
              key={`skeleton-${index}`}
            />
          ))}
        </FramePanel>
      </Frame>
    );
  } else if (nothingInstalled) {
    body = (
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FolderGit2Icon aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{t("emptyTitle")}</EmptyTitle>
          <EmptyDescription>
            {isAdmin ? t("emptyDescription") : t("emptyDescriptionViewer")}
          </EmptyDescription>
        </EmptyHeader>
        {isAdmin ? (
          <EmptyContent>
            <InstallLink />
          </EmptyContent>
        ) : null}
      </Empty>
    );
  } else if (data && items.length === 0) {
    body = <p className="text-muted-foreground text-sm">{t("emptySearch")}</p>;
  } else if (data) {
    body = (
      <>
        <Frame>
          <FramePanel
            aria-busy={isPlaceholderData}
            className={isPlaceholderData ? "p-0 opacity-60" : "p-0"}
          >
            {isAdmin ? (
              <AdminRepositoryTable items={items} />
            ) : (
              <RepositoryTable items={items} tools={null} />
            )}
          </FramePanel>
        </Frame>
        <TablePaginationControls
          canNextPage={page * pageSize < total}
          canPreviousPage={page > 1}
          nextLabel={t("pagination.next")}
          onNextPage={() => setPage(page + 1)}
          onPageSizeChange={(size) => {
            setPageSize(size);
            setPage(1);
          }}
          onPreviousPage={() => setPage(page - 1)}
          pageSize={pageSize}
          pageSizeItems={PAGE_SIZE_OPTIONS.map((size) => ({
            label: t("pagination.pageSizeOption", { count: size }),
            value: size,
          }))}
          pageSizeLabel={t("pagination.pageSize")}
          previousLabel={t("pagination.previous")}
          summary={t("pagination.summary", { from, to, total })}
        />
      </>
    );
  }

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <PageHeader description={t("description")} title={t("title")} />
      {nothingInstalled ? null : (
        <Input
          aria-label={t("searchLabel")}
          className="max-w-sm"
          onChange={(event) => setSearchInput(event.target.value)}
          placeholder={t("searchPlaceholder")}
          type="search"
          value={searchInput}
        />
      )}
      {body}
    </div>
  );
}
