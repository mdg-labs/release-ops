export const NOTIFICATION_EVENT_OPTIONS = [
  "create",
  "error",
  "supersede",
] as const;

export type NotificationEvent = (typeof NOTIFICATION_EVENT_OPTIONS)[number];

export const DEFAULT_NOTIFICATION_EVENTS: NotificationEvent[] = [
  "create",
  "error",
  "supersede",
];
