"use client";

import { CircleAlertIcon } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useId, useRef, useState } from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardHeader, CardPanel, CardTitle } from "@/components/ui/card";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api/client";
import { useLogin } from "@/lib/hooks/use-login";

export function LoginForm(): React.ReactElement {
  const t = useTranslations("auth");
  const router = useRouter();
  const searchParams = useSearchParams();
  const login = useLogin();
  const alertRef = useRef<HTMLDivElement>(null);
  const emailId = useId();
  const passwordId = useId();
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
    const email = String(formData.get("email") ?? "");
    const password = String(formData.get("password") ?? "");

    try {
      await login.mutateAsync({ email, password });
      const redirectTo = searchParams.get("redirect") ?? "/";
      router.push(redirectTo);
      router.refresh();
    } catch (error) {
      if (error instanceof ApiError) {
        setSubmitError(t("invalidCredentials"));
      } else {
        setSubmitError(t("loginFailed"));
      }
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("login")}</CardTitle>
      </CardHeader>
      <CardPanel className="flex flex-col gap-4">
        {submitError ? (
          <div ref={alertRef} tabIndex={-1}>
            <Alert variant="error">
              <CircleAlertIcon />
              <AlertTitle>{t("loginFailedTitle")}</AlertTitle>
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
          <Field name="password">
            <FieldLabel htmlFor={passwordId}>
              {t("password")} <span aria-hidden="true">*</span>
            </FieldLabel>
            <Input
              autoComplete="current-password"
              id={passwordId}
              name="password"
              required
              type="password"
            />
          </Field>
          <Button loading={login.isPending} type="submit">
            {t("signIn")}
          </Button>
        </form>
      </CardPanel>
    </Card>
  );
}
