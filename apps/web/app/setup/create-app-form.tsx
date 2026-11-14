"use client";

import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import { AUTH_FORM_CLASS, AuthErrorAlert } from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ApiError } from "@/lib/api/client";
import { useCreateSetupManifest } from "@/lib/hooks/use-setup";
import type { SetupOwnerType } from "@/lib/query/types";
import { submitManifestForm } from "@/lib/setup/submit-manifest";

type SetupT = ReturnType<typeof useTranslations<"setup">>;

type OwnerItem = { value: SetupOwnerType; label: string };

function resolveManifestError(error: unknown, t: SetupT): string {
  if (error instanceof ApiError) {
    if (error.status === 429 || error.code === "RATE_LIMITED") {
      return t("rateLimited");
    }
    if (error.status === 401) {
      return t("sessionExpired");
    }
    if (error.status === 400) {
      return t("invalidOwnerOrName");
    }
  }
  return t("requestFailed");
}

function resolveCallbackError(code: string, t: SetupT): string {
  switch (code) {
    case "invalid_state":
      return t("callbackInvalidState");
    case "conversion_rejected":
      return t("callbackRejected");
    case "conversion_failed":
      return t("callbackFailed");
    default:
      return t("requestFailed");
  }
}

type CreateAppFormProps = {
  /** The error code GitHub's callback redirected back to /setup with, if any. */
  callbackError?: string | null;
  /** Called when the setup session is gone (401) or setup is over (404). */
  onSessionLost: () => void;
};

/** Chooses the App's owner and name, then hands the manifest to GitHub. */
export function CreateAppForm({
  callbackError = null,
  onSessionLost,
}: CreateAppFormProps): React.ReactElement {
  const t = useTranslations("setup");
  const create = useCreateSetupManifest();
  const alertRef = useRef<HTMLDivElement>(null);
  const ownerId = useId();
  const loginId = useId();
  const nameId = useId();
  const [owner, setOwner] = useState<SetupOwnerType>("user");
  const [error, setError] = useState<string | null>(
    callbackError ? resolveCallbackError(callbackError, t) : null,
  );
  const [handedOff, setHandedOff] = useState(false);

  const ownerItems: OwnerItem[] = [
    { value: "user", label: t("ownerUser") },
    { value: "organization", label: t("ownerOrganization") },
  ];
  const selectedOwner = ownerItems.find((item) => item.value === owner);

  useEffect(() => {
    if (error) {
      alertRef.current?.focus();
    }
  }, [error]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    const formData = new FormData(event.currentTarget);
    const login = String(formData.get("login") ?? "").trim();
    const name = String(formData.get("name") ?? "").trim();

    try {
      const result = await create.mutateAsync({
        owner:
          owner === "organization" ? { type: owner, login } : { type: owner },
        ...(name ? { name } : {}),
      });
      setHandedOff(true);
      submitManifestForm(result.url, result.manifest);
    } catch (caught) {
      if (
        caught instanceof ApiError &&
        (caught.status === 401 || caught.status === 404)
      ) {
        onSessionLost();
        return;
      }
      setError(resolveManifestError(caught, t));
    }
  }

  const busy = create.isPending || handedOff;

  return (
    <>
      {error ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={error}
          title={t("createFailedTitle")}
        />
      ) : null}
      <form className={AUTH_FORM_CLASS} onSubmit={handleSubmit}>
        <Field name="owner">
          <FieldLabel htmlFor={ownerId}>{t("ownerLabel")}</FieldLabel>
          <Select
            disabled={busy}
            itemToStringValue={(item) => item.value}
            items={ownerItems}
            onValueChange={(next) => {
              if (next) {
                setOwner(next.value);
              }
            }}
            value={selectedOwner}
          >
            <SelectTrigger id={ownerId}>
              <SelectValue />
            </SelectTrigger>
            <SelectPopup>
              {ownerItems.map((item) => (
                <SelectItem key={item.value} value={item}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectPopup>
          </Select>
        </Field>
        {owner === "organization" ? (
          <Field name="login">
            <FieldLabel htmlFor={loginId}>
              {t("organizationLabel")} <span aria-hidden="true">*</span>
            </FieldLabel>
            <Input
              autoComplete="off"
              disabled={busy}
              id={loginId}
              maxLength={39}
              name="login"
              required
              spellCheck={false}
              type="text"
            />
          </Field>
        ) : null}
        <Field name="name">
          <FieldLabel htmlFor={nameId}>{t("nameLabel")}</FieldLabel>
          <Input
            autoComplete="off"
            disabled={busy}
            id={nameId}
            maxLength={34}
            name="name"
            placeholder={t("namePlaceholder")}
            type="text"
          />
          <FieldDescription>{t("nameDescription")}</FieldDescription>
        </Field>
        <Button loading={busy} type="submit">
          {t("createApp")}
        </Button>
      </form>
    </>
  );
}
