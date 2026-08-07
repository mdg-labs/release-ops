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
});
