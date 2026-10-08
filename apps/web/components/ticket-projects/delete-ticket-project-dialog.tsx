"use client";

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
import type { TicketProject } from "@/lib/query/types";

type DeleteTicketProjectDialogProps = {
  project: TicketProject | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  error: string | null;
  isDeleting: boolean;
};

export function DeleteTicketProjectDialog({
  project,
  open,
  onOpenChange,
  onConfirm,
  error,
  isDeleting,
}: DeleteTicketProjectDialogProps): React.ReactElement {
  const t = useTranslations("ticket-projects");
  const tCommon = useTranslations("common");

  return (
    <AlertDialog onOpenChange={onOpenChange} open={open}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("deleteTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("deleteDescription", { name: project?.name ?? "" })}
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
            loading={isDeleting}
            onClick={onConfirm}
            variant="destructive"
          >
            {tCommon("delete")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
