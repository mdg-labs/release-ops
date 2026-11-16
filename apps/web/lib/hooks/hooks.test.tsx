import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { useIntegrations } from "./use-integrations";
import { useRedeemLoginLink } from "./use-login";
import { useSession, useSessionRole } from "./use-session";
import { useSettings, useUpdateSettings } from "./use-settings";
import { useStatus } from "./use-status";

const ORIGIN = "http://localhost:3000";

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}

describe("React Query hooks", () => {
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

  it("useRedeemLoginLink POSTs the token to /api/v1/auth/login-link", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login-link",
        method: "POST",
        body: JSON.stringify({ token: "abc" }),
      })
      .reply(200, { user: { id: 583231, login: "octocat", role: "admin" } });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useRedeemLoginLink(), {
      wrapper: createWrapper(queryClient),
    });

    const redeemed = await result.current.mutateAsync({ token: "abc" });
    expect(redeemed).toEqual({
      user: { id: 583231, login: "octocat", role: "admin" },
    });
  });

  it("useSession fetches GET /api/v1/auth/session", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/session",
        method: "GET",
      })
      .reply(200, { user: { id: "u1", email: "admin@example.com" } });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useSession(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual({
      user: { id: "u1", email: "admin@example.com" },
    });
  });

  it("useSessionRole reads the role from the session and none when it fails", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(200, { user: { id: 5, login: "vera", role: "viewer" } });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useSessionRole(), {
      wrapper: createWrapper(queryClient),
    });
    expect(result.current).toEqual({
      role: null,
      isLoading: true,
      isError: false,
    });
    await waitFor(() => expect(result.current.role).toBe("viewer"));

    // The last data stays after a failed refetch; the role must not.
    pool
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(503, { error: { code: "SERVICE_UNAVAILABLE" } });
    await queryClient.refetchQueries();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.role).toBeNull();
  });

  it("useStatus fetches GET /api/v1/status", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, { version: "v2.0.0" });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useStatus(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.version).toBe("v2.0.0");
  });

  it("useIntegrations includes test mutation", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, [
        { id: "int-1", kind: "jira", name: "Jira", hasSecret: true },
      ]);

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useIntegrations(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1/test",
        method: "POST",
      })
      .reply(200, { success: true, message: "ok" });

    const testResult =
      await result.current.testIntegration.mutateAsync("int-1");
    expect(testResult).toEqual({ success: true, message: "ok" });
  });

  it("useSettings GETs the settings and PATCHes only the concurrency cap", async () => {
    const settings = {
      concurrencyCap: 2,
      mode: "webhook",
      presetRepositories: [],
      globalConfig: { source: null, repository: null, ref: null },
    };
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/settings", method: "GET" })
      .reply(200, settings);

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useSettings(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.concurrencyCap).toBe(2);

    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "PATCH",
        body: JSON.stringify({ concurrencyCap: 4 }),
      })
      .reply(200, { ...settings, concurrencyCap: 4 });

    const update = renderHook(() => useUpdateSettings(), {
      wrapper: createWrapper(queryClient),
    });
    const updated = await update.result.current.mutateAsync({
      concurrencyCap: 4,
    });
    expect(updated.concurrencyCap).toBe(4);
    await waitFor(() => expect(result.current.data?.concurrencyCap).toBe(4));
  });
});
