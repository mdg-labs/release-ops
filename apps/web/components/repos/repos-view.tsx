"use client";

import { PencilIcon, Trash2Icon } from "lucide-react";
import { useFormatter, useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { DeleteRepoDialog } from "@/components/repos/delete-repo-dialog";
import { RepoDialog } from "@/components/repos/repo-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Frame, FramePanel } from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useIntegrations } from "@/lib/hooks/use-integrations";
import { useNotificationTargets } from "@/lib/hooks/use-notifications";
import { useRepos } from "@/lib/hooks/use-repos";
import { useTicketProjects } from "@/lib/hooks/use-ticket-projects";
import type { Repo } from "@/lib/query/types";

type DialogState =
  { mode: "closed" } | { mode: "create" } | { mode: "edit"; repo: Repo };

export function ReposView(): React.ReactElement {
  const t = useTranslations("repos");
  const tIntegrations = useTranslations("integrations");
  const format = useFormatter();

  const {
    data: repos,
    isLoading,
    isError,
    createRepo,
    updateRepo,
    deleteRepo,
  } = useRepos();
  const { data: ticketProjects = [] } = useTicketProjects();
  const { data: integrations = [] } = useIntegrations();
  const { data: notificationTargets = [] } = useNotificationTargets();

  const [dialog, setDialog] = useState<DialogState>({ mode: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<Repo | null>(null);

  const ticketProjectNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const project of ticketProjects) {
      map.set(project.id, project.name);
    }
    return map;
  }, [ticketProjects]);

  const integrationNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const integration of integrations) {
      map.set(integration.id, integration.name);
    }
    return map;
  }, [integrations]);

  const sortedRepos = useMemo(() => {
    if (!repos) {
      return [];
    }

    return [...repos].sort((left, right) =>
      left.projectPath.localeCompare(right.projectPath),
    );
  }, [repos]);

  const dialogOpen = dialog.mode !== "closed";

  function formatLastPolledAt(value: string | null): string {
    if (!value) {
      return t("lastPolledNever");
    }

    return format.dateTime(new Date(value), {
      dateStyle: "medium",
      timeStyle: "short",
    });
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="font-semibold text-2xl">{t("title")}</h1>
        <Button onClick={() => setDialog({ mode: "create" })}>
          {t("add")}
        </Button>
      </div>

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      <Frame>
        <FramePanel className="p-0">
          <Table variant="card">
            <TableHeader>
              <TableRow>
                <TableHead>{t("columns.projectPath")}</TableHead>
                <TableHead>{t("columns.sourceKind")}</TableHead>
                <TableHead>{t("columns.ticketProject")}</TableHead>
                <TableHead>{t("columns.enabled")}</TableHead>
                <TableHead>{t("columns.lastPolledAt")}</TableHead>
                <TableHead>{t("columns.lastError")}</TableHead>
                <TableHead className="text-end">
                  {t("columns.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading
                ? Array.from({ length: 3 }, (_, index) => (
                    <TableRow key={`skeleton-${index}`}>
                      <TableCell colSpan={7}>
                        <Skeleton className="h-8 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                : null}

              {!isLoading && sortedRepos.length === 0 ? (
                <TableRow>
                  <TableCell className="text-muted-foreground" colSpan={7}>
                    {t("empty")}
                  </TableCell>
                </TableRow>
              ) : null}

              {!isLoading
                ? sortedRepos.map((repo) => {
                    const ticketProjectName =
                      ticketProjectNameById.get(repo.ticketProjectId) ??
                      repo.ticketProjectId;
                    const integrationName = repo.sourceIntegrationId
                      ? (integrationNameById.get(repo.sourceIntegrationId) ??
                        repo.sourceIntegrationId)
                      : null;

                    return (
                      <TableRow key={repo.id}>
                        <TableCell className="font-medium font-mono text-sm">
                          {repo.projectPath}
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <Badge variant="outline">
                              {tIntegrations(`kinds.${repo.sourceKind}`)}
                            </Badge>
                            {integrationName ? (
                              <span className="text-muted-foreground text-xs">
                                {integrationName}
                              </span>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>{ticketProjectName}</TableCell>
                        <TableCell>
                          <Badge
                            variant={repo.enabled ? "success" : "secondary"}
                          >
                            {repo.enabled ? t("enabledOn") : t("enabledOff")}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-muted-foreground text-sm">
                          {formatLastPolledAt(repo.lastPolledAt)}
                        </TableCell>
                        <TableCell className="max-w-48 truncate text-sm">
                          {repo.lastError ? (
                            <span
                              className="text-destructive-foreground"
                              title={repo.lastError}
                            >
                              {repo.lastError}
                            </span>
                          ) : (
                            <span className="text-muted-foreground">
                              {t("noError")}
                            </span>
                          )}
                        </TableCell>
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            <Button
                              aria-label={t("editAria", {
                                path: repo.projectPath,
                              })}
                              onClick={() => setDialog({ mode: "edit", repo })}
                              size="icon-sm"
                              variant="ghost"
                            >
                              <PencilIcon />
                            </Button>
                            <Button
                              aria-label={t("deleteAria", {
                                path: repo.projectPath,
                              })}
                              onClick={() => setDeleteTarget(repo)}
                              size="icon-sm"
                              variant="ghost"
                            >
                              <Trash2Icon />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    );
                  })
                : null}
            </TableBody>
          </Table>
        </FramePanel>
      </Frame>

      <RepoDialog
        integrations={integrations}
        isSaving={createRepo.isPending || updateRepo.isPending}
        mode={dialog.mode === "edit" ? "edit" : "create"}
        notificationTargets={notificationTargets}
        onCreate={(input) => createRepo.mutateAsync(input)}
        onOpenChange={(open) => {
          if (!open) {
            setDialog({ mode: "closed" });
          }
        }}
        onUpdate={(id, input) => updateRepo.mutateAsync({ id, input })}
        open={dialogOpen}
        repo={dialog.mode === "edit" ? dialog.repo : null}
        ticketProjects={ticketProjects}
      />

      <DeleteRepoDialog
        isDeleting={deleteRepo.isPending}
        onConfirm={async () => {
          if (!deleteTarget) {
            return;
          }
          await deleteRepo.mutateAsync(deleteTarget.id);
          setDeleteTarget(null);
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        open={deleteTarget !== null}
        repo={deleteTarget}
      />
    </div>
  );
}
