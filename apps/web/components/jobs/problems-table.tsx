"use client";

import { useTranslations } from "next-intl";
import { JobTime } from "@/components/jobs/job-time";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Problem, ProblemKind } from "@/lib/query/types";

const KIND_VARIANTS: Record<ProblemKind, BadgeProps["variant"]> = {
  job_failed: "error",
  ticket_failed: "error",
  rate_limited: "warning",
  permissions_lost: "warning",
};

/** An external link is only rendered for an http(s) address. */
function webUrl(value: string | undefined): string | null {
  if (!value) {
    return null;
  }
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? value : null;
  } catch {
    return null;
  }
}

type ProblemsTableProps = {
  items: Problem[];
  isAdmin: boolean;
  onOpenJob: (id: string) => void;
  onClearMarker: (updateTicketId: string, repository: string) => void;
};

function MessageCell({
  problem,
  isAdmin,
}: {
  problem: Problem;
  isAdmin: boolean;
}): React.ReactElement {
  const t = useTranslations("jobs.problems");
  if (problem.message) {
    return <span className="break-words">{problem.message}</span>;
  }
  return (
    <span className="text-muted-foreground">
      {problem.kind === "ticket_failed" && !isAdmin
        ? t("messageAdminOnly")
        : t("noMessage")}
    </span>
  );
}

function ActionsCell({
  problem,
  isAdmin,
  onOpenJob,
  onClearMarker,
}: ProblemsTableProps & { problem: Problem }): React.ReactElement {
  const t = useTranslations("jobs.problems");
  const jobId = problem.ref?.jobId;
  const pullRequest = webUrl(problem.ref?.updatePrUrl);
  const clearId = isAdmin ? problem.ref?.updateTicketId : undefined;

  return (
    <div className="flex flex-wrap items-center gap-2">
      {jobId ? (
        <Button onClick={() => onOpenJob(jobId)} size="sm" variant="outline">
          {t("viewJob")}
        </Button>
      ) : null}
      {pullRequest ? (
        <a
          className="text-sm underline underline-offset-4"
          href={pullRequest}
          rel="noopener noreferrer"
          target="_blank"
        >
          {t("viewPullRequest")}
        </a>
      ) : null}
      {clearId ? (
        <Button
          onClick={() =>
            onClearMarker(clearId, problem.repository?.fullName ?? "")
          }
          size="sm"
          variant="destructive-outline"
        >
          {t("clear")}
        </Button>
      ) : null}
    </div>
  );
}

export function ProblemsTable(props: ProblemsTableProps): React.ReactElement {
  const { items, isAdmin } = props;
  const t = useTranslations("jobs.problems");

  return (
    <Table variant="card">
      <TableHeader>
        <TableRow>
          <TableHead>{t("columns.kind")}</TableHead>
          <TableHead>{t("columns.repository")}</TableHead>
          <TableHead>{t("columns.since")}</TableHead>
          <TableHead>{t("columns.message")}</TableHead>
          <TableHead>{t("columns.actions")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((problem, index) => (
          <TableRow
            key={`${problem.kind}-${problem.repository?.id ?? "app"}-${problem.ref?.jobId ?? problem.ref?.updatePrId ?? index}`}
          >
            <TableCell>
              <Badge variant={KIND_VARIANTS[problem.kind]}>
                {t(`kinds.${problem.kind}`)}
              </Badge>
            </TableCell>
            <TableCell className="whitespace-nowrap">
              {problem.repository ? (
                problem.repository.fullName
              ) : (
                <span className="text-muted-foreground">
                  {t("noRepository")}
                </span>
              )}
            </TableCell>
            <TableCell className="whitespace-nowrap">
              <JobTime value={problem.since} />
            </TableCell>
            <TableCell className="max-w-96">
              <MessageCell isAdmin={isAdmin} problem={problem} />
            </TableCell>
            <TableCell>
              <ActionsCell {...props} problem={problem} />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
