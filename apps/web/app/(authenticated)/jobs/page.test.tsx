import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { MockAgent, setGlobalDispatcher } from "undici";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import messages from "@/messages/en.json";
import JobsPage from "./page";

const ORIGIN = "http://localhost:3000";

vi.mock("next/navigation", () => ({
  usePathname: () => "/jobs",
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  useSearchParams: () => new URLSearchParams(),
}));

describe("JobsPage", () => {
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
  });

  it("shows the job history and the Problems tab to a viewer", async () => {
    const pool = mockAgent.get(ORIGIN);
    const headers = { "content-type": "application/json" };
    pool
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(
        200,
        { user: { id: 1, login: "octocat", role: "viewer" } },
        { headers },
      )
      .persist();
    pool
      .intercept({ path: /^\/api\/go\/api\/v1\/repositories\?/, method: "GET" })
      .reply(200, { items: [], page: 1, pageSize: 100, total: 0 }, { headers })
      .persist();
    pool
      .intercept({ path: /^\/api\/go\/api\/v1\/jobs\?/, method: "GET" })
      .reply(200, { items: [], page: 1, pageSize: 10, total: 0 }, { headers })
      .persist();
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <QueryClientProvider client={queryClient}>
          <JobsPage />
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );

    expect(
      await screen.findByRole("heading", { name: "Jobs", level: 1 }),
    ).toBeTruthy();
    expect(screen.getByRole("tab", { name: "Problems" })).toBeTruthy();
    expect(await screen.findByText("No jobs yet")).toBeTruthy();
  });
});
