"use client";

import { CircleAlertIcon } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardPanel, CardTitle } from "@/components/ui/card";
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
      <Card>
        <CardHeader>
          <CardTitle>{t("acceptInvitation")}</CardTitle>
        </CardHeader>
        <CardPanel className="flex flex-col gap-4">
          <Alert variant="error">
            <CircleAlertIcon />
            <AlertTitle>{t("invalidTokenTitle")}</AlertTitle>
            <AlertDescription>{t("invalidToken")}</AlertDescription>
          </Alert>
          <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
        </CardPanel>
      </Card>
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
    <Card>
      <CardHeader>
        <CardTitle>{t("acceptInvitation")}</CardTitle>
      </CardHeader>
      <CardPanel className="flex flex-col gap-4">
        <p className="text-muted-foreground text-sm">
          {t("acceptInvitationDescription")}
        </p>
        {submitError ? (
          <div ref={alertRef} tabIndex={-1}>
            <Alert variant="error">
              <CircleAlertIcon />
              <AlertTitle>{t("requestFailedTitle")}</AlertTitle>
              <AlertDescription>{submitError}</AlertDescription>
            </Alert>
          </div>
        ) : null}
        <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
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
      </CardPanel>
    </Card>
  );
}
