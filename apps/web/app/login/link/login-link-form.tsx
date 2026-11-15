"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useRef, useState } from "react";
import { AuthErrorAlert, AuthLayout } from "@/components/auth/auth-layout";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api/client";
import { useRedeemLoginLink } from "@/lib/hooks/use-login";

function resolveError(
  error: unknown,
  t: ReturnType<typeof useTranslations<"auth">>,
): string {
  if (error instanceof ApiError) {
    if (error.status === 429 || error.code === "RATE_LIMITED") {
      return t("rateLimited");
    }
    if (error.status === 400) {
      return t("loginLinkInvalid");
    }
  }
  return t("loginFailed");
}

// The token is only sent on the click: a link preview or a prefetch of this page must not
// spend the one-time link.
export function LoginLinkForm(): React.ReactElement {
  const t = useTranslations("auth");
  const router = useRouter();
  const token = useSearchParams().get("token") ?? "";
  const redeem = useRedeemLoginLink();
  const alertRef = useRef<HTMLDivElement>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (error) {
      alertRef.current?.focus();
    }
  }, [error]);

  async function handleConfirm() {
    setError(null);
    try {
      await redeem.mutateAsync({ token });
      router.push("/");
      router.refresh();
    } catch (failure) {
      setError(resolveError(failure, t));
    }
  }

  return (
    <AuthLayout
      subtitle={t("loginLinkDescription")}
      title={t("loginLinkTitle")}
    >
      {error || !token ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={error ?? t("loginLinkInvalid")}
          title={t("loginFailedTitle")}
        />
      ) : null}
      <Button
        disabled={!token}
        loading={redeem.isPending}
        onClick={() => void handleConfirm()}
      >
        {t("loginLinkConfirm")}
      </Button>
    </AuthLayout>
  );
}
