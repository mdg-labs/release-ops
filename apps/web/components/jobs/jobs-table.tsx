"use client";

import { useTranslations } from "next-intl";
import { DurationText, elapsedMs, JobTime } from "@/components/jobs/job-time";
import { JobStateBadge } from "@/components/jobs/job-badges";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Job } from "@/lib/query/types";

function TriggerCell({ job }: { job: Job }): React.ReactElement {
  const t = useTranslations("jobs");
  return (
    <div className="flex flex-col gap-0.5">
      <span>{t(`triggers.${job.trigger}`)}</span>
      {job.matchedMarkers.length > 0 ? (
        <span className="max-w-64 truncate text-muted-foreground text-xs">
          {t("markers", { markers: job.matchedMarkers.join(", ") })}
        </span>
      ) : null}
    </div>
  );
}

export function JobsTable({
  items,
  onOpenJob,
}: {
  items: Job[];
  onOpenJob: (id: string) => void;
}): React.ReactElement {
  const t = useTranslations("jobs");

  return (
    <Table variant="card">
      <TableHeader>
        <TableRow>
          <TableHead>{t("columns.repository")}</TableHead>
          <TableHead>{t("columns.state")}</TableHead>
          <TableHead>{t("columns.priority")}</TableHead>
          <TableHead>{t("columns.trigger")}</TableHead>
          <TableHead>{t("columns.attempts")}</TableHead>
          <TableHead>{t("columns.created")}</TableHead>
          <TableHead>{t("columns.started")}</TableHead>
          <TableHead>{t("columns.finished")}</TableHead>
          <TableHead>{t("columns.duration")}</TableHead>
          <TableHead>{t("columns.latency")}</TableHead>
          <TableHead>
            <span className="sr-only">{t("columns.actions")}</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((job) => {
          const duration = elapsedMs(job.startedAt, job.finishedAt);
          return (
            <TableRow key={job.id}>
              <TableCell className="whitespace-nowrap font-medium">
                {job.repository.fullName}
              </TableCell>
              <TableCell>
                <JobStateBadge state={job.state} />
              </TableCell>
              <TableCell>{t(`priorities.${job.priority}`)}</TableCell>
              <TableCell>
                <TriggerCell job={job} />
              </TableCell>
              <TableCell>{job.attempts}</TableCell>
              <TableCell className="whitespace-nowrap">
                <JobTime value={job.createdAt} />
              </TableCell>
              <TableCell className="whitespace-nowrap">
                <JobTime placeholder={t("notYet")} value={job.startedAt} />
              </TableCell>
              <TableCell className="whitespace-nowrap">
                <JobTime placeholder={t("notYet")} value={job.finishedAt} />
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {duration === null ? (
                  <span className="text-muted-foreground">
                    {t("noDuration")}
                  </span>
                ) : (
                  <DurationText ms={duration} />
                )}
              </TableCell>
              <TableCell className="whitespace-nowrap">
                {job.startLatencyMs === null ? (
                  <span className="text-muted-foreground">
                    {t("noDuration")}
                  </span>
                ) : (
                  <DurationText ms={job.startLatencyMs} />
                )}
              </TableCell>
              <TableCell>
                <Button
                  aria-label={t("viewDetails", {
                    repository: job.repository.fullName,
                  })}
                  onClick={() => onOpenJob(job.id)}
                  size="sm"
                  variant="outline"
                >
                  {t("columns.actions")}
                </Button>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
