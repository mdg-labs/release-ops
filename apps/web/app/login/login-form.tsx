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
    <AuthLayout title={t("login")}>
      {submitError ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={submitError}
          title={t("loginFailedTitle")}
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
        <div className="text-right text-sm">
          <Link
            className="text-muted-foreground underline-offset-4 hover:underline"
            href="/forgot-password"
          >
            {t("forgotPasswordLink")}
          </Link>
        </div>
        <Button loading={login.isPending} type="submit">
          {t("signIn")}
        </Button>
      </form>
    </AuthLayout>
  );
}
