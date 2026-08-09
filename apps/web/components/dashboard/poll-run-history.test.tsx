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
import { PollRunHistory } from "@/components/dashboard/poll-run-history";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const sampleRuns = [
  {
    id: "run-1",
    startedAt: "2026-08-07T10:00:00.000Z",
    finishedAt: "2026-08-07T10:05:00.000Z",
    status: "success",
    triggerSource: "manual",
    reposChecked: 2,
    ticketsCreated: 1,
    ticketsSuperseded: 0,
    errors: [],
  },
  {
    id: "run-2",
    startedAt: "2026-08-06T10:00:00.000Z",
    finishedAt: "2026-08-06T10:03:00.000Z",
    status: "partial",
    triggerSource: "scheduled",
    reposChecked: 3,
    ticketsCreated: 0,
    ticketsSuperseded: 0,
    errors: [{ repoId: "repo-1", message: "timeout" }],
  },
];

const sampleRunDetail = {
  id: "run-1",
  startedAt: "2026-08-07T10:00:00.000Z",
  finishedAt: "2026-08-07T10:05:00.000Z",
  status: "success",
  triggerSource: "manual",
  reposChecked: 2,
  ticketsCreated: 1,
  ticketsSuperseded: 0,
  errors: [],
  events: [
    {
      id: "evt-1",
      pollRunId: "run-1",
      monitoredRepoId: "repo-1",
      action: "create",
      detail: "TASK-99",
      createdAt: "2026-08-07T10:04:00.000Z",
    },
    {
      id: "evt-2",
      pollRunId: "run-1",
      monitoredRepoId: "repo-2",
      action: "skip",
      detail: null,
      createdAt: "2026-08-07T10:04:30.000Z",
    },
  ],
};

function renderPollRunHistory(): void {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <PollRunHistory />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("PollRunHistory", () => {
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

  it("lists poll runs from GET /api/v1/poll/runs", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(200, sampleRuns);

    renderPollRunHistory();

    expect(await screen.findByText("Success")).toBeTruthy();
    expect(await screen.findByText("Partial")).toBeTruthy();
    expect(await screen.findByText("Manual")).toBeTruthy();
    expect(await screen.findByText("Automatic")).toBeTruthy();
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

    renderPollRunHistory();

    expect(await screen.findByText("No poll runs yet")).toBeTruthy();
    expect(
      await screen.findByText(
        "Runs appear here after the scheduler or a manual poll completes.",
      ),
    ).toBeTruthy();
  });

  it("opens drawer with events from GET /api/v1/poll/runs/{id}", async () => {
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
      .reply(200, sampleRunDetail);

    renderPollRunHistory();

    const successBadge = await screen.findByText("Success");
    const row = successBadge.closest("tr");
    expect(row).toBeTruthy();
    fireEvent.click(row!);

    expect(await screen.findByText("Poll run details")).toBeTruthy();
    expect(await screen.findByText("Manual")).toBeTruthy();
    expect(await screen.findByText("Create ticket")).toBeTruthy();
    expect(await screen.findByText("Skip")).toBeTruthy();
    expect(await screen.findByText("TASK-99")).toBeTruthy();
  });

  it("paginates with limit and offset", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "0" },
      })
      .reply(
        200,
        Array.from({ length: 10 }, (_, index) => ({
          id: `run-page1-${index}`,
          startedAt: "2026-08-07T10:00:00.000Z",
          finishedAt: "2026-08-07T10:05:00.000Z",
          status: "success",
          triggerSource: "scheduled",
          reposChecked: 1,
          ticketsCreated: 0,
          ticketsSuperseded: 0,
          errors: [],
        })),
      );
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "10", offset: "10" },
      })
      .reply(200, [
        {
          id: "run-page2-0",
          startedAt: "2026-08-06T10:00:00.000Z",
          finishedAt: "2026-08-06T10:05:00.000Z",
          status: "failed",
          triggerSource: "scheduled",
          reposChecked: 1,
          ticketsCreated: 0,
          ticketsSuperseded: 0,
          errors: [],
        },
      ]);

    renderPollRunHistory();

    await screen.findAllByText("Success");
    const nextButton = await screen.findByRole("button", { name: "Next" });
    fireEvent.click(nextButton);

    await waitFor(async () => {
      expect(await screen.findByText("Failed")).toBeTruthy();
    });
  });
});
