import { describe, expect, it } from "vitest";
import { safeRedirectPath } from "./safe-redirect";

describe("safeRedirectPath", () => {
  it("keeps same-origin paths with query and hash", () => {
    expect(safeRedirectPath("/repos")).toBe("/repos");
    expect(safeRedirectPath("/repos?page=2#top")).toBe("/repos?page=2#top");
  });

  it("falls back to / for missing values", () => {
    expect(safeRedirectPath(null)).toBe("/");
    expect(safeRedirectPath(undefined)).toBe("/");
    expect(safeRedirectPath("")).toBe("/");
  });

  it.each([
    "https://evil.example/login",
    "//evil.example",
    "/\\evil.example",
    "\\\\evil.example",
    "javascript:alert(1)",
    "evil.example",
  ])("rejects off-site redirect %s", (value) => {
    expect(safeRedirectPath(value)).toBe("/");
  });
});
