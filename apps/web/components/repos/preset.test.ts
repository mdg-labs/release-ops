import { describe, expect, it } from "vitest";
import { enableRepoPreset } from "./preset";

describe("enableRepoPreset", () => {
  it("is the first preset repository as a local preset", () => {
    expect(enableRepoPreset(["acme/presets", "acme/other"])).toBe(
      "local>acme/presets",
    );
  });

  it("is config:recommended when no preset repository is configured", () => {
    expect(enableRepoPreset([])).toBe("config:recommended");
  });
});
