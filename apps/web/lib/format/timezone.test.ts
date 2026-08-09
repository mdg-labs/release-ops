import { afterEach, describe, expect, it } from "vitest";
import { getAppTimeZone } from "./timezone";

describe("getAppTimeZone", () => {
  const original = process.env["APP_TIMEZONE"];

  afterEach(() => {
    if (original === undefined) {
      delete process.env["APP_TIMEZONE"];
    } else {
      process.env["APP_TIMEZONE"] = original;
    }
  });

  it("defaults to UTC when unset", () => {
    delete process.env["APP_TIMEZONE"];
    expect(getAppTimeZone()).toBe("UTC");
  });

  it("returns a valid IANA timezone from APP_TIMEZONE", () => {
    process.env["APP_TIMEZONE"] = "Europe/Vienna";
    expect(getAppTimeZone()).toBe("Europe/Vienna");
  });

  it("falls back to UTC for invalid IANA values", () => {
    process.env["APP_TIMEZONE"] = "Not/A_Timezone";
    expect(getAppTimeZone()).toBe("UTC");
  });
});
