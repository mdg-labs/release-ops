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
import { DashboardView } from "@/components/dashboard/dashboard-view";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const sampleStatus = {
  pollIntervalMinutes: 360,
  lastRun: {
    id: "run-1",
    startedAt: "2026-08-07T10:00:00.000Z",
    finishedAt: "2026-08-07T10:05:00.000Z",
    status: "failed",
    reposChecked: 2,
    ticketsCreated: 0,
    ticketsSuperseded: 0,
    errors: [
      {
        repoId: "repo-1",
        message: "rate limit exceeded",
      },
    ],
  },
  repos: [
    {
      id: "repo-1",
      sourceKind: "github",
      projectPath: "org/app",
      ticketProjectId: "tp-1",
      ticketProjectName: "Jira — DEV",
      enabled: true,
      openTicketExternalId: "TASK-42",
      openTicketTag: "v2.0.0",
      lastKnownTag: "v2.0.0",
      lastPolledAt: "2026-08-07T10:05:00.000Z",
      lastError: "rate limit exceeded",
    },
    {
      id: "repo-2",
      sourceKind: "gitlab",
      projectPath: "group/service",
      ticketProjectId: "tp-1",
      ticketProjectName: "Jira — DEV",
      enabled: true,
      openTicketExternalId: null,
      openTicketTag: null,
      lastKnownTag: "1.0.0",
      lastPolledAt: "2026-08-07T10:05:00.000Z",
      lastError: null,
    },
  ],
  isPolling: false,
};

function renderDashboard(): void {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <DashboardView />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("DashboardView", () => {
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

  it("renders status card and repo table", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus);

    renderDashboard();

    expect(
      await screen.findByRole("heading", { name: "Dashboard" }),
    ).toBeTruthy();
    expect(document.querySelector('[data-slot="page-header"]')).toBeTruthy();
    expect(await screen.findByText("System status")).toBeTruthy();
    expect(
      await screen.findByRole("heading", { name: "Monitored repos" }),
    ).toBeTruthy();
    expect((await screen.findAllByText("org/app")).length).toBeGreaterThan(0);
    expect(await screen.findByText("group/service")).toBeTruthy();
    expect(await screen.findByText("TASK-42")).toBeTruthy();
  });

  it("renders last polled column with formatted dates and Never", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        ...sampleStatus,
        repos: [
          {
            ...sampleStatus.repos[0],
            lastPolledAt: "2026-08-06T15:30:00.000Z",
          },
          {
            ...sampleStatus.repos[1],
            lastPolledAt: null,
          },
        ],
      });

    renderDashboard();

    expect(await screen.findByText("Last polled")).toBeTruthy();
    expect(await screen.findByText("Aug 6, 2026, 15:30")).toBeTruthy();
    expect(await screen.findByText("Never")).toBeTruthy();
  });

  it("formats poll health timestamps with milliseconds in 24-hour clock", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus);

    renderDashboard();

    expect(await screen.findByText("Aug 7, 2026, 10:00:00.000")).toBeTruthy();
    expect(await screen.findByText("Aug 7, 2026, 10:05:00.000")).toBeTruthy();
  });

  it("shows error alert when last run has errors", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus);

    renderDashboard();

    expect(await screen.findByText("Errors in last poll run")).toBeTruthy();
    expect(await screen.findByText(/rate limit exceeded/)).toBeTruthy();
  });

  it("triggers manual poll via POST /api/v1/poll/trigger", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus)
      .times(2);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/trigger",
        method: "POST",
      })
      .reply(202, { runId: "run-2" });

    renderDashboard();

    const runPollButton = await screen.findByRole("button", {
      name: "Run poll now",
    });
    fireEvent.click(runPollButton);

    await waitFor(() => {
      expect(runPollButton).toBeTruthy();
    });
  });

  it("links to poll run history from status card", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus);

    renderDashboard();

    const viewAllRunsLink = await screen.findByRole("link", {
      name: "View all runs",
    });
    expect(viewAllRunsLink.getAttribute("href")).toBe("/poll-runs");
  });

  it("does not render poll run history section", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, sampleStatus);

    renderDashboard();

    await screen.findByRole("heading", { name: "Dashboard" });
    expect(
      screen.queryByRole("heading", { name: "Poll run history" }),
    ).toBeNull();
  });

  it("renders empty state with CTA to /repos", async () => {
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

    renderDashboard();

    expect(await screen.findByText("No monitored repos")).toBeTruthy();
    expect(document.querySelector('[data-slot="empty"]')).toBeTruthy();
    const cta = await screen.findByRole("link", { name: "Add repo" });
    expect(cta.getAttribute("href")).toBe("/repos");
  });
});
