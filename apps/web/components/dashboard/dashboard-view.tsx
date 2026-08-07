"use client";

import { useTranslations } from "next-intl";
import { useEffect } from "react";
import { LastRunErrors } from "@/components/dashboard/last-run-errors";
import { PollRunHistory } from "@/components/dashboard/poll-run-history";
import { RepoStatusTable } from "@/components/dashboard/repo-status-table";
import { StatusCard } from "@/components/dashboard/status-card";
import { useTriggerPoll } from "@/lib/hooks/use-poll";
import { useStatus } from "@/lib/hooks/use-status";

export function DashboardView(): React.ReactElement {
  const t = useTranslations("dashboard");
  const { data, isLoading, isError, refetch } = useStatus();
  const triggerPoll = useTriggerPoll();

  const isPolling = Boolean(data?.isPolling || triggerPoll.isPending);

  useEffect(() => {
    if (!data?.isPolling) {
      return;
    }

    const intervalId = window.setInterval(() => {
      void refetch();
    }, 2000);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [data?.isPolling, refetch]);

  const lastRunErrors = data?.lastRun?.errors ?? [];

  return (
    <div className="flex min-w-0 w-full flex-col gap-6">
      <h1 className="font-semibold text-2xl">{t("title")}</h1>

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      <StatusCard
        isLoading={isLoading}
        isPolling={isPolling}
        isTriggerPending={triggerPoll.isPending}
        onRunPoll={() => {
          triggerPoll.mutate();
        }}
        status={data}
      />

      {!isLoading && lastRunErrors.length > 0 ? (
        <LastRunErrors errors={lastRunErrors} repos={data?.repos ?? []} />
      ) : null}

      <section className="flex flex-col gap-3">
        <h2 className="font-semibold text-lg">
          {t("pollHistorySectionTitle")}
        </h2>
        <PollRunHistory />
      </section>

      <section className="flex flex-col gap-3">
        <h2 className="font-semibold text-lg">{t("reposSectionTitle")}</h2>
        <RepoStatusTable isLoading={isLoading} repos={data?.repos ?? []} />
      </section>
    </div>
  );
}
