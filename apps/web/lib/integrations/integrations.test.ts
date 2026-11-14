import { describe, expect, it } from "vitest";
import { buildIntegrationSecret } from "@/lib/integrations/secret";
import {
  INTEGRATION_KINDS,
  kindIsJira,
  kindRequiresBaseUrl,
  kindUsesApiKeyLabel,
} from "@/lib/integrations/kinds";

describe("integration kinds", () => {
  it("includes only the ticket provider kinds", () => {
    expect(INTEGRATION_KINDS).toEqual(["kaneo", "jira", "linear"]);
  });

  it("requires base URL for Kaneo and Jira", () => {
    expect(kindRequiresBaseUrl("kaneo")).toBe(true);
    expect(kindRequiresBaseUrl("jira")).toBe(true);
    expect(kindRequiresBaseUrl("linear")).toBe(false);
  });

  it("identifies Jira-specific fields", () => {
    expect(kindIsJira("jira")).toBe(true);
    expect(kindIsJira("linear")).toBe(false);
  });

  it("uses API key label for kaneo and linear", () => {
    expect(kindUsesApiKeyLabel("kaneo")).toBe(true);
    expect(kindUsesApiKeyLabel("linear")).toBe(true);
    expect(kindUsesApiKeyLabel("jira")).toBe(false);
  });
});

describe("buildIntegrationSecret", () => {
  it("builds api_key payload for kaneo and linear", () => {
    expect(buildIntegrationSecret("kaneo", "pk_test")).toBe(
      JSON.stringify({ api_key: "pk_test" }),
    );
    expect(buildIntegrationSecret("linear", "lin_test")).toBe(
      JSON.stringify({ api_key: "lin_test" }),
    );
  });

  it("builds Jira email and api_token payload", () => {
    expect(buildIntegrationSecret("jira", "token", "user@example.com")).toBe(
      JSON.stringify({ email: "user@example.com", api_token: "token" }),
    );
  });
});
