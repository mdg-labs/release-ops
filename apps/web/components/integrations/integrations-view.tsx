"use client";

import { PencilIcon, PlugZapIcon, Trash2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { PageHeader } from "@/components/common/page-header";
import { DeleteIntegrationDialog } from "@/components/integrations/delete-integration-dialog";
import { IntegrationDrawer } from "@/components/integrations/integration-drawer";
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
import { toastManager } from "@/components/ui/toast";
import { useIntegrations } from "@/lib/hooks/use-integrations";
import type { Integration } from "@/lib/query/types";

type DrawerState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; integration: Integration };

export function IntegrationsView(): React.ReactElement {
  const t = useTranslations("integrations");
  const {
    data: integrations,
    isLoading,
    isError,
    createIntegration,
    updateIntegration,
    deleteIntegration,
    testIntegration,
  } = useIntegrations();

  const [drawer, setDrawer] = useState<DrawerState>({ mode: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<Integration | null>(null);

  const drawerOpen = drawer.mode !== "closed";
  const isEmpty = !isLoading && integrations?.length === 0;

  function openCreateDrawer(): void {
    setDrawer({ mode: "create" });
  }

  function runTestConnection(integration: Integration) {
    toastManager.promise(
      (async () => {
        const result = await testIntegration.mutateAsync(integration.id);
        if (!result.success) {
          throw new Error(
            result.message ?? t("testConnectionFailedDescription"),
          );
        }
        return result;
      })(),
      {
        loading: {
          title: t("testConnectionLoading"),
          description: t("testConnectionLoadingDescription"),
        },
        success: (result) => ({
          title: t("testConnectionSuccess"),
          description: result.message ?? t("testConnectionSuccessDescription"),
        }),
        error: (error: Error) => ({
          title: t("testConnectionFailed"),
          description: error.message || t("testConnectionFailedDescription"),
        }),
      },
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        action={<Button onClick={openCreateDrawer}>{t("add")}</Button>}
        title={t("title")}
      />

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      <Frame>
        <FramePanel className="p-0">
          {isLoading ? (
            <Table variant="card">
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.name")}</TableHead>
                  <TableHead>{t("columns.kind")}</TableHead>
                  <TableHead>{t("columns.baseUrl")}</TableHead>
                  <TableHead>{t("columns.secret")}</TableHead>
                  <TableHead className="text-end">
                    {t("columns.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {Array.from({ length: 3 }, (_, index) => (
                  <TableRow key={`skeleton-${index}`}>
                    <TableCell colSpan={5}>
                      <Skeleton className="h-8 w-full" />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          ) : isEmpty ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <PlugZapIcon aria-hidden="true" />
                </EmptyMedia>
                <EmptyTitle>{t("emptyTitle")}</EmptyTitle>
                <EmptyDescription>{t("emptyDescription")}</EmptyDescription>
              </EmptyHeader>
              <EmptyContent>
                <Button onClick={openCreateDrawer}>{t("add")}</Button>
              </EmptyContent>
            </Empty>
          ) : (
            <Table variant="card">
              <TableHeader>
                <TableRow>
                  <TableHead>{t("columns.name")}</TableHead>
                  <TableHead>{t("columns.kind")}</TableHead>
                  <TableHead>{t("columns.baseUrl")}</TableHead>
                  <TableHead>{t("columns.secret")}</TableHead>
                  <TableHead className="text-end">
                    {t("columns.actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {integrations?.map((integration) => (
                  <TableRow key={integration.id}>
                    <TableCell className="font-medium">
                      {integration.name}
                    </TableCell>
                    <TableCell>
                      <Badge variant="outline">
                        {t(`kinds.${integration.kind}`)}
                      </Badge>
                    </TableCell>
                    <TableCell className="max-w-48 truncate">
                      {integration.baseUrl ?? t("baseUrlDefault")}
                    </TableCell>
                    <TableCell>
                      {integration.hasSecret ? (
                        <Badge variant="success">{t("secretConfigured")}</Badge>
                      ) : (
                        <Badge variant="warning">{t("secretMissing")}</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <Button
                          aria-label={t("editAria", {
                            name: integration.name,
                          })}
                          onClick={() =>
                            setDrawer({ mode: "edit", integration })
                          }
                          size="icon-sm"
                          variant="ghost"
                        >
                          <PencilIcon />
                        </Button>
                        <Button
                          aria-label={t("testConnectionAria", {
                            name: integration.name,
                          })}
                          onClick={() => runTestConnection(integration)}
                          size="icon-sm"
                          variant="ghost"
                        >
                          <PlugZapIcon />
                        </Button>
                        <Button
                          aria-label={t("deleteAria", {
                            name: integration.name,
                          })}
                          onClick={() => setDeleteTarget(integration)}
                          size="icon-sm"
                          variant="ghost"
                        >
                          <Trash2Icon />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </FramePanel>
      </Frame>

      <IntegrationDrawer
        integration={drawer.mode === "edit" ? drawer.integration : null}
        isSaving={createIntegration.isPending || updateIntegration.isPending}
        mode={drawer.mode === "edit" ? "edit" : "create"}
        onCreate={(input) => createIntegration.mutateAsync(input)}
        onOpenChange={(open) => {
          if (!open) {
            setDrawer({ mode: "closed" });
          }
        }}
        onTest={(id) => testIntegration.mutateAsync(id)}
        onUpdate={(id, input) => updateIntegration.mutateAsync({ id, input })}
        open={drawerOpen}
      />

      <DeleteIntegrationDialog
        integration={deleteTarget}
        isDeleting={deleteIntegration.isPending}
        onConfirm={async () => {
          if (!deleteTarget) {
            return;
          }
          await deleteIntegration.mutateAsync(deleteTarget.id);
          setDeleteTarget(null);
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        open={deleteTarget !== null}
      />
    </div>
  );
}
