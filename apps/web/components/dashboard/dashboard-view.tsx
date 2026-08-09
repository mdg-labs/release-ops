"use client";

import { useTranslations } from "next-intl";
import { useEffect } from "react";
import { PageHeader } from "@/components/common/page-header";
import { LastRunErrors } from "@/components/dashboard/last-run-errors";
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
      <PageHeader title={t("title")} />

      {isError ? (
        <p className="text-destructive-foreground text-sm" role="alert">
          {t("loadFailed")}
        </p>
      ) : null}

      <section
        className="flex flex-col gap-6"
        data-slot="dashboard-status-section"
      >
        <StatusCard
          isLoading={isLoading}
          isPolling={isPolling}
          isTriggerPending={triggerPoll.isPending}
          onRunPoll={() => {
            triggerPoll.mutate();
          }}
          status={data}
        />
      </section>

      {!isLoading && lastRunErrors.length > 0 ? (
        <section
          className="flex flex-col gap-6"
          data-slot="dashboard-errors-section"
        >
          <LastRunErrors errors={lastRunErrors} repos={data?.repos ?? []} />
        </section>
      ) : null}

      <section
        className="flex flex-col gap-6"
        data-slot="dashboard-repos-section"
      >
        <h2 className="font-semibold text-lg tracking-tight">
          {t("reposSectionTitle")}
        </h2>
        <RepoStatusTable isLoading={isLoading} repos={data?.repos ?? []} />
      </section>
    </div>
  );
}
