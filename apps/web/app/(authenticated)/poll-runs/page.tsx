"use client";

import { useTranslations } from "next-intl";
import { PageHeader } from "@/components/common/page-header";
import { PollRunHistory } from "@/components/dashboard/poll-run-history";

export default function PollRunsPage(): React.ReactElement {
  const t = useTranslations("pollRuns");

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <PageHeader title={t("title")} />
      <PollRunHistory />
    </div>
  );
}
