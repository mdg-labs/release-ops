const DEFAULT_TIME_ZONE = "UTC";

export function getAppTimeZone(): string {
  const configured = process.env.APP_TIMEZONE?.trim();

  if (!configured) {
    return DEFAULT_TIME_ZONE;
  }

  try {
    Intl.DateTimeFormat(undefined, { timeZone: configured });
    return configured;
  } catch {
    return DEFAULT_TIME_ZONE;
  }
}
