import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { DELETE, GET, PATCH, POST } from "./route";

const GO_ORIGIN = "http://127.0.0.1:8080";

function routeContext(path: string[]) {
  return { params: Promise.resolve({ path }) };
}

function makeRequest(
  url: string,
  options?: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
  },
) {
  return new NextRequest(url, {
    method: options?.method,
    headers: options?.headers,
    body: options?.body,
  });
}

describe("Go API proxy route", () => {
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

  it("forwards GET to upstream and returns JSON", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({
        path: "/api/v1/auth/session",
        method: "GET",
      })
      .reply(
        200,
        { user: null },
        {
          headers: { "content-type": "application/json" },
        },
      );

    const response = await GET(
      makeRequest("http://localhost:3000/api/go/api/v1/auth/session"),
      routeContext(["api", "v1", "auth", "session"]),
    );

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({ user: null });
  });

  // Metrics are served on METRICS_ADDR by their own listener; the Go API router has no
  // /metrics route (internal/api TestRouterServesReadyzAndNoMetrics), so the proxy returns
  // the 404 it gets and a scraper path is never reachable through /api/go.
  it("answers 404 for /api/go/metrics because the Go API serves no metrics route", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({ path: "/metrics", method: "GET" })
      .reply(404, "404 page not found\n", {
        headers: { "content-type": "text/plain; charset=utf-8" },
      });

    const response = await GET(
      makeRequest("http://localhost:3000/api/go/metrics"),
      routeContext(["metrics"]),
    );

    expect(response.status).toBe(404);
    expect(await response.text()).not.toContain("release_ops_");
  });

  it("forwards the preload-set X-Forwarded-For to upstream for per-IP rate limits", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({
        path: "/api/v1/auth/login",
        method: "POST",
        headers: { "x-forwarded-for": "198.51.100.42" },
      })
      .reply(200, { user: null });

    const response = await POST(
      makeRequest("http://localhost:3000/api/go/api/v1/auth/login", {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "x-forwarded-for": "198.51.100.42",
        },
        body: "{}",
      }),
      routeContext(["api", "v1", "auth", "login"]),
    );

    expect(response.status).toBe(200);
  });

  it("forwards Cookie header to upstream", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({
        path: "/api/v1/status",
        method: "GET",
        headers: { cookie: "release_ops_session=test-token" },
      })
      .reply(200, { ok: true });

    const response = await GET(
      makeRequest("http://localhost:3000/api/go/api/v1/status", {
        headers: { cookie: "release_ops_session=test-token" },
      }),
      routeContext(["api", "v1", "status"]),
    );

    expect(response.status).toBe(200);
  });

  it("forwards POST with JSON body for auth login", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({
        path: "/api/v1/auth/login",
        method: "POST",
      })
      .reply(
        200,
        { user: { id: "user-1", email: "admin@example.com" } },
        {
          headers: {
            "content-type": "application/json",
            "set-cookie":
              "release_ops_session=abc; Path=/; HttpOnly; SameSite=Lax",
          },
        },
      );

    const response = await POST(
      makeRequest("http://localhost:3000/api/go/api/v1/auth/login", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          email: "admin@example.com",
          password: "secret",
        }),
      }),
      routeContext(["api", "v1", "auth", "login"]),
    );

    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      user: { id: "user-1", email: "admin@example.com" },
    });
    expect(response.headers.get("set-cookie")).toContain(
      "release_ops_session=abc",
    );
  });

  it("forwards PATCH and DELETE methods", async () => {
    const pool = mockAgent.get(GO_ORIGIN);

    pool
      .intercept({
        path: "/api/v1/settings",
        method: "PATCH",
      })
      .reply(200, { pollIntervalMinutes: 60 });

    const patchResponse = await PATCH(
      makeRequest("http://localhost:3000/api/go/api/v1/settings", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ pollIntervalMinutes: 60 }),
      }),
      routeContext(["api", "v1", "settings"]),
    );
    expect(patchResponse.status).toBe(200);

    pool
      .intercept({
        path: "/api/v1/repos/repo-1",
        method: "DELETE",
      })
      .reply(204);

    const deleteResponse = await DELETE(
      makeRequest("http://localhost:3000/api/go/api/v1/repos/repo-1", {
        method: "DELETE",
      }),
      routeContext(["api", "v1", "repos", "repo-1"]),
    );
    expect(deleteResponse.status).toBe(204);
  });

  it("preserves query string on upstream URL", async () => {
    const pool = mockAgent.get(GO_ORIGIN);
    pool
      .intercept({
        path: "/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10" },
      })
      .reply(200, { runs: [] });

    const response = await GET(
      makeRequest("http://localhost:3000/api/go/api/v1/poll/runs?limit=10"),
      routeContext(["api", "v1", "poll", "runs"]),
    );

    expect(response.status).toBe(200);
  });

  it("returns 502 when upstream is unreachable", async () => {
    vi.stubEnv("GO_API_URL", "http://127.0.0.1:59999");

    const response = await GET(
      makeRequest("http://localhost:3000/api/go/healthz"),
      routeContext(["healthz"]),
    );

    expect(response.status).toBe(502);
    const body = await response.json();
    expect(body.error.code).toBe("UPSTREAM_ERROR");
  });

  describe("upstream redirects", () => {
    function replyRedirect(
      location: string,
      status = 302,
      extra: Record<string, string | string[]> = {},
    ) {
      mockAgent
        .get(GO_ORIGIN)
        .intercept({ path: "/api/v1/setup/callback", method: "GET" })
        .reply(status, "", { headers: { location, ...extra } });
    }

    function callRedirecting() {
      return GET(
        makeRequest("http://localhost:3000/api/go/api/v1/setup/callback"),
        routeContext(["api", "v1", "setup", "callback"]),
      );
    }

    it.each([301, 302, 303, 307, 308])(
      "returns an upstream %i to the browser with its Location",
      async (status) => {
        replyRedirect("/login", status);

        const response = await callRedirecting();

        expect(response.status).toBe(status);
        expect(response.headers.get("location")).toBe("/login");
      },
    );

    it("does not fetch the redirect target itself", async () => {
      replyRedirect("/login");
      const fetchSpy = vi.spyOn(globalThis, "fetch");

      await callRedirecting();

      expect(fetchSpy).toHaveBeenCalledTimes(1);
      expect(fetchSpy.mock.calls[0][1]).toMatchObject({ redirect: "manual" });
      fetchSpy.mockRestore();
    });

    it("passes a redirect to the GitHub App installation page", async () => {
      replyRedirect("https://github.com/apps/x/installations/new");

      const response = await callRedirecting();

      expect(response.status).toBe(302);
      expect(response.headers.get("location")).toBe(
        "https://github.com/apps/x/installations/new",
      );
    });

    it("passes a redirect to the host of GITHUB_BASE_URL", async () => {
      vi.stubEnv("GITHUB_BASE_URL", "https://ghe.example.com");
      replyRedirect("https://ghe.example.com/apps/x/installations/new");

      const response = await callRedirecting();

      expect(response.status).toBe(302);
      expect(response.headers.get("location")).toBe(
        "https://ghe.example.com/apps/x/installations/new",
      );
    });

    it.each([
      "https://evil.example.com/phish",
      "https://ghe.example.com/apps/x",
      "http://github.com/apps/x",
      "https://github.com@evil.example.com/",
      "//evil.example.com/",
      "/\\evil.example.com/",
      "login",
    ])("refuses a redirect to %s with a 502 and no Location", async (loc) => {
      replyRedirect(loc);

      const response = await callRedirecting();

      expect(response.status).toBe(502);
      expect(response.headers.get("location")).toBeNull();
    });

    it("still forwards Set-Cookie on a redirect response", async () => {
      replyRedirect("/login", 302, {
        "set-cookie": ["a=1; Path=/; HttpOnly", "b=2; Path=/"],
      });

      const response = await callRedirecting();

      expect(response.status).toBe(302);
      expect(response.headers.get("location")).toBe("/login");
      expect(response.headers.getSetCookie()).toEqual([
        "a=1; Path=/; HttpOnly",
        "b=2; Path=/",
      ]);
    });

    it("leaves a non-redirect 200 unchanged and drops a stray Location", async () => {
      mockAgent
        .get(GO_ORIGIN)
        .intercept({ path: "/api/v1/status", method: "GET" })
        .reply(
          200,
          { ok: true },
          {
            headers: {
              "content-type": "application/json",
              location: "https://evil.example.com/",
            },
          },
        );

      const response = await GET(
        makeRequest("http://localhost:3000/api/go/api/v1/status"),
        routeContext(["api", "v1", "status"]),
      );

      expect(response.status).toBe(200);
      expect(await response.json()).toEqual({ ok: true });
      expect(response.headers.get("location")).toBeNull();
    });
  });

  describe("GitHub webhook path", () => {
    const WEBHOOK_URL = "http://localhost:3000/api/go/hooks/github";
    const SIGNATURE = `sha256=${"ab".repeat(32)}`;
    const MAX_BODY = 1024 * 1024;

    type Upstream = {
      headers: Record<string, string | string[] | undefined>;
      body: string;
    };

    function interceptUpstream(path: string, status = 202) {
      const seen: Upstream[] = [];
      mockAgent
        .get(GO_ORIGIN)
        .intercept({ path, method: "POST" })
        .reply(status, (opts) => {
          seen.push({
            headers: opts.headers as Upstream["headers"],
            body:
              opts.body instanceof Uint8Array
                ? new TextDecoder().decode(opts.body)
                : String(opts.body ?? ""),
          });
          return "";
        });
      return seen;
    }

    function hookHeaders(extra: Record<string, string> = {}) {
      return {
        "content-type": "application/json",
        "x-hub-signature-256": SIGNATURE,
        "x-github-event": "installation",
        "x-github-delivery": "72d3162e-cc78-11e3-81ab-4c9367dc0958",
        ...extra,
      };
    }

    it("forwards the signature, event and delivery headers and the exact body bytes", async () => {
      const seen = interceptUpstream("/hooks/github");
      const body = '{"action":"created",  "installation":{"id":1000}}\n';

      const response = await POST(
        makeRequest(WEBHOOK_URL, {
          method: "POST",
          headers: hookHeaders(),
          body,
        }),
        routeContext(["hooks", "github"]),
      );

      expect(response.status).toBe(202);
      expect(seen).toHaveLength(1);
      expect(seen[0].headers["x-hub-signature-256"]).toBe(SIGNATURE);
      expect(seen[0].headers["x-github-event"]).toBe("installation");
      expect(seen[0].headers["x-github-delivery"]).toBe(
        "72d3162e-cc78-11e3-81ab-4c9367dc0958",
      );
      expect(seen[0].body).toBe(body);
    });

    it("does not forward a session cookie or authorization header on the webhook path", async () => {
      const seen = interceptUpstream("/hooks/github");

      await POST(
        makeRequest(WEBHOOK_URL, {
          method: "POST",
          headers: hookHeaders({
            cookie: "release_ops_session=test-token",
            authorization: "Bearer x",
          }),
          body: "{}",
        }),
        routeContext(["hooks", "github"]),
      );

      expect(seen[0].headers["cookie"]).toBeUndefined();
      expect(seen[0].headers["authorization"]).toBeUndefined();
    });

    it("forwards the webhook headers on no other path", async () => {
      const seen = interceptUpstream("/api/v1/auth/login", 200);

      await POST(
        makeRequest("http://localhost:3000/api/go/api/v1/auth/login", {
          method: "POST",
          headers: hookHeaders(),
          body: "{}",
        }),
        routeContext(["api", "v1", "auth", "login"]),
      );

      expect(seen).toHaveLength(1);
      expect(seen[0].headers["x-hub-signature-256"]).toBeUndefined();
      expect(seen[0].headers["x-github-event"]).toBeUndefined();
      expect(seen[0].headers["x-github-delivery"]).toBeUndefined();
    });

    it("rejects a Content-Length over the cap with 413 before reading the body or calling Go", async () => {
      const seen = interceptUpstream("/hooks/github");
      let bodyRead = false;
      const request = makeRequest(WEBHOOK_URL, {
        method: "POST",
        headers: hookHeaders({ "content-length": String(MAX_BODY + 1) }),
        body: "{}",
      });
      Object.defineProperty(request, "arrayBuffer", {
        value: () => {
          bodyRead = true;
          return Promise.resolve(new ArrayBuffer(0));
        },
      });
      Object.defineProperty(request, "body", {
        get: () => {
          bodyRead = true;
          return null;
        },
      });

      const response = await POST(request, routeContext(["hooks", "github"]));

      expect(response.status).toBe(413);
      expect(bodyRead).toBe(false);
      expect(seen).toHaveLength(0);
    });

    it("cuts off a body over the cap that declares no length", async () => {
      const seen = interceptUpstream("/hooks/github");
      const oversized = new Uint8Array(MAX_BODY + 1).fill(97);
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(oversized.slice(0, MAX_BODY / 2));
          controller.enqueue(oversized.slice(MAX_BODY / 2));
          controller.close();
        },
      });
      const request = new NextRequest(
        new Request(WEBHOOK_URL, {
          method: "POST",
          headers: hookHeaders(),
          body: stream,
          duplex: "half",
        } as RequestInit),
      );

      const response = await POST(request, routeContext(["hooks", "github"]));

      expect(response.status).toBe(413);
      expect(seen).toHaveLength(0);
    });

    it("forwards a body exactly at the cap", async () => {
      const seen = interceptUpstream("/hooks/github");

      const response = await POST(
        makeRequest(WEBHOOK_URL, {
          method: "POST",
          headers: hookHeaders(),
          body: "a".repeat(MAX_BODY),
        }),
        routeContext(["hooks", "github"]),
      );

      expect(response.status).toBe(202);
      expect(seen[0].body).toHaveLength(MAX_BODY);
    });

    it("passes Go's refusal of a bad signature through", async () => {
      interceptUpstream("/hooks/github", 401);

      const response = await POST(
        makeRequest(WEBHOOK_URL, {
          method: "POST",
          headers: hookHeaders(),
          body: "{}",
        }),
        routeContext(["hooks", "github"]),
      );

      expect(response.status).toBe(401);
    });
  });
});
