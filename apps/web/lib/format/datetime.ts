import type { useFormatter } from "next-intl";

type Formatter = ReturnType<typeof useFormatter>;

export const APP_DATE_TIME_OPTIONS = {
  dateStyle: "medium",
  timeStyle: "short",
  hour12: false,
} as const satisfies Intl.DateTimeFormatOptions;

export const POLL_DIAGNOSTIC_DATE_TIME_OPTIONS = {
  year: "numeric",
  month: "short",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  fractionalSecondDigits: 3,
  hour12: false,
} as const satisfies Intl.DateTimeFormatOptions;

function toDate(value: string | Date): Date {
  return value instanceof Date ? value : new Date(value);
}

export function formatAppDateTime(
  formatter: Formatter,
  value: string | Date | null | undefined,
  fallback: string,
): string {
  if (!value) {
    return fallback;
  }

  return formatter.dateTime(toDate(value), APP_DATE_TIME_OPTIONS);
}

export function formatPollDiagnosticDateTime(
  formatter: Formatter,
  value: string | Date | null | undefined,
  fallback: string,
): string {
  if (!value) {
    return fallback;
  }

  return formatter.dateTime(toDate(value), POLL_DIAGNOSTIC_DATE_TIME_OPTIONS);
}
