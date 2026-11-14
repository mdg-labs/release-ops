"use client";

import { useTranslations } from "next-intl";
import type React from "react";
import { ForbiddenState } from "@/components/common/forbidden-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useSessionRole } from "@/lib/hooks/use-session";

/**
 * Renders its children for an admin only. The Go API refuses a viewer on every admin
 * endpoint; this keeps a viewer from seeing a page whose every request would fail. While the
 * role is loading nothing of the page renders, and a role that could not be read is an error,
 * not a page.
 */
export function AdminOnly({
  children,
}: {
  children: React.ReactNode;
}): React.ReactElement {
  const t = useTranslations("common");
  const { role, isLoading, isError } = useSessionRole();

  if (isLoading) {
    return <Skeleton className="h-8 w-full" data-testid="admin-only-loading" />;
  }
  if (isError || role === null) {
    return (
      <p className="text-destructive-foreground text-sm" role="alert">
        {t("sessionLoadFailed")}
      </p>
    );
  }
  if (role !== "admin") {
    return <ForbiddenState />;
  }
  return <>{children}</>;
}
