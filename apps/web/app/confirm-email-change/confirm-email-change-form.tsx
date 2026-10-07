"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useRef, useState } from "react";
import { AuthErrorAlert, AuthLayout } from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
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
  }
  return t("requestFailed");
}

export function ConfirmEmailChangeForm(): React.ReactElement {
  const t = useTranslations("auth");
  const router = useRouter();
  const searchParams = useSearchParams();
  const token = searchParams.get("token");
  const alertRef = useRef<HTMLDivElement>(null);
  const confirmRequestRef = useRef<Promise<void> | null>(null);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [isPending, setIsPending] = useState(false);

  useEffect(() => {
    if (submitError) {
      alertRef.current?.focus();
    }
  }, [submitError]);

  useEffect(() => {
    if (!token || confirmRequestRef.current) {
      return;
    }

    setIsPending(true);
    setSubmitError(null);

    confirmRequestRef.current = apiClient
      .post<LoginResponse>("/auth/confirm-email-change", { token })
      .then(() => {
        router.push("/");
        router.refresh();
      })
      .catch((error: unknown) => {
        setSubmitError(resolveAuthError(error, t));
      })
      .finally(() => {
        setIsPending(false);
      });
  }, [token, router, t]);

  if (!token) {
    return (
      <AuthLayout title={t("confirmEmailChange")}>
        <AuthErrorAlert
          description={t("invalidToken")}
          title={t("invalidTokenTitle")}
        />
        <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
      </AuthLayout>
    );
  }

  if (isPending) {
    return (
      <AuthLayout
        subtitle={t("confirmEmailChangeDescription")}
        title={t("confirmEmailChange")}
      >
        <div className="flex flex-col items-center gap-4 py-2">
          <Spinner className="size-6" />
        </div>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title={t("confirmEmailChange")}>
      {submitError ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={submitError}
          title={t("requestFailedTitle")}
        />
      ) : null}
      <Button render={<Link href="/login" />}>{t("backToLogin")}</Button>
    </AuthLayout>
  );
}
