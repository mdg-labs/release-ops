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
import type { Invitation } from "@/lib/query/types";

type RevokeInviteDialogProps = {
  invitation: Invitation | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
  isRevoking: boolean;
};

export function RevokeInviteDialog({
  invitation,
  open,
  onOpenChange,
  onConfirm,
  isRevoking,
}: RevokeInviteDialogProps): React.ReactElement {
  const t = useTranslations("users");
  const tCommon = useTranslations("common");

  return (
    <AlertDialog onOpenChange={onOpenChange} open={open}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("revokeTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("revokeDescription", { email: invitation?.email ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="ghost" />}>
            {tCommon("cancel")}
          </AlertDialogClose>
          <Button
            loading={isRevoking}
            onClick={onConfirm}
            variant="destructive"
          >
            {t("revokeConfirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
