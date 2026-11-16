"use client";

import { useTranslations } from "next-intl";
import { JobTime } from "@/components/jobs/job-time";
import { JobStateBadge, RunOutcomeBadge } from "@/components/jobs/job-badges";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError } from "@/lib/api/client";
import { useJob } from "@/lib/query/jobs";
import type { JobDetail, JobRun } from "@/lib/query/types";

function RunSection({ run }: { run: JobRun }): React.ReactElement {
  const t = useTranslations("jobs.detail");
  const tJobs = useTranslations("jobs");

  return (
    <section
      aria-label={t("attempt", { attempt: run.attempt })}
      className="space-y-2 rounded-lg border p-3"
    >
      <div className="flex flex-wrap items-center gap-2">
        <h4 className="font-medium text-sm">
          {t("attempt", { attempt: run.attempt })}
        </h4>
        <RunOutcomeBadge outcome={run.outcome} />
        <span className="text-muted-foreground text-xs">
          <JobTime value={run.startedAt} />
          {run.finishedAt ? (
            <>
              {" → "}
              <JobTime value={run.finishedAt} />
            </>
          ) : null}
        </span>
      </div>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        {run.exitCode !== null ? (
          <>
            <dt className="text-muted-foreground">{t("exitCode")}</dt>
            <dd>{run.exitCode}</dd>
          </>
        ) : null}
        {run.errorCode ? (
          <>
            <dt className="text-muted-foreground">{t("errorCode")}</dt>
            <dd>{run.errorCode}</dd>
          </>
        ) : null}
        {run.errorMessage ? (
          <>
            <dt className="text-muted-foreground">{t("error")}</dt>
            <dd className="break-words">{run.errorMessage}</dd>
          </>
        ) : null}
        {run.reportStatus ? (
          <>
            <dt className="text-muted-foreground">{t("report")}</dt>
            <dd>{tJobs(`reportStatuses.${run.reportStatus}`)}</dd>
          </>
        ) : null}
      </dl>
      <div className="space-y-1">
        <h5 className="font-medium text-muted-foreground text-xs">
          {t("logExcerpt")}
        </h5>
        {run.logExcerpt ? (
          <pre
            aria-label={t("logExcerpt")}
            className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-2 font-mono text-xs"
            tabIndex={0}
          >
            {run.logExcerpt}
          </pre>
        ) : (
          <p className="text-muted-foreground text-sm">{t("noLog")}</p>
        )}
      </div>
    </section>
  );
}

function DetailBody({ job }: { job: JobDetail }): React.ReactElement {
  const t = useTranslations("jobs.detail");

  return (
    <div className="space-y-5">
      <section className="space-y-2">
        <h3 className="font-semibold text-sm">{t("runs")}</h3>
        {job.runs.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t("noRuns")}</p>
        ) : (
          job.runs.map((run) => <RunSection key={run.attempt} run={run} />)
        )}
      </section>
      <section className="space-y-2">
        <h3 className="font-semibold text-sm">{t("events")}</h3>
        {job.events.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t("noEvents")}</p>
        ) : (
          <ul className="space-y-1 text-sm">
            {job.events.map((event, index) => (
              <li
                className="flex flex-wrap gap-x-2"
                key={`${event.at}-${index}`}
              >
                <span className="text-muted-foreground">
                  <JobTime value={event.at} />
                </span>
                <span className="font-medium">{event.kind}</span>
                {event.message ? <span>{event.message}</span> : null}
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

type JobDetailDialogProps = {
  /** The job to show; the dialog is closed while null. */
  jobId: string | null;
  onClose: () => void;
};

/** The per-job detail of docs/specs.html §8.3: its attempts, redacted log excerpts and events. */
export function JobDetailDialog({
  jobId,
  onClose,
}: JobDetailDialogProps): React.ReactElement {
  const t = useTranslations("jobs");
  const query = useJob(jobId);
  const job = query.isError ? null : (query.data ?? null);

  let body: React.ReactElement;
  if (query.isError) {
    body = (
      <Alert variant="error">
        <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
          <span>
            {query.error instanceof ApiError && query.error.status === 404
              ? t("detail.notFound")
              : t("detail.loadFailed")}
          </span>
          {query.error instanceof ApiError &&
          query.error.status === 404 ? null : (
            <Button
              onClick={() => void query.refetch()}
              size="sm"
              variant="outline"
            >
              {t("retry")}
            </Button>
          )}
        </AlertDescription>
      </Alert>
    );
  } else if (job) {
    body = <DetailBody job={job} />;
  } else {
    body = (
      <div className="space-y-2">
        {Array.from({ length: 3 }, (_, index) => (
          <Skeleton
            className="h-8 w-full"
            data-testid="job-detail-skeleton"
            key={`skeleton-${index}`}
          />
        ))}
      </div>
    );
  }

  return (
    <Dialog
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      open={jobId !== null}
    >
      <DialogPopup className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {job
              ? t("detail.title", { repository: job.repository.fullName })
              : t("title")}
          </DialogTitle>
          <DialogDescription className="flex flex-wrap items-center gap-2">
            {job ? (
              <>
                <JobStateBadge state={job.state} />
                <span>{t(`triggers.${job.trigger}`)}</span>
              </>
            ) : null}
          </DialogDescription>
        </DialogHeader>
        <DialogPanel>{body}</DialogPanel>
        <DialogFooter variant="bare">
          <Button onClick={onClose} variant="outline">
            {t("detail.close")}
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}
