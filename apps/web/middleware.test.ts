import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MIDDLEWARE_MATCHER_PATTERN,
  SESSION_API_PATH,
  getSessionUser,
  isLoginPath,
  middleware,
} from "./middleware";

const ORIGIN = "http://localhost:3000";

function makeRequest(path: string, cookie?: string): NextRequest {
  return new NextRequest(`${ORIGIN}${path}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

function matcherRegex(): RegExp {
  return new RegExp(`^${MIDDLEWARE_MATCHER_PATTERN}$`);
}

describe("auth middleware", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("isLoginPath", () => {
    it("matches /login and nested login paths", () => {
      expect(isLoginPath("/login")).toBe(true);
      expect(isLoginPath("/login/reset")).toBe(true);
      expect(isLoginPath("/repos")).toBe(false);
    });
  });

  describe("getSessionUser", () => {
    it("calls GET /api/go/api/v1/auth/session with forwarded cookies", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({
          user: { id: "user-1", email: "admin@example.com" },
        }),
      });
      vi.stubGlobal("fetch", fetchMock);

      const request = makeRequest(
        "/repos",
        "release_ops_session=session-token",
      );
      const user = await getSessionUser(request);

      expect(user).toEqual({
        id: "user-1",
        email: "admin@example.com",
      });
      expect(fetchMock).toHaveBeenCalledOnce();
      const [url, init] = fetchMock.mock.calls[0] as [URL, RequestInit];
      expect(url.href).toBe(`${ORIGIN}${SESSION_API_PATH}`);
      expect(init.cache).toBe("no-store");
      const headers = new Headers(init.headers);
      expect(headers.get("cookie")).toBe("release_ops_session=session-token");
      expect(headers.get("accept")).toBe("application/json");
    });

    it("returns null when session endpoint is not ok", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
        }),
      );

      const user = await getSessionUser(makeRequest("/repos"));
      expect(user).toBeNull();
    });
  });

  describe("middleware", () => {
    it("allows /login without a session", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ user: null }),
        }),
      );

      const response = await middleware(makeRequest("/login"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });

    it("redirects protected pages to /login when user is null", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({ user: null }),
        }),
      );

      const response = await middleware(makeRequest("/repos"));
      expect(response.status).toBe(307);
      expect(response.headers.get("location")).toBe(
        `${ORIGIN}/login?redirect=%2Frepos`,
      );
    });

    it("redirects authenticated users away from /login", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({
            user: { id: "user-1", email: "admin@example.com" },
          }),
        }),
      );

      const response = await middleware(makeRequest("/login"));
      expect(response.status).toBe(307);
      expect(response.headers.get("location")).toBe(`${ORIGIN}/`);
    });

    it("allows authenticated access to protected pages", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({
            user: { id: "user-1", email: "admin@example.com" },
          }),
        }),
      );

      const response = await middleware(makeRequest("/settings"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });
  });

  describe("matcher config", () => {
    const regex = matcherRegex();

    it("matches app routes that require auth checks", () => {
      expect(regex.test("/")).toBe(true);
      expect(regex.test("/login")).toBe(true);
      expect(regex.test("/repos")).toBe(true);
      expect(regex.test("/settings")).toBe(true);
    });

    it("excludes api routes and static assets", () => {
      expect(regex.test("/api/go/api/v1/auth/session")).toBe(false);
      expect(regex.test("/_next/static/chunks/main.js")).toBe(false);
      expect(regex.test("/_next/image?url=%2Flogo.png")).toBe(false);
      expect(regex.test("/favicon.ico")).toBe(false);
      expect(regex.test("/logo.png")).toBe(false);
      expect(regex.test("/sitemap.xml")).toBe(false);
      expect(regex.test("/robots.txt")).toBe(false);
    });
  });
});
