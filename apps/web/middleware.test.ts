import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MIDDLEWARE_MATCHER_PATTERN,
  PUBLIC_AUTH_PATHS,
  getSessionUser,
  isLoginPath,
  isPublicAuthPath,
  middleware,
} from "./middleware";

const ORIGIN = "http://localhost:3000";
const GO_SESSION_URL = "http://127.0.0.1:8080/api/v1/auth/session";

function makeRequest(path: string, cookie?: string): NextRequest {
  return new NextRequest(`${ORIGIN}${path}`, {
    headers: cookie ? { cookie } : undefined,
  });
}

function matcherRegex(): RegExp {
  return new RegExp(`^${MIDDLEWARE_MATCHER_PATTERN}$`);
}

function mockNoSession(): void {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({ user: null }),
    }),
  );
}

describe("auth middleware", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  describe("isPublicAuthPath", () => {
    it("matches all public auth routes", () => {
      for (const path of PUBLIC_AUTH_PATHS) {
        expect(isPublicAuthPath(path)).toBe(true);
      }
      expect(isPublicAuthPath("/accept-invitation/extra")).toBe(true);
    });

    it("does not match protected routes", () => {
      expect(isPublicAuthPath("/repos")).toBe(false);
      expect(isPublicAuthPath("/settings")).toBe(false);
    });
  });

  describe("isLoginPath", () => {
    it("matches /login and nested login paths", () => {
      expect(isLoginPath("/login")).toBe(true);
      expect(isLoginPath("/login/reset")).toBe(true);
      expect(isLoginPath("/repos")).toBe(false);
    });
  });

  describe("getSessionUser", () => {
    it("calls GET /api/v1/auth/session on the Go API with forwarded cookies", async () => {
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
      const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
      expect(url).toBe(GO_SESSION_URL);
      expect(init.cache).toBe("no-store");
      const headers = new Headers(init.headers);
      expect(headers.get("cookie")).toBe("release_ops_session=session-token");
      expect(headers.get("accept")).toBe("application/json");
    });

    it("returns null when the session endpoint is not ok", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: false,
        }),
      );

      const user = await getSessionUser(makeRequest("/repos"));
      expect(user).toBeNull();
    });

    it("returns null when the session check cannot reach the Go API", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockRejectedValue(new TypeError("fetch failed")),
      );

      const user = await getSessionUser(makeRequest("/repos"));
      expect(user).toBeNull();
    });

    it("uses GO_API_URL when set", async () => {
      vi.stubEnv("GO_API_URL", "http://127.0.0.1:9090");
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ user: null }),
      });
      vi.stubGlobal("fetch", fetchMock);

      await getSessionUser(makeRequest("/repos"));

      expect(fetchMock).toHaveBeenCalledWith(
        "http://127.0.0.1:9090/api/v1/auth/session",
        expect.any(Object),
      );
    });
  });

  describe("middleware", () => {
    it("allows /login without a session", async () => {
      mockNoSession();

      const response = await middleware(makeRequest("/login"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });

    it("allows public auth routes without a session", async () => {
      mockNoSession();

      const paths = [
        "/accept-invitation?token=abc",
        "/forgot-password",
        "/reset-password?token=abc",
        "/confirm-email-change?token=abc",
      ];

      for (const path of paths) {
        const response = await middleware(makeRequest(path));
        expect(response.status).toBe(200);
        expect(response.headers.get("location")).toBeNull();
      }
    });

    it("redirects protected pages to /login when user is null", async () => {
      mockNoSession();

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

    it("allows authenticated users on other public auth routes", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({
            user: { id: "user-1", email: "admin@example.com" },
          }),
        }),
      );

      const response = await middleware(makeRequest("/forgot-password"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });
  });

  describe("matcher config", () => {
    const regex = matcherRegex();

    it("matches app routes that require auth checks", () => {
      expect(regex.test("/")).toBe(true);
      expect(regex.test("/login")).toBe(true);
      expect(regex.test("/accept-invitation")).toBe(true);
      expect(regex.test("/forgot-password")).toBe(true);
      expect(regex.test("/reset-password")).toBe(true);
      expect(regex.test("/confirm-email-change")).toBe(true);
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
