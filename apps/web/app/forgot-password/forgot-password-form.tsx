"use client";

import { CircleAlertIcon, CircleCheckIcon } from "lucide-react";
import Link from "next/link";
import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardPanel, CardTitle } from "@/components/ui/card";
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
      <Card>
        <CardHeader>
          <CardTitle>{t("forgotPassword")}</CardTitle>
        </CardHeader>
        <CardPanel className="flex flex-col gap-4">
          <Alert variant="success">
            <CircleCheckIcon />
            <AlertTitle>{t("forgotPasswordSuccessTitle")}</AlertTitle>
            <AlertDescription>{t("forgotPasswordSuccess")}</AlertDescription>
          </Alert>
          <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
        </CardPanel>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("forgotPassword")}</CardTitle>
      </CardHeader>
      <CardPanel className="flex flex-col gap-4">
        <p className="text-muted-foreground text-sm">
          {t("forgotPasswordDescription")}
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
      </CardPanel>
    </Card>
  );
}
