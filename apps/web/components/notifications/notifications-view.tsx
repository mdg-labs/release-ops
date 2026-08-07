"use client";

import { BellRingIcon, PencilIcon, Trash2Icon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useState } from "react";
import { DeleteNotificationDialog } from "@/components/notifications/delete-notification-dialog";
import { NotificationDrawer } from "@/components/notifications/notification-drawer";
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
import { toastManager } from "@/components/ui/toast";
import { useNotificationTargets } from "@/lib/hooks/use-notifications";
import type { NotificationTarget } from "@/lib/query/types";

type DrawerState =
  | { mode: "closed" }
  | { mode: "create" }
  | { mode: "edit"; target: NotificationTarget };

export function NotificationsView(): React.ReactElement {
  const t = useTranslations("notifications");
  const {
    data: targets,
    isLoading,
    isError,
    createNotificationTarget,
    updateNotificationTarget,
    deleteNotificationTarget,
    testNotificationTarget,
  } = useNotificationTargets();

  const [drawer, setDrawer] = useState<DrawerState>({ mode: "closed" });
  const [deleteTarget, setDeleteTarget] = useState<NotificationTarget | null>(
    null,
  );

  const drawerOpen = drawer.mode !== "closed";

  function runTestNotification(target: NotificationTarget) {
    toastManager.promise(
      (async () => {
        const result = await testNotificationTarget.mutateAsync(target.id);
        if (!result.success) {
          throw new Error(result.message ?? t("testFailedDescription"));
        }
        return result;
      })(),
      {
        loading: {
          title: t("testLoading"),
          description: t("testLoadingDescription"),
        },
        success: (result) => ({
          title: t("testSuccess"),
          description: result.message ?? t("testSuccessDescription"),
        }),
        error: (error: Error) => ({
          title: t("testFailed"),
          description: error.message || t("testFailedDescription"),
        }),
      },
    );
  }

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
                <TableHead>{t("columns.name")}</TableHead>
                <TableHead>{t("columns.events")}</TableHead>
                <TableHead>{t("columns.enabled")}</TableHead>
                <TableHead>{t("columns.url")}</TableHead>
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

              {!isLoading && targets?.length === 0 ? (
                <TableRow>
                  <TableCell className="text-muted-foreground" colSpan={5}>
                    {t("empty")}
                  </TableCell>
                </TableRow>
              ) : null}

              {!isLoading
                ? targets?.map((target) => (
                    <TableRow key={target.id}>
                      <TableCell className="font-medium">
                        {target.name}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {target.events.map((event) => (
                            <Badge key={event} variant="outline">
                              {t(`eventOptions.${event}`)}
                            </Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell>
                        {target.enabled ? (
                          <Badge variant="success">{t("enabledOn")}</Badge>
                        ) : (
                          <Badge variant="warning">{t("enabledOff")}</Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {target.hasSecret ? (
                          <Badge variant="success">{t("urlConfigured")}</Badge>
                        ) : (
                          <Badge variant="warning">{t("urlMissing")}</Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            aria-label={t("editAria", { name: target.name })}
                            onClick={() => setDrawer({ mode: "edit", target })}
                            size="icon-sm"
                            variant="ghost"
                          >
                            <PencilIcon />
                          </Button>
                          <Button
                            aria-label={t("testAria", { name: target.name })}
                            onClick={() => runTestNotification(target)}
                            size="icon-sm"
                            variant="ghost"
                          >
                            <BellRingIcon />
                          </Button>
                          <Button
                            aria-label={t("deleteAria", { name: target.name })}
                            onClick={() => setDeleteTarget(target)}
                            size="icon-sm"
                            variant="ghost"
                          >
                            <Trash2Icon />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                : null}
            </TableBody>
          </Table>
        </FramePanel>
      </Frame>

      <NotificationDrawer
        isSaving={
          createNotificationTarget.isPending ||
          updateNotificationTarget.isPending
        }
        mode={drawer.mode === "edit" ? "edit" : "create"}
        onCreate={(input) => createNotificationTarget.mutateAsync(input)}
        onOpenChange={(open) => {
          if (!open) {
            setDrawer({ mode: "closed" });
          }
        }}
        onTest={(id) => testNotificationTarget.mutateAsync(id)}
        onUpdate={(id, input) =>
          updateNotificationTarget.mutateAsync({ id, input })
        }
        open={drawerOpen}
        target={drawer.mode === "edit" ? drawer.target : null}
      />

      <DeleteNotificationDialog
        isDeleting={deleteNotificationTarget.isPending}
        onConfirm={async () => {
          if (!deleteTarget) {
            return;
          }
          await deleteNotificationTarget.mutateAsync(deleteTarget.id);
          setDeleteTarget(null);
        }}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        open={deleteTarget !== null}
        target={deleteTarget}
      />
    </div>
  );
}
