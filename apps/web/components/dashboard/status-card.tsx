"use client";

import Link from "next/link";
import { useFormatter, useTranslations } from "next-intl";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardDescription,
  CardFooter,
  CardHeader,
  CardPanel,
  CardTitle,
} from "@/components/ui/card";
import {
  Frame,
  FrameDescription,
  FrameFooter,
  FrameHeader,
  FrameTitle,
} from "@/components/ui/frame";
import { Skeleton } from "@/components/ui/skeleton";
import type { PollRun, StatusResponse } from "@/lib/query/types";

type StatusCardProps = {
  status: StatusResponse | undefined;
  isLoading: boolean;
  isPolling: boolean;
  isTriggerPending: boolean;
  onRunPoll: () => void;
};

function runStatusVariant(
  status: string | undefined,
  isPolling: boolean,
): "success" | "error" | "warning" | "info" | "secondary" {
  if (isPolling) {
    return "info";
  }

  switch (status) {
    case "success":
      return "success";
    case "failed":
      return "error";
    case "running":
      return "info";
    default:
      return "secondary";
  }
}

function runStatusLabel(
  status: string | undefined,
  isPolling: boolean,
  t: ReturnType<typeof useTranslations<"dashboard">>,
): string {
  if (isPolling) {
    return t("runStatus.polling");
  }

  if (!status) {
    return t("runStatus.never");
  }

  switch (status) {
    case "success":
      return t("runStatus.success");
    case "failed":
      return t("runStatus.failed");
    case "running":
      return t("runStatus.running");
    default:
      return status;
  }
}

function formatTimestamp(
  value: string | null | undefined,
  format: ReturnType<typeof useFormatter>,
  neverLabel: string,
): string {
  if (!value) {
    return neverLabel;
  }

  return format.dateTime(new Date(value), {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

function LastRunSummary({
  lastRun,
  format,
  t,
}: {
  lastRun: PollRun | null;
  format: ReturnType<typeof useFormatter>;
  t: ReturnType<typeof useTranslations<"dashboard">>;
}): React.ReactElement {
  if (!lastRun) {
    return <p className="text-muted-foreground text-sm">{t("lastRunNever")}</p>;
  }

  return (
    <dl className="grid gap-3 text-sm sm:grid-cols-2">
      <div>
        <dt className="text-muted-foreground">{t("lastRunStarted")}</dt>
        <dd className="font-medium">
          {formatTimestamp(lastRun.startedAt, format, t("lastRunNever"))}
        </dd>
      </div>
      <div>
        <dt className="text-muted-foreground">{t("lastRunFinished")}</dt>
        <dd className="font-medium">
          {formatTimestamp(lastRun.finishedAt, format, t("lastRunInProgress"))}
        </dd>
      </div>
      <div>
        <dt className="text-muted-foreground">{t("lastRunReposChecked")}</dt>
        <dd className="font-medium">{lastRun.reposChecked}</dd>
      </div>
      <div>
        <dt className="text-muted-foreground">{t("lastRunTicketsCreated")}</dt>
        <dd className="font-medium">{lastRun.ticketsCreated}</dd>
      </div>
    </dl>
  );
}

export function StatusCard({
  status,
  isLoading,
  isPolling,
  isTriggerPending,
  onRunPoll,
}: StatusCardProps): React.ReactElement {
  const t = useTranslations("dashboard");
  const format = useFormatter();

  const lastRun = status?.lastRun ?? null;
  const runStatus = lastRun?.status;
  const badgeVariant = runStatusVariant(runStatus, isPolling);
  const badgeLabel = runStatusLabel(runStatus, isPolling, t);
  const pollBusy = isPolling || isTriggerPending;

  return (
    <Frame>
      <FrameHeader>
        <FrameTitle>{t("statusCardTitle")}</FrameTitle>
        <FrameDescription>{t("statusCardDescription")}</FrameDescription>
      </FrameHeader>
      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <CardTitle>{t("lastPollTitle")}</CardTitle>
              <CardDescription>
                {t("pollIntervalValue", {
                  minutes: status?.pollIntervalMinutes ?? "—",
                })}
              </CardDescription>
            </div>
            {isLoading ? (
              <Skeleton className="h-5.5 w-24" />
            ) : (
              <Badge variant={badgeVariant}>{badgeLabel}</Badge>
            )}
          </div>
        </CardHeader>
        <CardPanel>
          {isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : (
            <LastRunSummary format={format} lastRun={lastRun} t={t} />
          )}
        </CardPanel>
        <CardFooter className="flex flex-wrap gap-2">
          <Button disabled={pollBusy} loading={pollBusy} onClick={onRunPoll}>
            {t("runPoll")}
          </Button>
          <Button render={<Link href="/poll-runs" />} variant="outline">
            {t("viewAllRuns")}
          </Button>
        </CardFooter>
      </Card>
      {pollBusy ? (
        <FrameFooter>
          <p className="text-muted-foreground text-sm">
            {t("pollingInProgress")}
          </p>
        </FrameFooter>
      ) : null}
    </Frame>
  );
}
