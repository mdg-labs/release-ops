import { describe, expect, it } from "vitest";
import { ROUTE_BREADCRUMBS } from "@/components/app-breadcrumb";
import messages from "@/messages/en.json";

describe("ROUTE_BREADCRUMBS", () => {
  it("only references keys that exist in the nav messages", () => {
    const keys = Object.values(ROUTE_BREADCRUMBS)
      .flat()
      .map((segment) => segment.labelKey);

    for (const key of keys) {
      expect(messages.nav, key).toHaveProperty(key);
    }
  });

  it("labels the repositories page with the Repositories entry, not the dashboard", () => {
    expect(ROUTE_BREADCRUMBS["/repositories"]).toEqual([
      { labelKey: "repositories" },
    ]);
    expect(messages.nav.repositories).toBe("Repositories");
  });

  it("labels the about page with the About entry", () => {
    expect(ROUTE_BREADCRUMBS["/about"]).toEqual([{ labelKey: "about" }]);
    expect(messages.nav.about).toBe("About");
  });
});
