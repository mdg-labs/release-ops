"use client";

import Link from "next/link";
import { useTranslations } from "next-intl";
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
import { ApiError, apiClient } from "@/lib/api/client";

function resolveAuthError(
  error: unknown,
  t: ReturnType<typeof useTranslations<"auth">>,
): string {
  if (error instanceof ApiError) {
    if (error.status === 429 || error.code === "RATE_LIMITED") {
      return t("rateLimited");
    }
  }
  return t("requestFailed");
}

export function ForgotPasswordForm(): React.ReactElement {
  const t = useTranslations("auth");
  const alertRef = useRef<HTMLDivElement>(null);
  const emailId = useId();
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [isSuccess, setIsSuccess] = useState(false);
  const [isPending, setIsPending] = useState(false);

  useEffect(() => {
    if (submitError) {
      alertRef.current?.focus();
    }
  }, [submitError]);

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitError(null);

    const formData = new FormData(event.currentTarget);
    const email = String(formData.get("email") ?? "");

    setIsPending(true);
    try {
      await apiClient.post("/auth/forgot-password", { email });
      setIsSuccess(true);
    } catch (error) {
      setSubmitError(resolveAuthError(error, t));
    } finally {
      setIsPending(false);
    }
  }

  if (isSuccess) {
    return (
      <AuthLayout title={t("forgotPassword")}>
        <AuthSuccessAlert
          description={t("forgotPasswordSuccess")}
          title={t("forgotPasswordSuccessTitle")}
        />
        <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout
      subtitle={t("forgotPasswordDescription")}
      title={t("forgotPassword")}
    >
      {submitError ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={submitError}
          title={t("requestFailedTitle")}
        />
      ) : null}
      <form className={AUTH_FORM_CLASS} onSubmit={handleSubmit}>
        <Field name="email">
          <FieldLabel htmlFor={emailId}>
            {t("email")} <span aria-hidden="true">*</span>
          </FieldLabel>
          <Input
            autoComplete="email"
            autoFocus
            id={emailId}
            name="email"
            required
            type="email"
          />
        </Field>
        <Button loading={isPending} type="submit">
          {t("sendResetLink")}
        </Button>
      </form>
      <Button render={<Link href="/login" />} variant="outline">
        {t("backToLogin")}
      </Button>
    </AuthLayout>
  );
}
