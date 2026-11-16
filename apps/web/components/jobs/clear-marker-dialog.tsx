"use client";

import { useRef, useState } from "react";
import { useTranslations } from "next-intl";
import {
  AlertDialog,
  AlertDialogClose,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogPopup,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { toastManager } from "@/components/ui/toast";
import { ApiError } from "@/lib/api/client";
import { useClearPendingMarker } from "@/lib/query/jobs";

type ClearMarkerDialogProps = {
  /** The update ticket whose marker is to be cleared; the dialog is closed while null. */
  updateTicketId: string | null;
  repository: string;
  onClose: () => void;
};

/**
 * Confirms the admin's clear action for a stuck pending marker (docs/specs.html §5.2.2, §7.6).
 * The confirmation names the line to look for in the ticket tracker first: clearing a marker for
 * a ticket that does exist makes a duplicate.
 */
export function ClearMarkerDialog({
  updateTicketId,
  repository,
  onClose,
}: ClearMarkerDialogProps): React.ReactElement {
  const t = useTranslations("jobs.problems");
  const tCommon = useTranslations("common");
  const clear = useClearPendingMarker();
  const [error, setError] = useState<string | null>(null);
  // Each attempt gets a number; a result that settles after the dialog was closed or another
  // attempt started is not applied.
  const attempt = useRef(0);

  function close(): void {
    attempt.current += 1;
    setError(null);
    clear.reset();
    onClose();
  }

  async function confirm(): Promise<void> {
    if (updateTicketId === null) {
      return;
    }
    const mine = ++attempt.current;
    setError(null);
    try {
      await clear.mutateAsync(updateTicketId);
      if (mine !== attempt.current) {
        return;
      }
      toastManager.add({
        title: t("clearDone", { repository }),
        type: "success",
      });
      close();
    } catch (caught) {
      if (mine !== attempt.current) {
        return;
      }
      setError(
        t("clearFailed", {
          message:
            caught instanceof ApiError ? caught.message : t("clearUnknown"),
        }),
      );
    }
  }

  return (
    <AlertDialog
      onOpenChange={(open) => {
        if (!open) {
          close();
        }
      }}
      open={updateTicketId !== null}
    >
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("clearTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("clearDescription", { key: updateTicketId ?? "" })}
          </AlertDialogDescription>
          {error ? (
            <p className="text-destructive-foreground text-sm" role="alert">
              {error}
            </p>
          ) : null}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="ghost" />}>
            {tCommon("cancel")}
          </AlertDialogClose>
          <Button
            loading={clear.isPending}
            onClick={() => {
              void confirm();
            }}
            variant="destructive"
          >
            {t("clearConfirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
