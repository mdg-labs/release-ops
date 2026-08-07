"use client";

import { PencilIcon, Trash2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useMemo, useState } from "react";
import { DeleteTicketProjectDialog } from "@/components/ticket-projects/delete-ticket-project-dialog";
import { ProjectDrawer } from "@/components/ticket-projects/project-drawer";
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
import { useTicketProjects } from "@/lib/hooks/use-ticket-projects";
import type { TicketProject } from "@/lib/query/types";

type DrawerState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; project: TicketProject };

export function TicketProjectsView(): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const tIntegrations = useTranslations("integrations");
  const {
    data: projects,
    isLoading,
    isError,
    createTicketProject,
    updateTicketProject,
    deleteTicketProject,
  } = useTicketProjects();
  const { data: integrations = [] } = useIntegrations();

  const [drawer, setDrawer] = useState<DrawerState>({ mode: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<TicketProject | null>(null);

  const integrationNameById = useMemo(() => {
    const map = new Map<string, string>();
    for (const integration of integrations) {
      map.set(integration.id, integration.name);
    }
    return map;
  }, [integrations]);

  const integrationKindById = useMemo(() => {
    const map = new Map<string, string>();
    for (const integration of integrations) {
      map.set(integration.id, integration.kind);
    }
    return map;
  }, [integrations]);

  const sortedProjects = useMemo(() => {
    if (!projects) {
      return [];
    }

    return [...projects].sort((left, right) => {
      const leftIntegration =
        integrationNameById.get(left.integrationId) ?? left.integrationId;
      const rightIntegration =
        integrationNameById.get(right.integrationId) ?? right.integrationId;

      if (leftIntegration !== rightIntegration) {
        return leftIntegration.localeCompare(rightIntegration);
      }

      return left.name.localeCompare(right.name);
    });
  }, [projects, integrationNameById]);

  const drawerOpen = drawer.mode !== "closed";

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="font-semibold text-2xl">{t("title")}</h1>
        <Button onClick={() => setDrawer({ mode: "create" })}>
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
                <TableHead>{t("columns.integration")}</TableHead>
                <TableHead>{t("columns.name")}</TableHead>
                <TableHead>{t("columns.externalProjectId")}</TableHead>
                <TableHead>{t("columns.policy")}</TableHead>
                <TableHead className="text-end">
                  {t("columns.actions")}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading
                ? Array.from({ length: 3 }, (_, index) => (
                    <TableRow key={`skeleton-${index}`}>
                      <TableCell colSpan={5}>
                        <Skeleton className="h-8 w-full" />
                      </TableCell>
                    </TableRow>
                  ))
                : null}

              {!isLoading && sortedProjects.length === 0 ? (
                <TableRow>
                  <TableCell className="text-muted-foreground" colSpan={5}>
                    {t("empty")}
                  </TableCell>
                </TableRow>
              ) : null}

              {!isLoading
                ? sortedProjects.map((project) => {
                    const integrationKind =
                      integrationKindById.get(project.integrationId) ?? "";
                    const integrationName =
                      integrationNameById.get(project.integrationId) ??
                      project.integrationId;

                    return (
                      <TableRow key={project.id}>
                        <TableCell>
                          <div className="flex flex-col gap-1">
                            <span className="font-medium">
                              {integrationName}
                            </span>
                            {integrationKind ? (
                              <Badge variant="outline">
                                {tIntegrations(`kinds.${integrationKind}`)}
                              </Badge>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell className="font-medium">
                          {project.name}
                        </TableCell>
                        <TableCell className="font-mono text-sm">
                          {project.externalProjectId}
                        </TableCell>
                        <TableCell>
                          <Badge variant="secondary">
                            {t(`policies.${project.onOpenTicketPolicy}`)}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <div className="flex justify-end gap-1">
                            <Button
                              aria-label={t("editAria", { name: project.name })}
                              onClick={() =>
                                setDrawer({ mode: "edit", project })
                              }
                              size="icon-sm"
                              variant="ghost"
                            >
                              <PencilIcon />
                            </Button>
                            <Button
                              aria-label={t("deleteAria", {
                                name: project.name,
                              })}
                              onClick={() => setDeleteTarget(project)}
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

      <ProjectDrawer
        integrations={integrations}
        isSaving={
          createTicketProject.isPending || updateTicketProject.isPending
        }
        mode={drawer.mode === "edit" ? "edit" : "create"}
        onCreate={(input) => createTicketProject.mutateAsync(input)}
        onOpenChange={(open) => {
          if (!open) {
            setDrawer({ mode: "closed" });
          }
        }}
        onUpdate={(id, input) => updateTicketProject.mutateAsync({ id, input })}
        open={drawerOpen && drawer.mode !== "closed"}
        project={drawer.mode === "edit" ? drawer.project : null}
      />

      <DeleteTicketProjectDialog
        isDeleting={deleteTicketProject.isPending}
        onConfirm={async () => {
          if (!deleteTarget) {
            return;
          }
          await deleteTicketProject.mutateAsync(deleteTarget.id);
          setDeleteTarget(null);
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        open={deleteTarget !== null}
        project={deleteTarget}
      />
    </div>
  );
}
