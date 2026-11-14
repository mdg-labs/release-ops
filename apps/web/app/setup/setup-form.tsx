"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useFormatter, useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import {
  AUTH_FORM_CLASS,
  AuthErrorAlert,
  AuthLayout,
  AuthSuccessAlert,
} from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/lib/api/client";
import { formatAppDateTime } from "@/lib/format/datetime";
import { CreateAppForm } from "./create-app-form";
import {
  useRedeemSetupToken,
  useSetupSession,
  useSetupStatus,
} from "@/lib/hooks/use-setup";

function resolveRedeemError(
  error: unknown,
  t: ReturnType<typeof useTranslations<"setup">>,
): string {
  if (error instanceof ApiError) {
    if (error.status === 429 || error.code === "RATE_LIMITED") {
      return t("rateLimited");
    }
    if (error.status === 401) {
      return t("invalidToken");
    }
  }
  return t("requestFailed");
}

function LoadingState(): React.ReactElement {
  const common = useTranslations("common");

  return (
    <div
      aria-live="polite"
      className="flex items-center gap-2 text-muted-foreground text-sm"
    >
      <Spinner />
      {common("loading")}
    </div>
  );
}

export function SetupForm(): React.ReactElement {
  const t = useTranslations("setup");
  const format = useFormatter();
  const status = useSetupStatus();
  const callbackError = useSearchParams().get("error");
  const setupRequired = status.data?.setupRequired === true;
  const session = useSetupSession(setupRequired);
  const redeem = useRedeemSetupToken();
  const alertRef = useRef<HTMLDivElement>(null);
  const tokenId = useId();
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    if (submitError) {
      alertRef.current?.focus();
    }
  }, [submitError]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitError(null);

    const formData = new FormData(event.currentTarget);
    const token = String(formData.get("token") ?? "").trim();

    try {
      await redeem.mutateAsync({ token });
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) {
        await status.refetch();
        return;
      }
      setSubmitError(resolveRedeemError(error, t));
    }
  }

  if (status.isPending) {
    return (
      <AuthLayout title={t("title")}>
        <LoadingState />
      </AuthLayout>
    );
  }

  if (status.isError) {
    return (
      <AuthLayout title={t("title")}>
        <AuthErrorAlert
          description={t("statusFailed")}
          title={t("statusFailedTitle")}
        />
      </AuthLayout>
    );
  }

  if (!status.data.setupRequired) {
    return (
      <AuthLayout title={t("title")}>
        <AuthSuccessAlert
          description={t("alreadyDescription")}
          title={t("alreadyTitle")}
        />
        {status.data.installUrl ? (
          <Button render={<a href={status.data.installUrl} />}>
            {t("installApp")}
          </Button>
        ) : null}
        <Button
          render={<Link href="/login" />}
          variant={status.data.installUrl ? "outline" : "default"}
        >
          {t("goToLogin")}
        </Button>
      </AuthLayout>
    );
  }

  if (session.isPending) {
    return (
      <AuthLayout title={t("title")}>
        <LoadingState />
      </AuthLayout>
    );
  }

  if (session.isError) {
    return (
      <AuthLayout title={t("title")}>
        <AuthErrorAlert
          description={t("requestFailed")}
          title={t("requestFailedTitle")}
        />
      </AuthLayout>
    );
  }

  if (session.data) {
    return (
      <AuthLayout title={t("title")}>
        <AuthSuccessAlert
          description={t("sessionOpenDescription", {
            time: formatAppDateTime(
              format,
              session.data.expiresAt,
              session.data.expiresAt,
            ),
          })}
          title={t("sessionOpenTitle")}
        />
        <CreateAppForm
          callbackError={callbackError}
          onSessionLost={() => {
            void status.refetch();
            void session.refetch();
          }}
        />
      </AuthLayout>
    );
  }

  return (
    <AuthLayout subtitle={t("description")} title={t("title")}>
      {submitError ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={submitError}
          title={t("tokenRefusedTitle")}
        />
      ) : callbackError === "session_expired" ? (
        <AuthErrorAlert
          description={t("callbackSessionExpired")}
          title={t("callbackSessionExpiredTitle")}
        />
      ) : callbackError === "rate_limited" ? (
        <AuthErrorAlert
          description={t("rateLimited")}
          title={t("requestFailedTitle")}
        />
      ) : null}
      <form className={AUTH_FORM_CLASS} onSubmit={handleSubmit}>
        <Field name="token">
          <FieldLabel htmlFor={tokenId}>
            {t("tokenLabel")} <span aria-hidden="true">*</span>
          </FieldLabel>
          <Input
            autoComplete="off"
            autoFocus
            id={tokenId}
            name="token"
            required
            spellCheck={false}
            type="password"
          />
        </Field>
        <Button loading={redeem.isPending} type="submit">
          {t("submit")}
        </Button>
      </form>
    </AuthLayout>
  );
}
