"use client";

import { LayoutDashboardIcon } from "lucide-react";
import { useTranslations } from "next-intl";
import { PageHeader } from "@/components/common/page-header";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

export function DashboardView(): React.ReactElement {
  const t = useTranslations("dashboard");

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <PageHeader title={t("title")} />
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <LayoutDashboardIcon aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{t("emptyTitle")}</EmptyTitle>
          <EmptyDescription>{t("emptyDescription")}</EmptyDescription>
        </EmptyHeader>
      </Empty>
    </div>
  );
}
