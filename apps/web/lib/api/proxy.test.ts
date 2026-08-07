import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { apiClient, ApiError, NetworkError } from "./client";

const ORIGIN = "http://localhost:3000";

describe("apiClient proxy client", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    mockAgent = new MockAgent();
    setGlobalDispatcher(mockAgent);
    mockAgent.disableNetConnect();

    globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
      const url =
        typeof input === "string" && input.startsWith("/")
          ? `${ORIGIN}${input}`
          : input instanceof URL
            ? input
            : input instanceof Request
              ? input.url.startsWith("/")
                ? `${ORIGIN}${input.url}`
                : input.url
              : input;
      return originalFetch(url, init);
    }) as typeof fetch;
  });

  afterEach(async () => {
    globalThis.fetch = originalFetch;
    await mockAgent.close();
  });

  it("uses same-origin /api/go/api/v1 base path with credentials include", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/session",
        method: "GET",
      })
      .reply(200, { user: null });

    const result = await apiClient.get<{ user: null }>("/auth/session");
    expect(result).toEqual({ user: null });
  });

  it("sets Content-Type application/json on POST body", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login",
        method: "POST",
        headers: {
          "content-type": "application/json",
        },
        body: JSON.stringify({ email: "a@b.com", password: "pw" }),
      })
      .reply(200, { user: { id: "1", email: "a@b.com" } });

    const result = await apiClient.post("/auth/login", {
      email: "a@b.com",
      password: "pw",
    });
    expect(result).toEqual({ user: { id: "1", email: "a@b.com" } });
  });

  it("parses Go error envelope on non-2xx responses", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login",
        method: "POST",
      })
      .reply(401, {
        error: {
          code: "invalid_credentials",
          message: "invalid email or password",
        },
      });

    await expect(
      apiClient.post("/auth/login", { email: "bad", password: "bad" }),
    ).rejects.toMatchObject({
      name: "ApiError",
      code: "invalid_credentials",
      message: "invalid email or password",
      status: 401,
    });
  });

  it("throws ApiError for validation errors from Go", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/repos",
        method: "POST",
      })
      .reply(400, {
        error: { code: "VALIDATION_ERROR", message: "projectPath is required" },
      });

    try {
      await apiClient.post("/repos", {});
      expect.fail("expected ApiError");
    } catch (error) {
      expect(error).toBeInstanceOf(ApiError);
      expect((error as ApiError).code).toBe("VALIDATION_ERROR");
      expect((error as ApiError).message).toBe("projectPath is required");
      expect((error as ApiError).status).toBe(400);
    }
  });

  it("throws NetworkError when fetch fails", async () => {
    globalThis.fetch = () => {
      throw new TypeError("fetch failed");
    };

    await expect(apiClient.get("/auth/session")).rejects.toBeInstanceOf(
      NetworkError,
    );
  });
});
