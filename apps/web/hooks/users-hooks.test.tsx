import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { useInvitations } from "./useInvitations";
import { useUsers } from "./useUsers";

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

describe("useUsers", () => {
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

  afterEach(() => {
    globalThis.fetch = originalFetch;
    setGlobalDispatcher(mockAgent);
    mockAgent.close();
  });

  it("GET /api/v1/users", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "user-1",
            email: "admin@example.com",
            createdAt: "2026-01-01T00:00:00.000Z",
          },
        ],
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useUsers(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.users).toHaveLength(1);
    expect(result.current.users[0].email).toBe("admin@example.com");
  });

  it("DELETE /api/v1/users/{id}", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, { items: [] });
    pool
      .intercept({
        path: "/api/go/api/v1/users/user-2",
        method: "DELETE",
      })
      .reply(200, {});
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, { items: [] });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useUsers(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await result.current.deleteUser.mutateAsync("user-2");
  });

  it("POST /api/v1/users/me/email-change-request", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, { items: [] });
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/email-change-request",
        method: "POST",
        body: JSON.stringify({
          newEmail: "new@example.com",
          currentPassword: "secret",
        }),
      })
      .reply(200, {});

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useUsers(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await result.current.requestEmailChange.mutateAsync({
      newEmail: "new@example.com",
      currentPassword: "secret",
    });
  });
});

describe("useInvitations", () => {
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

  afterEach(() => {
    globalThis.fetch = originalFetch;
    setGlobalDispatcher(mockAgent);
    mockAgent.close();
  });

  it("GET /api/v1/users/invitations", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "inv-1",
            email: "invitee@example.com",
            expiresAt: "2026-02-01T00:00:00.000Z",
            createdAt: "2026-01-01T00:00:00.000Z",
            invitedByUserId: "user-1",
          },
        ],
      });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useInvitations(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.invitations).toHaveLength(1);
  });

  it("POST /api/v1/users/invitations", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, { items: [] });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "POST",
        body: JSON.stringify({ email: "new@example.com" }),
      })
      .reply(201, {
        id: "inv-2",
        email: "new@example.com",
        expiresAt: "2026-02-01T00:00:00.000Z",
        inviteUrl: "https://example.com/accept-invitation?token=abc",
      });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, { items: [] });

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useInvitations(), {
      wrapper: createWrapper(queryClient),
    });

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    const created = await result.current.createInvitation.mutateAsync({
      email: "new@example.com",
    });
    expect(created.inviteUrl).toContain("accept-invitation");
  });
});
