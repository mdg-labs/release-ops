import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { useProfile } from "./useProfile";

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

describe("useProfile", () => {
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

  it("POST /api/v1/users/me/email-change-request", async () => {
    const pool = mockAgent.get(ORIGIN);
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
    const { result } = renderHook(() => useProfile(), {
      wrapper: createWrapper(queryClient),
    });

    await result.current.requestEmailChange.mutateAsync({
      newEmail: "new@example.com",
      currentPassword: "secret",
    });
  });

  it("POST /api/v1/users/me/password-change", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/password-change",
        method: "POST",
        body: JSON.stringify({
          currentPassword: "secret",
          newPassword: "newpassword",
        }),
      })
      .reply(200, {});

    const queryClient = createTestQueryClient();
    const { result } = renderHook(() => useProfile(), {
      wrapper: createWrapper(queryClient),
    });

    await result.current.changePassword.mutateAsync({
      currentPassword: "secret",
      newPassword: "newpassword",
    });
  });
});
