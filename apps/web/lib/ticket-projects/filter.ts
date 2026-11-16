import type { FilterUpdateType, TicketProjectFilter } from "@/lib/query/types";

/** The update types a filter may list (docs/specs.html §5.2), in display order. */
export const FILTER_UPDATE_TYPES: readonly FilterUpdateType[] = [
  "major",
  "minor",
  "patch",
  "digest",
  "lockfile",
];

/** The filter of a project that configures none: all types, no labels, automerged skipped. */
export function defaultFilter(): TicketProjectFilter {
  return { updateTypes: [], labels: [], skipAutomerged: true };
}

/** Reads a stored filter; a key it lacks has its default. */
export function filterFromRecord(
  record: Partial<TicketProjectFilter> | undefined | null,
): TicketProjectFilter {
  const fallback = defaultFilter();
  if (!record) {
    return fallback;
  }

  return {
    updateTypes: Array.isArray(record.updateTypes)
      ? FILTER_UPDATE_TYPES.filter((type) => record.updateTypes?.includes(type))
      : fallback.updateTypes,
    labels: Array.isArray(record.labels)
      ? record.labels.filter((label) => typeof label === "string")
      : fallback.labels,
    skipAutomerged:
      typeof record.skipAutomerged === "boolean"
        ? record.skipAutomerged
        : fallback.skipAutomerged,
  };
}

/** Splits the comma-separated labels field into trimmed, distinct, non-empty labels. */
export function parseLabels(text: string): string[] {
  const labels: string[] = [];
  for (const part of text.split(",")) {
    const label = part.trim();
    if (label && !labels.includes(label)) {
      labels.push(label);
    }
  }
  return labels;
}

export function formatLabels(labels: readonly string[]): string {
  return labels.join(", ");
}

/**
 * The labels to save. The field is comma-separated (docs/specs.html §8), so a stored label that
 * holds a comma cannot survive a parse; while the text is what formatLabels gave for the stored
 * labels they are kept as they were, and only an edited field is parsed.
 */
export function labelsToSave(
  stored: readonly string[],
  labelsText: string,
): string[] {
  return labelsText === formatLabels(stored)
    ? [...stored]
    : parseLabels(labelsText);
}

/** True when the filter is the default: every non-automerged PR gets a ticket. */
export function isDefaultFilter(filter: TicketProjectFilter): boolean {
  return (
    filter.updateTypes.length === 0 &&
    filter.labels.length === 0 &&
    filter.skipAutomerged
  );
}
