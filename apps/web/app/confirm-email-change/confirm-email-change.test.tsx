import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { ConfirmEmailChangeForm } from "./confirm-email-change-form";

const ORIGIN = "http://localhost:3000";

const pushMock = vi.fn();
const refreshMock = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: pushMock, refresh: refreshMock }),
  useSearchParams: () => new URLSearchParams("token=email-token"),
}));

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
}));

describe("ConfirmEmailChangeForm", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    pushMock.mockReset();
    refreshMock.mockReset();
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
    cleanup();
    globalThis.fetch = originalFetch;
    void mockAgent.close();
  });

  it("confirms token on mount and redirects to /", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/confirm-email-change",
        method: "POST",
        body: JSON.stringify({ token: "email-token" }),
      })
      .reply(200, {
        user: { id: "u1", email: "new@example.com" },
      });

    render(<ConfirmEmailChangeForm />);

    await waitFor(() => expect(pushMock).toHaveBeenCalledWith("/"));
    expect(refreshMock).toHaveBeenCalled();
  });

  it("shows invalid token error from API", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/confirm-email-change",
        method: "POST",
      })
      .reply(400, {
        error: {
          code: "VALIDATION_ERROR",
          message: "invalid or expired token",
        },
      });

    render(<ConfirmEmailChangeForm />);

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("auth.invalidToken"),
    );
  });
});
