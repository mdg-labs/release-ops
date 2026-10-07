"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import {
  AUTH_FORM_CLASS,
  AuthErrorAlert,
  AuthLayout,
} from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { ApiError, apiClient } from "@/lib/api/client";
import type { LoginResponse } from "@/lib/query/types";

function resolveAuthError(
  error: unknown,
  t: ReturnType<typeof useTranslations<"auth">>,
): string {
  if (error instanceof ApiError) {
    if (error.status === 429 || error.code === "RATE_LIMITED") {
      return t("rateLimited");
    }
    if (
      error.code === "VALIDATION_ERROR" &&
      error.message.includes("invalid or expired token")
    ) {
      return t("invalidToken");
    }
    if (
      error.code === "VALIDATION_ERROR" &&
      error.message.includes("at least 8 characters")
    ) {
      return t("passwordTooShort");
    }
  }
  return t("requestFailed");
}

export function AcceptInvitationForm(): React.ReactElement {
  const t = useTranslations("auth");
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  const alertRef = useRef<HTMLDivElement>(null);
  const passwordId = useId();
  const confirmPasswordId = useId();
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [isPending, setIsPending] = useState(false);

  useEffect(() => {
    if (submitError) {
      alertRef.current?.focus();
    }
  }, [submitError]);

  if (!token) {
    return (
      <AuthLayout title={t("acceptInvitation")}>
        <AuthErrorAlert
          description={t("invalidToken")}
          title={t("invalidTokenTitle")}
        />
        <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
      </AuthLayout>
    );
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmitError(null);

    const formData = new FormData(event.currentTarget);
    const password = String(formData.get("password") ?? "");
    const confirmPassword = String(formData.get("confirmPassword") ?? "");

    if (password.length < 8) {
      setSubmitError(t("passwordTooShort"));
      return;
    }
    if (password !== confirmPassword) {
      setSubmitError(t("passwordMismatch"));
      return;
    }

    setIsPending(true);
    try {
      await apiClient.post<LoginResponse>("/auth/accept-invitation", {
        token,
        password,
      });
      router.push("/");
      router.refresh();
    } catch (error) {
      setSubmitError(resolveAuthError(error, t));
    } finally {
      setIsPending(false);
    }
  }

  return (
    <AuthLayout
      subtitle={t("acceptInvitationDescription")}
      title={t("acceptInvitation")}
    >
      {submitError ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={submitError}
          title={t("requestFailedTitle")}
        />
      ) : null}
      <form className={AUTH_FORM_CLASS} onSubmit={handleSubmit}>
        <Field name="password">
          <FieldLabel htmlFor={passwordId}>
            {t("password")} <span aria-hidden="true">*</span>
          </FieldLabel>
          <Input
            autoComplete="new-password"
            autoFocus
            id={passwordId}
            name="password"
            required
            type="password"
          />
        </Field>
        <Field name="confirmPassword">
          <FieldLabel htmlFor={confirmPasswordId}>
            {t("confirmPassword")} <span aria-hidden="true">*</span>
          </FieldLabel>
          <Input
            autoComplete="new-password"
            id={confirmPasswordId}
            name="confirmPassword"
            required
            type="password"
          />
        </Field>
        <Button loading={isPending} type="submit">
          {t("setPassword")}
        </Button>
      </form>
      <Button render={<Link href="/login" />} variant="outline">
        {t("backToLogin")}
      </Button>
    </AuthLayout>
  );
}
