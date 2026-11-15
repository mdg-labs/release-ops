import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { NextRequest } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  MIDDLEWARE_MATCHER_PATTERN,
  PUBLIC_AUTH_PATHS,
  config,
  getSessionUser,
  isLoginPath,
  isPublicAuthPath,
  middleware,
} from "./middleware";

const ORIGIN = "http://localhost:3000";

const REMOVED_LOCAL_ACCOUNT_PATHS = [
  "/forgot-password",
  "/reset-password",
  "/accept-invitation",
  "/confirm-email-change",
  "/profile",
  "/settings/users",
] as const;

// Where each removed route's page folder would live under apps/web/app. The two
// signed-in pages sat in the (authenticated) route group, so they are checked there too.
const REMOVED_PAGE_DIRS = [
  "forgot-password",
  "reset-password",
  "accept-invitation",
  "confirm-email-change",
  "profile",
  "settings/users",
  "(authenticated)/profile",
  "(authenticated)/settings/users",
] as const;
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
    });

    it("lists exactly the sign-in and setup paths", () => {
      expect([...PUBLIC_AUTH_PATHS]).toEqual([
        "/login",
        "/login/link",
        "/setup",
      ]);
    });

    it("does not treat the removed local-account paths as public", () => {
      for (const path of REMOVED_LOCAL_ACCOUNT_PATHS) {
        expect(isPublicAuthPath(path)).toBe(false);
        expect(isPublicAuthPath(`${path}/extra`)).toBe(false);
      }
    });

    it("has no page left for the removed local-account paths", () => {
      for (const dir of REMOVED_PAGE_DIRS) {
        expect(
          existsSync(resolve(__dirname, "app", dir)),
          `apps/web/app/${dir} must not exist`,
        ).toBe(false);
      }
    });

    it("lists the break-glass sign-in page and keeps its siblings protected", () => {
      expect(PUBLIC_AUTH_PATHS).toContain("/login/link");
      expect(isPublicAuthPath("/login/link")).toBe(true);
      expect(isPublicAuthPath("/loginlink")).toBe(false);
      expect(isPublicAuthPath("/login-link")).toBe(false);
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

    it("returns null when the session endpoint answers 503", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({ ok: false, status: 503 }),
      );

      expect(await getSessionUser(makeRequest("/repos"))).toBeNull();
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
    it("allows the break-glass page without a session and sends a signed-in GitHub user home", async () => {
      mockNoSession();
      const anonymous = await middleware(makeRequest("/login/link?token=abc"));
      expect(anonymous.status).toBe(200);

      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({
            user: { id: 583231, login: "octocat", role: "admin" },
          }),
        }),
      );
      const signedIn = await middleware(makeRequest("/login/link?token=abc"));
      expect(signedIn.status).toBe(307);
      expect(new URL(signedIn.headers.get("location") ?? "").pathname).toBe(
        "/",
      );
      const page = await middleware(makeRequest("/repos"));
      expect(page.status).toBe(200);
    });

    it("allows /login without a session", async () => {
      mockNoSession();

      const response = await middleware(makeRequest("/login"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });

    it("allows public auth routes without a session", async () => {
      mockNoSession();

      const paths = ["/login/link", "/setup"];

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

    it("does not send a valid session to /login while the access check is unavailable", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({ ok: false, status: 503 }),
      );

      const response = await middleware(makeRequest("/repos"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
    });

    it("still redirects to /login when the session endpoint answers another error or cannot be reached", async () => {
      for (const status of [401, 500, 502]) {
        vi.stubGlobal(
          "fetch",
          vi.fn().mockResolvedValue({ ok: false, status }),
        );
        const response = await middleware(makeRequest("/repos"));
        expect(response.status).toBe(307);
        expect(response.headers.get("location")).toBe(
          `${ORIGIN}/login?redirect=%2Frepos`,
        );
      }

      vi.stubGlobal(
        "fetch",
        vi.fn().mockRejectedValue(new TypeError("fetch failed")),
      );
      const unreachable = await middleware(makeRequest("/repos"));
      expect(unreachable.status).toBe(307);
      expect(unreachable.headers.get("location")).toBe(
        `${ORIGIN}/login?redirect=%2Frepos`,
      );
    });

    it("renders /login and does not redirect home while the access check is unavailable", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({ ok: false, status: 503 }),
      );

      const response = await middleware(makeRequest("/login"));
      expect(response.status).toBe(200);
      expect(response.headers.get("location")).toBeNull();
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

    it("redirects the removed local-account paths to /login without a session", async () => {
      mockNoSession();

      for (const path of REMOVED_LOCAL_ACCOUNT_PATHS) {
        const response = await middleware(makeRequest(`${path}?token=abc`));
        expect(response.status).toBe(307);
        expect(response.headers.get("location")).toBe(
          `${ORIGIN}/login?redirect=${encodeURIComponent(path)}`,
        );
      }
    });

    it("passes the removed local-account paths on to the Next.js 404 when signed in", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
          ok: true,
          json: async () => ({
            user: { id: "user-1", email: "admin@example.com" },
          }),
        }),
      );

      for (const path of REMOVED_LOCAL_ACCOUNT_PATHS) {
        const response = await middleware(makeRequest(path));
        expect(response.status).toBe(200);
        expect(response.headers.get("location")).toBeNull();
      }
    });
  });

  describe("matcher config", () => {
    const regex = matcherRegex();

    it("matches app routes that require auth checks", () => {
      expect(regex.test("/")).toBe(true);
      expect(regex.test("/login")).toBe(true);
      for (const path of REMOVED_LOCAL_ACCOUNT_PATHS) {
        expect(regex.test(path)).toBe(true);
      }
      expect(regex.test("/repos")).toBe(true);
      expect(regex.test("/settings")).toBe(true);
    });

    it("excludes exactly the GitHub webhook path from the login redirect", () => {
      expect(regex.test("/hooks/github")).toBe(false);
      expect(regex.test("/hooks/github/")).toBe(true);
      expect(regex.test("/hooks/github/other")).toBe(true);
      expect(regex.test("/hooks/githubx")).toBe(true);
      expect(regex.test("/hooks")).toBe(true);
      expect(regex.test("/hooks/other")).toBe(true);
      expect(regex.test("/x/hooks/github")).toBe(true);
    });

    it("keeps the literal matcher in config in step with the exported pattern", () => {
      expect(config.matcher).toEqual([MIDDLEWARE_MATCHER_PATTERN]);
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
