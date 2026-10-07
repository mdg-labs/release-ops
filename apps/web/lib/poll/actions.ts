export const POLL_EVENT_ACTIONS = [
  "baseline",
  "skip",
  "create",
  "supersede",
  "merge",
  "skip_open",
  "error",
] as const;

export type PollEventAction = (typeof POLL_EVENT_ACTIONS)[number];

export function isPollEventAction(value: string): value is PollEventAction {
  return (POLL_EVENT_ACTIONS as readonly string[]).includes(value);
}
