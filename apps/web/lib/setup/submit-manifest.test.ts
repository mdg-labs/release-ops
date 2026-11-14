import { afterEach, describe, expect, it, vi } from "vitest";
import { submitManifestForm } from "./submit-manifest";

describe("submitManifestForm", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  it("posts the manifest as a JSON string in the manifest field", () => {
    const submit = vi
      .spyOn(HTMLFormElement.prototype, "submit")
      .mockImplementation(() => undefined);

    submitManifestForm("https://github.com/settings/apps/new?state=abc", {
      name: "Release Ops",
      public: false,
    });

    expect(submit).toHaveBeenCalledTimes(1);
    const form = document.querySelector("form");
    expect(form?.method).toBe("post");
    expect(form?.getAttribute("action")).toBe(
      "https://github.com/settings/apps/new?state=abc",
    );
    const field = form?.querySelector<HTMLInputElement>("input[name=manifest]");
    expect(JSON.parse(field?.value ?? "")).toEqual({
      name: "Release Ops",
      public: false,
    });
  });
});
