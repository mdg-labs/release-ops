import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { AdminOnly } from "@/components/common/admin-only";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

function renderAdminOnly() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <QueryClientProvider client={queryClient}>
        <AdminOnly>
          <p data-testid="admin-page" />
        </AdminOnly>
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
  return queryClient;
}

describe("AdminOnly", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    mockAgent = new MockAgent();
    setGlobalDispatcher(mockAgent);
    mockAgent.disableNetConnect();
    globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) =>
      originalFetch(
        typeof input === "string" && input.startsWith("/")
          ? `${ORIGIN}${input}`
          : input,
        init,
      )) as typeof fetch;
  });

  afterEach(async () => {
    cleanup();
    globalThis.fetch = originalFetch;
    await mockAgent.close();
    vi.unstubAllGlobals();
  });

  function session(reply: object, statusCode = 200) {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(statusCode, reply, {
        headers: { "content-type": "application/json" },
      });
  }

  it("renders the page for an admin", async () => {
    session({ user: { id: 1, login: "octocat", role: "admin" } });
    renderAdminOnly();

    expect(await screen.findByTestId("admin-page")).toBeInTheDocument();
    expect(screen.queryByTestId("forbidden-state")).not.toBeInTheDocument();
  });

  it("shows a viewer the forbidden state instead of the page", async () => {
    session({ user: { id: 2, login: "vera", role: "viewer" } });
    renderAdminOnly();

    expect(await screen.findByTestId("forbidden-state")).toHaveTextContent(
      "You do not have access to this page",
    );
    expect(screen.queryByTestId("admin-page")).not.toBeInTheDocument();
  });

  it("renders nothing of the page while the role loads", () => {
    renderAdminOnly();

    expect(screen.getByTestId("admin-only-loading")).toBeInTheDocument();
    expect(screen.queryByTestId("admin-page")).not.toBeInTheDocument();
  });

  it("shows an error, not the page, when the session request fails", async () => {
    session({ error: { code: "SERVICE_UNAVAILABLE" } }, 503);
    renderAdminOnly();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not check your access",
    );
    expect(screen.queryByTestId("admin-page")).not.toBeInTheDocument();
  });

  it("shows an error, not the page, for a session without a role", async () => {
    session({ user: { id: 3, login: "legacy" } });
    renderAdminOnly();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not check your access",
    );
    expect(screen.queryByTestId("admin-page")).not.toBeInTheDocument();
  });

  it("drops the page when a refetch fails after an admin was shown it", async () => {
    session({ user: { id: 1, login: "octocat", role: "admin" } });
    const queryClient = renderAdminOnly();
    expect(await screen.findByTestId("admin-page")).toBeInTheDocument();

    session({ error: { code: "SERVICE_UNAVAILABLE" } }, 503);
    await queryClient.refetchQueries();

    await waitFor(() =>
      expect(screen.queryByTestId("admin-page")).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Could not check your access",
    );
  });
});
