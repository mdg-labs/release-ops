import { describe, expect, it } from "vitest";
import {
  defaultFilter,
  filterFromRecord,
  formatLabels,
  isDefaultFilter,
  labelsToSave,
  parseLabels,
} from "@/lib/ticket-projects/filter";

describe("ticket project filter", () => {
  it("defaults to every update type, no labels and automerged skipped", () => {
    expect(defaultFilter()).toEqual({
      updateTypes: [],
      labels: [],
      skipAutomerged: true,
    });
    expect(isDefaultFilter(defaultFilter())).toBe(true);
    expect(filterFromRecord(undefined)).toEqual(defaultFilter());
    expect(filterFromRecord({})).toEqual(defaultFilter());
  });

  it("reads a stored filter and drops unknown update types", () => {
    const filter = filterFromRecord({
      updateTypes: ["patch", "major", "other" as never],
      labels: ["deps"],
      skipAutomerged: false,
    });
    expect(filter).toEqual({
      updateTypes: ["major", "patch"],
      labels: ["deps"],
      skipAutomerged: false,
    });
    expect(isDefaultFilter(filter)).toBe(false);
  });

  it("parses the comma-separated labels into trimmed, distinct labels", () => {
    expect(parseLabels(" deps, renovate ,, deps,security ")).toEqual([
      "deps",
      "renovate",
      "security",
    ]);
    expect(parseLabels("")).toEqual([]);
  });

  it("keeps stored labels as they were while the labels field is untouched", () => {
    const stored = ["needs, review", "deps"];
    expect(labelsToSave(stored, formatLabels(stored))).toEqual(stored);
    expect(labelsToSave([], "")).toEqual([]);
  });

  it("parses the labels field once it was edited", () => {
    const stored = ["needs, review", "deps"];
    expect(labelsToSave(stored, "needs, review, deps, security")).toEqual([
      "needs",
      "review",
      "deps",
      "security",
    ]);
    expect(labelsToSave(stored, "")).toEqual([]);
  });
});
