import { describe, expect, it } from "vitest";
import { buildIntegrationSecret } from "@/lib/integrations/secret";
import {
  INTEGRATION_KINDS,
  kindIsJira,
  kindRequiresBaseUrl,
  kindUsesApiKeyLabel,
} from "@/lib/integrations/kinds";

describe("integration kinds", () => {
  it("includes all eight provider kinds", () => {
    expect(INTEGRATION_KINDS).toHaveLength(8);
    expect(INTEGRATION_KINDS).toEqual([
      "github",
      "gitlab",
      "gitea",
      "forgejo",
      "codeberg",
      "phasical",
      "jira",
      "linear",
    ]);
  });

  it("requires base URL for self-hosted and ticket providers", () => {
    expect(kindRequiresBaseUrl("gitlab")).toBe(true);
    expect(kindRequiresBaseUrl("jira")).toBe(true);
    expect(kindRequiresBaseUrl("github")).toBe(false);
    expect(kindRequiresBaseUrl("linear")).toBe(false);
  });

  it("identifies Jira-specific fields", () => {
    expect(kindIsJira("jira")).toBe(true);
    expect(kindIsJira("linear")).toBe(false);
  });

  it("uses API key label for phasical and linear", () => {
    expect(kindUsesApiKeyLabel("phasical")).toBe(true);
    expect(kindUsesApiKeyLabel("linear")).toBe(true);
    expect(kindUsesApiKeyLabel("github")).toBe(false);
  });
});

describe("buildIntegrationSecret", () => {
  it("builds token payload for source providers", () => {
    expect(buildIntegrationSecret("github", "ghp_test")).toBe(
      JSON.stringify({ token: "ghp_test" }),
    );
  });

  it("builds api_key payload for phasical and linear", () => {
    expect(buildIntegrationSecret("phasical", "pk_test")).toBe(
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
