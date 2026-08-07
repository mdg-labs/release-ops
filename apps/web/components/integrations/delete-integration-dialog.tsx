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
import type { Integration } from "@/lib/query/types";

type DeleteIntegrationDialogProps = {
  integration: Integration | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  isDeleting: boolean;
};

export function DeleteIntegrationDialog({
  integration,
  open,
  onOpenChange,
  onConfirm,
  isDeleting,
}: DeleteIntegrationDialogProps): React.ReactElement {
  const t = useTranslations("integrations");
  const tCommon = useTranslations("common");

  return (
    <AlertDialog onOpenChange={onOpenChange} open={open}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("deleteTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("deleteDescription", { name: integration?.name ?? "" })}
          </AlertDialogDescription>
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
