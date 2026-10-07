"use client";

import { CopyIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { useId, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api/client";
import { useInvitations } from "@/hooks/useInvitations";

type InviteDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCopyLink: (url: string) => Promise<void>;
};

export function InviteDialog({
  open,
  onOpenChange,
  onCopyLink,
}: InviteDialogProps): React.ReactElement {
  const t = useTranslations("users");
  const tCommon = useTranslations("common");
  const emailId = useId();
  const { createInvitation } = useInvitations();

  const [email, setEmail] = useState("");
  const [validationError, setValidationError] = useState<string | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [inviteUrl, setInviteUrl] = useState<string | null>(null);

  function resetState(): void {
    setEmail("");
    setValidationError(null);
    setSubmitError(null);
    setInviteUrl(null);
  }

  function handleOpenChange(nextOpen: boolean): void {
    if (!nextOpen) {
      resetState();
    }
    onOpenChange(nextOpen);
  }

  async function handleSubmit(): Promise<void> {
    setSubmitError(null);
    setValidationError(null);

    if (!email.trim()) {
      setValidationError(t("validation.emailRequired"));
      return;
    }

    try {
      const result = await createInvitation.mutateAsync({
        email: email.trim(),
      });
      setInviteUrl(result.inviteUrl);
    } catch (error) {
      if (error instanceof ApiError) {
        setSubmitError(error.message);
      } else {
        setSubmitError(t("inviteFailed"));
      }
    }
  }

  return (
    <Dialog onOpenChange={handleOpenChange} open={open}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>{t("inviteTitle")}</DialogTitle>
          <DialogDescription>{t("inviteDescription")}</DialogDescription>
        </DialogHeader>
        <DialogPanel>
          {inviteUrl ? (
            <div className="flex flex-col gap-4">
              <p className="text-muted-foreground text-sm">
                {t("inviteCreatedDescription")}
              </p>
              <div className="flex gap-2">
                <Input readOnly value={inviteUrl} />
                <Button
                  aria-label={t("copyInviteLink")}
                  onClick={() => {
                    void onCopyLink(inviteUrl);
                  }}
                  size="icon"
                  variant="outline"
                >
                  <CopyIcon aria-hidden="true" />
                </Button>
              </div>
            </div>
          ) : (
            <Field>
              <FieldLabel htmlFor={emailId}>{t("inviteEmail")}</FieldLabel>
              <Input
                autoComplete="email"
                id={emailId}
                onChange={(event) => {
                  setEmail(event.target.value);
                  setValidationError(null);
                  setSubmitError(null);
                }}
                type="email"
                value={email}
              />
              {validationError ? (
                <FieldError>{validationError}</FieldError>
              ) : null}
              {submitError ? <FieldError>{submitError}</FieldError> : null}
            </Field>
          )}
        </DialogPanel>
        <DialogFooter>
          {inviteUrl ? (
            <Button onClick={() => handleOpenChange(false)}>
              {tCommon("cancel")}
            </Button>
          ) : (
            <>
              <Button onClick={() => handleOpenChange(false)} variant="ghost">
                {tCommon("cancel")}
              </Button>
              <Button
                loading={createInvitation.isPending}
                onClick={() => {
                  void handleSubmit();
                }}
              >
                {t("inviteSubmit")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}
