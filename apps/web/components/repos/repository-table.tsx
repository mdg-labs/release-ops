"use client";

import { TriangleAlertIcon } from "lucide-react";
import Link from "next/link";
import { useFormatter, useTranslations } from "next-intl";
import { useState } from "react";
import { EnableRepoDialog } from "@/components/repos/enable-repo-dialog";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import { toastManager } from "@/components/ui/toast";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { ApiError } from "@/lib/api/client";
import { formatAppDateTime } from "@/lib/format/datetime";
import {
  useRunRepository,
  useUpdateRepository,
} from "@/lib/query/repositories";
import type {
  EnableRenovateResult,
  Repository,
  RepositoryConfigState,
  RepositoryJobState,
  TicketProject,
} from "@/lib/query/types";

/** What only an admin sees and uses on the page. */
export type AdminTools = {
  /** The ticket projects, or null while they load or when they could not be loaded. */
  projects: TicketProject[] | null;
  projectsLoading: boolean;
  /** The name of the installation's default ticket project; null when none is set. */
  defaultProjectName: string | null;
  /** The preset "Enable repo" extends, or null while it could not be read. */
  preset: string | null;
};

const CONFIG_VARIANTS: Record<RepositoryConfigState, BadgeProps["variant"]> = {
  present: "success",
  missing: "warning",
  unknown: "outline",
};

const JOB_VARIANTS: Record<RepositoryJobState, BadgeProps["variant"]> = {
  queued: "info",
  running: "info",
  succeeded: "success",
  failed: "error",
};

const INSTALLATION_DEFAULT = "installation-default";

/** An external link is only rendered for an http(s) address. */
function webUrl(value: string | null): string | null {
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

function ExternalLink({
  href,
  children,
}: {
  href: string | null;
  children: React.ReactNode;
}): React.ReactElement {
  const safe = webUrl(href);
  if (!safe) {
    return <>{children}</>;
  }
  return (
    <a
      className="underline-offset-4 hover:underline"
      href={safe}
      rel="noopener noreferrer"
      target="_blank"
    >
      {children}
    </a>
  );
}

function messageOf(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

function LastJobCell({
  repository,
}: {
  repository: Repository;
}): React.ReactElement {
  const t = useTranslations("repositories");
  const format = useFormatter();
  const job = repository.lastJob;

  if (!job) {
    return <span className="text-muted-foreground">{t("noJob")}</span>;
  }
  const active = job.state === "queued" || job.state === "running";
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <Badge variant={JOB_VARIANTS[job.state]}>
        {active ? <Spinner className="size-3" /> : null}
        {t(`jobStates.${job.state}`)}
      </Badge>
      {job.finishedAt ? (
        <time
          className="text-muted-foreground text-xs"
          dateTime={job.finishedAt}
        >
          {formatAppDateTime(format, job.finishedAt, "")}
        </time>
      ) : null}
    </div>
  );
}

function TicketProjectCell({
  repository,
  tools,
}: {
  repository: Repository;
  tools: AdminTools;
}): React.ReactElement {
  const t = useTranslations("repositories");
  const update = useUpdateRepository();

  if (tools.projectsLoading) {
    return <Skeleton className="h-8.5 w-48" />;
  }
  if (!tools.projects) {
    return (
      <span className="text-muted-foreground text-xs">
        {t("ticketProjectsUnavailable")}
      </span>
    );
  }

  const options = [
    {
      value: INSTALLATION_DEFAULT,
      label: t("installationDefault", {
        project: tools.defaultProjectName ?? t("installationDefaultNone"),
      }),
    },
    ...tools.projects.map((project) => ({
      value: project.id,
      label: project.name,
    })),
  ];
  const current = repository.ticketProjectId ?? INSTALLATION_DEFAULT;
  const selected = options.find((option) => option.value === current) ?? null;

  async function change(value: string): Promise<void> {
    try {
      await update.mutateAsync({
        id: repository.id,
        patch: {
          ticketProjectId: value === INSTALLATION_DEFAULT ? null : value,
        },
      });
    } catch (error) {
      toastManager.add({
        title: t("ticketProjectSaveFailed", {
          repository: repository.fullName,
          message: messageOf(error, t("actionFailedUnknown")),
        }),
        type: "error",
      });
    }
  }

  return (
    <div className="flex min-w-48 flex-col gap-1">
      <Select
        disabled={update.isPending}
        itemToStringValue={(item) => item.value}
        items={options}
        onValueChange={(next) => {
          if (next && next.value !== current) {
            void change(next.value);
          }
        }}
        value={selected}
      >
        <SelectTrigger
          aria-label={t("ticketProjectLabel", {
            repository: repository.fullName,
          })}
        >
          <SelectValue />
        </SelectTrigger>
        <SelectPopup>
          {options.map((option) => (
            <SelectItem key={option.value} value={option}>
              {option.label}
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
      {repository.effectiveTicketProjectId === null ? (
        <p className="flex items-center gap-1 text-warning-foreground text-xs">
          <TriangleAlertIcon aria-hidden="true" className="size-3 shrink-0" />
          {t("noTicketProject")}
        </p>
      ) : null}
    </div>
  );
}

function EnabledCell({
  repository,
}: {
  repository: Repository;
}): React.ReactElement {
  const t = useTranslations("repositories");
  const update = useUpdateRepository();

  async function toggle(enabled: boolean): Promise<void> {
    try {
      await update.mutateAsync({ id: repository.id, patch: { enabled } });
    } catch (error) {
      toastManager.add({
        title: t("toggleFailed", {
          repository: repository.fullName,
          message: messageOf(error, t("actionFailedUnknown")),
        }),
        type: "error",
      });
    }
  }

  return (
    <Switch
      aria-label={t("enabledSwitch", { repository: repository.fullName })}
      checked={repository.enabled}
      disabled={update.isPending}
      onCheckedChange={(checked) => {
        void toggle(checked);
      }}
    />
  );
}

function ActionsCell({
  repository,
  tools,
}: {
  repository: Repository;
  tools: AdminTools;
}): React.ReactElement {
  const t = useTranslations("repositories");
  const run = useRunRepository();
  const [enableOpen, setEnableOpen] = useState(false);
  const [pullRequest, setPullRequest] = useState<EnableRenovateResult | null>(
    null,
  );

  async function runNow(): Promise<void> {
    try {
      await run.mutateAsync(repository.id);
      toastManager.add({
        title: t("runNowQueued", { repository: repository.fullName }),
        description: (
          <Link className="underline underline-offset-4" href="/jobs">
            {t("runNowView")}
          </Link>
        ),
        type: "success",
      });
    } catch (error) {
      toastManager.add({
        title: t("runNowFailed", {
          repository: repository.fullName,
          message: messageOf(error, t("actionFailedUnknown")),
        }),
        type: "error",
      });
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button
        disabled={!repository.enabled}
        loading={run.isPending}
        onClick={() => {
          void runNow();
        }}
        size="sm"
        variant="outline"
      >
        {t("runNow")}
      </Button>
      {repository.configState === "missing" ? (
        pullRequest ? (
          <ExternalLink href={pullRequest.pullRequestUrl}>
            {t("enableRepoLink", { number: pullRequest.number })}
          </ExternalLink>
        ) : (
          <>
            <Button
              onClick={() => setEnableOpen(true)}
              size="sm"
              variant="outline"
            >
              {t("enableRepo")}
            </Button>
            <EnableRepoDialog
              onOpenChange={setEnableOpen}
              onOpened={(result) => {
                setPullRequest(result);
                setEnableOpen(false);
                toastManager.add({
                  title: t("enableRepoOpened", {
                    number: result.number,
                    repository: repository.fullName,
                  }),
                  type: "success",
                });
              }}
              open={enableOpen}
              preset={tools.preset}
              repository={repository}
            />
          </>
        )
      ) : null}
    </div>
  );
}

export function RepositoryTable({
  items,
  tools,
}: {
  items: Repository[];
  /** Null for a viewer, who sees the state and none of the actions. */
  tools: AdminTools | null;
}): React.ReactElement {
  const t = useTranslations("repositories");

  return (
    <Table variant="card">
      <TableHeader>
        <TableRow>
          <TableHead>{t("columns.repository")}</TableHead>
          <TableHead>{t("columns.config")}</TableHead>
          <TableHead>{t("columns.openPrs")}</TableHead>
          <TableHead>{t("columns.lastJob")}</TableHead>
          <TableHead>{t("columns.dashboard")}</TableHead>
          {tools ? (
            <>
              <TableHead>{t("columns.enabled")}</TableHead>
              <TableHead>{t("columns.ticketProject")}</TableHead>
              <TableHead>{t("columns.actions")}</TableHead>
            </>
          ) : null}
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((repository) => (
          <TableRow key={repository.id}>
            <TableCell className="whitespace-nowrap font-medium">
              <ExternalLink href={repository.htmlUrl}>
                {repository.fullName}
              </ExternalLink>
              {!tools && !repository.enabled ? (
                <Badge className="ml-2" variant="secondary">
                  {t("paused")}
                </Badge>
              ) : null}
            </TableCell>
            <TableCell>
              <Badge variant={CONFIG_VARIANTS[repository.configState]}>
                {t(`configStates.${repository.configState}`)}
              </Badge>
            </TableCell>
            <TableCell>{repository.openUpdatePrs}</TableCell>
            <TableCell>
              <LastJobCell repository={repository} />
            </TableCell>
            <TableCell>
              {webUrl(repository.dashboardUrl) ? (
                <ExternalLink href={repository.dashboardUrl}>
                  {t("dashboardLink")}
                </ExternalLink>
              ) : (
                <span className="text-muted-foreground">
                  {t("noDashboard")}
                </span>
              )}
            </TableCell>
            {tools ? (
              <>
                <TableCell>
                  <EnabledCell repository={repository} />
                </TableCell>
                <TableCell>
                  <TicketProjectCell repository={repository} tools={tools} />
                </TableCell>
                <TableCell>
                  <ActionsCell repository={repository} tools={tools} />
                </TableCell>
              </>
            ) : null}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
