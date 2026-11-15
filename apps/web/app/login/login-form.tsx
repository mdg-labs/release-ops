"use client";

import { GithubIcon, InfoIcon } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useTranslations } from "next-intl";
import { useEffect, useRef, useState } from "react";
import { AuthErrorAlert, AuthLayout } from "@/components/auth/auth-layout";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { ApiError } from "@/lib/api/client";
import { githubLoginHref, loginErrorKey } from "@/lib/auth/login-error";
import { safeRedirectPath } from "@/lib/auth/safe-redirect";
import { useSetupStatus } from "@/lib/hooks/use-setup";

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

export function LoginForm(): React.ReactElement {
  const t = useTranslations("auth");
  const searchParams = useSearchParams();
  const setupStatus = useSetupStatus();
  const alertRef = useRef<HTMLDivElement>(null);
  const [redirecting, setRedirecting] = useState(false);

  const errorKey = loginErrorKey(searchParams.get("error"));
  const href = githubLoginHref(safeRedirectPath(searchParams.get("redirect")));

  useEffect(() => {
    if (errorKey) {
      alertRef.current?.focus();
    }
  }, [errorKey]);

  // The hand-off to GitHub leaves this page in the back-forward cache; coming back must not
  // find the button still spinning.
  useEffect(() => {
    function onPageShow(event: PageTransitionEvent) {
      if (event.persisted) {
        setRedirecting(false);
      }
    }
    window.addEventListener("pageshow", onPageShow);
    return () => window.removeEventListener("pageshow", onPageShow);
  }, []);

  if (setupStatus.isPending) {
    return (
      <AuthLayout title={t("login")}>
        <LoadingState />
      </AuthLayout>
    );
  }

  if (setupStatus.isError) {
    const rateLimited =
      setupStatus.error instanceof ApiError &&
      (setupStatus.error.status === 429 ||
        setupStatus.error.code === "RATE_LIMITED");
    return (
      <AuthLayout title={t("login")}>
        <AuthErrorAlert
          description={rateLimited ? t("rateLimited") : t("loginStatusFailed")}
          title={t("loginStatusFailedTitle")}
        />
        <Button onClick={() => void setupStatus.refetch()} variant="outline">
          {t("tryAgain")}
        </Button>
      </AuthLayout>
    );
  }

  if (setupStatus.data.setupRequired) {
    return (
      <AuthLayout title={t("login")}>
        <Alert variant="info">
          <InfoIcon />
          <AlertTitle>{t("setupRequiredTitle")}</AlertTitle>
          <AlertDescription>{t("setupRequired")}</AlertDescription>
        </Alert>
        <Button render={<Link href="/setup" />}>{t("goToSetup")}</Button>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title={t("login")}>
      {errorKey ? (
        <AuthErrorAlert
          alertRef={alertRef}
          description={t(errorKey)}
          title={t("loginFailedTitle")}
        />
      ) : null}
      <Button
        loading={redirecting}
        onClick={() => setRedirecting(true)}
        render={<a href={href} />}
        size="lg"
      >
        <GithubIcon aria-hidden />
        {t("signInWithGitHub")}
      </Button>
    </AuthLayout>
  );
}
