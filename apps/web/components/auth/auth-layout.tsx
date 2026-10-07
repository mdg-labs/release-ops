"use client";

import { CircleAlertIcon, CircleCheckIcon, RadioIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import type React from "react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  Card,
  CardDescription,
  CardHeader,
  CardPanel,
  CardTitle,
} from "@/components/ui/card";
import { cn } from "@/lib/utils";

export const AUTH_FORM_CLASS = "flex flex-col gap-4";

type AuthLayoutProps = {
  children: React.ReactNode;
  title: string;
  subtitle?: string;
};

export function AuthLayout({
  children,
  title,
  subtitle,
}: AuthLayoutProps): React.ReactElement {
  const t = useTranslations("common");

  return (
    <div className="flex min-h-svh w-full flex-col items-center overflow-y-auto bg-background px-4 py-6 sm:py-10">
      <div className="my-auto w-full max-w-sm space-y-6">
        <div className="flex flex-col items-center gap-2 text-center">
          <div className="flex size-10 items-center justify-center rounded-xl border bg-muted">
            <RadioIcon aria-hidden className="size-5" />
          </div>
          <div className="space-y-1">
            <p className="font-semibold text-lg">{t("appTitle")}</p>
            <p className="text-muted-foreground text-sm">
              {t("appDescription")}
            </p>
          </div>
        </div>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle>{title}</CardTitle>
            {subtitle ? <CardDescription>{subtitle}</CardDescription> : null}
          </CardHeader>
          <CardPanel className={cn("flex flex-col gap-4 pt-0")}>
            {children}
          </CardPanel>
        </Card>
      </div>
    </div>
  );
}

type AuthErrorAlertProps = {
  alertRef?: React.RefObject<HTMLDivElement | null>;
  description: string;
  title: string;
};

export function AuthErrorAlert({
  alertRef,
  description,
  title,
}: AuthErrorAlertProps): React.ReactElement {
  return (
    <div ref={alertRef} tabIndex={-1}>
      <Alert variant="error">
        <CircleAlertIcon />
        <AlertTitle>{title}</AlertTitle>
        <AlertDescription>{description}</AlertDescription>
      </Alert>
    </div>
  );
}

type AuthSuccessAlertProps = {
  description: string;
  title: string;
};

export function AuthSuccessAlert({
  description,
  title,
}: AuthSuccessAlertProps): React.ReactElement {
  return (
    <Alert variant="success">
      <CircleCheckIcon />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>{description}</AlertDescription>
    </Alert>
  );
}
