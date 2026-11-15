import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import nextConfig from "./next.config";
import { POST } from "./app/api/go/[...path]/route";

const GO_ORIGIN = "http://127.0.0.1:8080";
const ORIGIN = "http://localhost:3000";

type Rewrite = { source: string; destination: string };

async function webhookRewrite(): Promise<Rewrite | undefined> {
  const rewrites = await nextConfig.rewrites?.();
  const rules: Rewrite[] = Array.isArray(rewrites)
    ? rewrites
    : (rewrites?.beforeFiles ?? []).concat(
        rewrites?.afterFiles ?? [],
        rewrites?.fallback ?? [],
      );
  return rules.find((rule) => rule.source === "/hooks/github");
}

describe("/hooks/github on the Next listener", () => {
  let mockAgent: MockAgent;

  beforeEach(() => {
    vi.stubEnv("GO_API_URL", GO_ORIGIN);
    mockAgent = new MockAgent();
    setGlobalDispatcher(mockAgent);
    mockAgent.disableNetConnect();
  });

  afterEach(async () => {
    await mockAgent.close();
    vi.unstubAllEnvs();
  });

  it("rewrites to the Go API proxy, which delivers a signed installation event to Go", async () => {
    const rule = await webhookRewrite();
    expect(rule).toBeDefined();
    const destination = new URL(rule!.destination, ORIGIN);
    expect(destination.pathname).toBe("/api/go/hooks/github");

    const body = '{"action":"created","installation":{"id":1000}}';
    const signature = `sha256=${"cd".repeat(32)}`;
    let seen: { headers: Record<string, unknown>; body: string } | undefined;
    mockAgent
      .get(GO_ORIGIN)
      .intercept({ path: "/hooks/github", method: "POST" })
      .reply(202, (opts) => {
        seen = {
          headers: opts.headers as Record<string, unknown>,
          body: new TextDecoder().decode(opts.body as Uint8Array),
        };
        return "";
      });

    const response = await POST(
      new NextRequest(destination, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "x-hub-signature-256": signature,
          "x-github-event": "installation",
          "x-github-delivery": "72d3162e-cc78-11e3-81ab-4c9367dc0958",
        },
        body,
      }),
      {
        params: Promise.resolve({
          path: destination.pathname.replace("/api/go/", "").split("/"),
        }),
      },
    );

    expect(response.status).toBe(202);
    expect(seen?.body).toBe(body);
    expect(seen?.headers["x-hub-signature-256"]).toBe(signature);
    expect(seen?.headers["x-github-event"]).toBe("installation");
  });

  it("rewrites no other path", async () => {
    const rewrites = await nextConfig.rewrites?.();
    const rules = Array.isArray(rewrites) ? rewrites : [];
    expect(rules.map((rule) => rule.source)).toEqual(["/hooks/github"]);
  });
});
