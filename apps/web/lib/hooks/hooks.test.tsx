import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { useIntegrations } from "./use-integrations";
import { useLogin } from "./use-login";
import { usePollRun, usePollRuns, useTriggerPoll } from "./use-poll";
import { useRepos } from "./use-repos";
import { useSession } from "./use-session";
import { useSettings } from "./use-settings";
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

  it("useLogin POST /api/v1/auth/login", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login",
        method: "POST",
        body: JSON.stringify({
          email: "admin@example.com",
          password: "secret",
        }),
      })
      .reply(200, {
        user: { id: "u1", email: "admin@example.com" },
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useLogin(), {
      wrapper: createWrapper(queryClient),
    });

    const loginResult = await result.current.mutateAsync({
      email: "admin@example.com",
      password: "secret",
    });
    expect(loginResult).toEqual({
      user: { id: "u1", email: "admin@example.com" },
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

  it("useStatus fetches GET /api/v1/status", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 360,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useStatus(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.pollIntervalMinutes).toBe(360);
    expect(result.current.data?.isPolling).toBe(false);
  });

  it("useRepos provides CRUD mutations", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/repos",
        method: "GET",
      })
      .reply(200, []);

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useRepos(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    pool
      .intercept({
        path: "/api/go/api/v1/repos",
        method: "POST",
      })
      .reply(200, {
        id: "repo-1",
        sourceKind: "github",
        projectPath: "org/repo",
        enabled: true,
        sourceIntegrationId: null,
        ticketProjectId: "tp-1",
        notificationTargetIds: [],
        openTicketExternalId: null,
        openTicketTag: null,
        lastKnownTag: null,
        lastPolledAt: null,
        lastError: null,
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      });

    pool
      .intercept({
        path: "/api/go/api/v1/repos",
        method: "GET",
      })
      .reply(200, [
        {
          id: "repo-1",
          sourceKind: "github",
          projectPath: "org/repo",
          enabled: true,
          sourceIntegrationId: null,
          ticketProjectId: "tp-1",
          notificationTargetIds: [],
          openTicketExternalId: null,
          openTicketTag: null,
          lastKnownTag: null,
          lastPolledAt: null,
          lastError: null,
          createdAt: "2026-01-01T00:00:00Z",
          updatedAt: "2026-01-01T00:00:00Z",
        },
      ]);

    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 360,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    const created = await result.current.createRepo.mutateAsync({
      sourceKind: "github",
      projectPath: "org/repo",
      ticketProjectId: "tp-1",
    });
    expect(created.id).toBe("repo-1");

    pool
      .intercept({
        path: "/api/go/api/v1/repos/repo-1",
        method: "PATCH",
      })
      .reply(200, {
        id: "repo-1",
        sourceKind: "github",
        projectPath: "org/repo",
        enabled: false,
        sourceIntegrationId: null,
        ticketProjectId: "tp-1",
        notificationTargetIds: [],
        openTicketExternalId: null,
        openTicketTag: null,
        lastKnownTag: null,
        lastPolledAt: null,
        lastError: null,
        createdAt: "2026-01-01T00:00:00Z",
        updatedAt: "2026-01-01T00:00:00Z",
      });

    const updated = await result.current.updateRepo.mutateAsync({
      id: "repo-1",
      input: {
        sourceKind: "github",
        projectPath: "org/repo",
        enabled: false,
        ticketProjectId: "tp-1",
        notificationTargetIds: [],
      },
    });
    expect(updated.enabled).toBe(false);

    pool
      .intercept({
        path: "/api/go/api/v1/repos/repo-1",
        method: "DELETE",
      })
      .reply(204);

    pool
      .intercept({
        path: "/api/go/api/v1/repos",
        method: "GET",
      })
      .reply(200, []);

    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 360,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    await result.current.deleteRepo.mutateAsync("repo-1");
  });

  it("useIntegrations includes test mutation", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, [
        { id: "int-1", kind: "github", name: "GH", hasSecret: true },
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

  it("usePollRuns fetches paginated run list", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "5" },
      })
      .reply(200, [
        {
          id: "run-1",
          startedAt: "2026-01-01T00:00:00Z",
          finishedAt: "2026-01-01T00:01:00Z",
          status: "success",
          triggerSource: "manual",
          reposChecked: 3,
          ticketsCreated: 1,
          ticketsSuperseded: 0,
          errors: [],
        },
      ]);

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => usePollRuns({ limit: 10, offset: 5 }), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data?.[0].id).toBe("run-1");
  });

  it("usePollRun fetches single run with events[]", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs/run-1",
        method: "GET",
      })
      .reply(200, {
        id: "run-1",
        startedAt: "2026-01-01T00:00:00Z",
        finishedAt: "2026-01-01T00:01:00Z",
        status: "success",
        triggerSource: "manual",
        reposChecked: 1,
        ticketsCreated: 1,
        ticketsSuperseded: 0,
        errors: [],
        events: [
          {
            id: "evt-1",
            pollRunId: "run-1",
            monitoredRepoId: "repo-1",
            action: "create",
            detail: null,
            createdAt: "2026-01-01T00:00:30Z",
          },
        ],
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => usePollRun("run-1"), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.events).toHaveLength(1);
    expect(result.current.data?.events?.[0].action).toBe("create");
  });

  it("useSettings GET and PATCH /api/v1/settings", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 360,
        inviteTokenExpiryHours: 168,
        passwordResetTokenExpiryMinutes: 60,
        smtpConfigured: false,
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useSettings(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.pollIntervalMinutes).toBe(360);

    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "PATCH",
        body: JSON.stringify({ pollIntervalMinutes: 120 }),
      })
      .reply(200, {
        pollIntervalMinutes: 120,
        inviteTokenExpiryHours: 168,
        passwordResetTokenExpiryMinutes: 60,
        smtpConfigured: false,
      });

    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 120,
        inviteTokenExpiryHours: 168,
        passwordResetTokenExpiryMinutes: 60,
        smtpConfigured: false,
      });

    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 120,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    const updated = await result.current.updateSettings.mutateAsync({
      pollIntervalMinutes: 120,
    });
    expect(updated.pollIntervalMinutes).toBe(120);
  });

  it("useTriggerPoll POST /api/v1/poll/trigger", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/trigger",
        method: "POST",
      })
      .reply(202, { runId: "run-new" });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useTriggerPoll(), {
      wrapper: createWrapper(queryClient),
    });

    const triggerResult = await result.current.mutateAsync();
    expect(triggerResult).toEqual({ runId: "run-new" });
  });
});
