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
import { ApiError } from "@/lib/api/client";
import { useEnableRenovate } from "@/lib/query/repositories";
import type { EnableRenovateResult, Repository } from "@/lib/query/types";

/** The branch "Enable repo" opens its pull request from (internal/github EnableRenovateBranch). */
const ENABLE_BRANCH = "release-ops/enable-renovate";

type EnableRepoDialogProps = {
  repository: Repository;
  /** The preset the opened config extends, or null while it could not be read. */
  preset: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the pull request once it is open, new or one an earlier call left open. */
  onOpened: (result: EnableRenovateResult) => void;
};

/** Confirms and runs "Enable repo": a pull request that adds renovate.json (docs/specs.html §7.5). */
export function EnableRepoDialog({
  repository,
  preset,
  open,
  onOpenChange,
  onOpened,
}: EnableRepoDialogProps): React.ReactElement {
  const t = useTranslations("repositories");
  const tCommon = useTranslations("common");
  const enable = useEnableRenovate();
  const [error, setError] = useState<string | null>(null);
  // Each attempt gets a number; a result that settles after the dialog was closed or another
  // attempt started is not applied.
  const attempt = useRef(0);

  function handleOpenChange(next: boolean): void {
    if (!next) {
      attempt.current += 1;
      setError(null);
      enable.reset();
    }
    onOpenChange(next);
  }

  async function handleConfirm(): Promise<void> {
    const mine = ++attempt.current;
    setError(null);
    try {
      const result = await enable.mutateAsync(repository.id);
      if (mine !== attempt.current) {
        return;
      }
      onOpened(result);
    } catch (caught) {
      if (mine !== attempt.current) {
        return;
      }
      if (
        caught instanceof ApiError &&
        caught.code === "ENABLE_BRANCH_EXISTS"
      ) {
        setError(t("enableRepoBranchExists", { branch: ENABLE_BRANCH }));
        return;
      }
      setError(
        t("enableRepoFailed", {
          message:
            caught instanceof ApiError
              ? caught.message
              : t("actionFailedUnknown"),
        }),
      );
    }
  }

  return (
    <AlertDialog onOpenChange={handleOpenChange} open={open}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("enableRepoTitle")}</AlertDialogTitle>
          <AlertDialogDescription>
            {preset
              ? t("enableRepoDescription", {
                  repository: repository.fullName,
                  preset,
                })
              : t("enableRepoPresetUnavailable")}
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
            disabled={preset === null}
            loading={enable.isPending}
            onClick={() => {
              void handleConfirm();
            }}
          >
            {t("enableRepoConfirm")}
          </Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  );
}
