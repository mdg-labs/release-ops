"use client";

import { useFormatter, useTranslations } from "next-intl";
import { formatAppDateTime } from "@/lib/format/datetime";

/** A stored UTC timestamp, or the placeholder while the event has not happened. */
export function JobTime({
  value,
  placeholder,
}: {
  value: string | null;
  placeholder?: string;
}): React.ReactElement {
  const format = useFormatter();
  if (!value) {
    return <span className="text-muted-foreground">{placeholder ?? ""}</span>;
  }
  return <time dateTime={value}>{formatAppDateTime(format, value, "")}</time>;
}

/** A length of time in whole units: milliseconds under a second, then seconds, minutes, hours. */
export function DurationText({ ms }: { ms: number }): React.ReactElement {
  const t = useTranslations("jobs.duration");
  if (ms < 1000) {
    return <>{t("milliseconds", { ms: Math.max(0, Math.round(ms)) })}</>;
  }
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) {
    return <>{t("seconds", { seconds })}</>;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return <>{t("minutes", { minutes, seconds: seconds % 60 })}</>;
  }
  return (
    <>
      {t("hours", { hours: Math.floor(minutes / 60), minutes: minutes % 60 })}
    </>
  );
}

/** The time between two stored timestamps, or null while either is missing or unreadable. */
export function elapsedMs(
  start: string | null,
  end: string | null,
): number | null {
  if (!start || !end) {
    return null;
  }
  const ms = new Date(end).getTime() - new Date(start).getTime();
  return Number.isFinite(ms) ? Math.max(0, ms) : null;
}
