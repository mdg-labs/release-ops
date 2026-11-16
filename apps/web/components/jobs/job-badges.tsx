"use client";

import { useTranslations } from "next-intl";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import type { JobRunOutcome, JobState } from "@/lib/query/types";

const STATE_VARIANTS: Record<JobState, BadgeProps["variant"]> = {
  queued: "info",
  running: "info",
  succeeded: "success",
  failed: "error",
};

const OUTCOME_VARIANTS: Record<JobRunOutcome, BadgeProps["variant"]> = {
  running: "info",
  succeeded: "success",
  no_config: "warning",
  failed: "error",
  aborted: "secondary",
  lease_expired: "warning",
};

export function JobStateBadge({
  state,
}: {
  state: JobState;
}): React.ReactElement {
  const t = useTranslations("jobs");
  const active = state === "queued" || state === "running";
  return (
    <Badge variant={STATE_VARIANTS[state]}>
      {active ? <Spinner className="size-3" /> : null}
      {t(`states.${state}`)}
    </Badge>
  );
}

export function RunOutcomeBadge({
  outcome,
}: {
  outcome: JobRunOutcome;
}): React.ReactElement {
  const t = useTranslations("jobs");
  return (
    <Badge variant={OUTCOME_VARIANTS[outcome]}>
      {t(`outcomes.${outcome}`)}
    </Badge>
  );
}
