"use client";

import { LockIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { ApiError } from "@/lib/api/client";

/** True for the API's 403: the role does not allow the call. */
export function isForbiddenError(error: unknown): boolean {
  return error instanceof ApiError && error.status === 403;
}

export function ForbiddenState(): React.ReactElement {
  const t = useTranslations("common");

  return (
    <Empty data-testid="forbidden-state" role="alert">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <LockIcon aria-hidden="true" />
        </EmptyMedia>
        <EmptyTitle>{t("forbiddenTitle")}</EmptyTitle>
        <EmptyDescription>{t("forbiddenDescription")}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
