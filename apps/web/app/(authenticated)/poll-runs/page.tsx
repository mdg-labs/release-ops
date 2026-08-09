"use client";

import { useTranslations } from "next-intl";
import { PollRunHistory } from "@/components/dashboard/poll-run-history";

export default function PollRunsPage(): React.ReactElement {
  const t = useTranslations("pollRuns");

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <h1 className="font-semibold text-2xl">{t("title")}</h1>
      <PollRunHistory />
    </div>
  );
}
