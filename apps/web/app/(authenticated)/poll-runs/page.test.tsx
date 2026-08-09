import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import messages from "@/messages/en.json";
import PollRunsPage from "./page";

const ORIGIN = "http://localhost:3000";

const sampleRuns = [
  {
    id: "run-1",
    startedAt: "2026-08-07T10:00:00.000Z",
    finishedAt: "2026-08-07T10:05:00.000Z",
    status: "success",
    reposChecked: 2,
    ticketsCreated: 1,
    ticketsSuperseded: 0,
    errors: [],
  },
];

function renderPollRunsPage(): HTMLElement {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  const { container } = render(
    <QueryClientProvider client={queryClient}>
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <PollRunsPage />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );

  return container;
}

describe("PollRunsPage", () => {
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
    cleanup();
    globalThis.fetch = originalFetch;
    await mockAgent.close();
  });

  it("renders page title and poll run history table", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(200, sampleRuns);

    renderPollRunsPage();

    expect(
      await screen.findByRole("heading", { name: "Poll run history" }),
    ).toBeTruthy();
    expect(await screen.findByText("Success")).toBeTruthy();
    expect(await screen.findByText("2")).toBeTruthy();
    expect(await screen.findByText("1")).toBeTruthy();
  });

  it("shows empty state when no runs exist", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(200, []);

    renderPollRunsPage();

    expect(await screen.findByText("No poll runs yet")).toBeTruthy();
  });

  it("opens run detail drawer from table row", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(200, sampleRuns);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs/run-1",
        method: "GET",
      })
      .reply(200, {
        ...sampleRuns[0],
        events: [
          {
            id: "evt-1",
            pollRunId: "run-1",
            monitoredRepoId: "repo-1",
            action: "create",
            detail: "TASK-99",
            createdAt: "2026-08-07T10:04:00.000Z",
          },
        ],
      });

    renderPollRunsPage();

    const successBadge = await screen.findByText("Success");
    const row = successBadge.closest("tr");
    expect(row).toBeTruthy();
    fireEvent.click(row!);

    await waitFor(async () => {
      expect(await screen.findByText("Poll run details")).toBeTruthy();
    });
  });

  it("uses a mobile-friendly responsive root layout", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(200, sampleRuns);

    const container = renderPollRunsPage();

    await screen.findByRole("heading", { name: "Poll run history" });

    const root = container.querySelector(".min-w-0.w-full");
    expect(root).toBeTruthy();
  });
});
