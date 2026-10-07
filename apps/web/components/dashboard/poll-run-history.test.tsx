import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { PollRunHistory } from "@/components/dashboard/poll-run-history";
import { POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY } from "@/lib/pagination/constants";
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

const noRef = {
  sourceKind: null,
  projectPath: null,
  ticketExternalId: null,
  ticketUrl: null,
  releaseTag: null,
};

const sampleRunDetail = {
  id: "run-1",
  startedAt: "2026-08-07T10:00:00.000Z",
  finishedAt: "2026-08-07T10:05:00.000Z",
  status: "success",
  triggerSource: "manual",
  reposChecked: 2,
  ticketsCreated: 1,
  ticketsSuperseded: 2,
  errors: [
    { repoId: "repo-1", message: "timeout" },
    { repoId: "repo-gone", message: "boom" },
  ],
  events: [
    {
      id: "evt-1",
      pollRunId: "run-1",
      monitoredRepoId: "repo-1",
      action: "create",
      detail: "created ticket",
      createdAt: "2026-08-07T10:04:00.000Z",
      sourceKind: "github",
      projectPath: "acme/widget",
      ticketExternalId: "TASK-99",
      ticketUrl: "https://tracker.example/TASK-99",
      releaseTag: "v1.2.0",
    },
    {
      id: "evt-2",
      pollRunId: "run-1",
      monitoredRepoId: null,
      action: "skip",
      detail: null,
      createdAt: "2026-08-07T10:04:30.000Z",
      ...noRef,
    },
    {
      id: "evt-3",
      pollRunId: "run-1",
      monitoredRepoId: "repo-3",
      action: "merge",
      detail: null,
      createdAt: "2026-08-07T10:04:40.000Z",
      ...noRef,
      ticketExternalId: "EVIL-1",
      ticketUrl: "javascript:alert(1)",
    },
    {
      id: "evt-4",
      pollRunId: "run-1",
      monitoredRepoId: "repo-4",
      action: "supersede",
      detail: null,
      createdAt: "2026-08-07T10:04:50.000Z",
      ...noRef,
      ticketExternalId: "OLD-7",
    },
  ],
};

function createLocalStorageMock(): Storage {
  let store: Record<string, string> = {};

  return {
    get length() {
      return Object.keys(store).length;
    },
    clear() {
      store = {};
    },
    getItem(key: string) {
      return store[key] ?? null;
    },
    key(index: number) {
      return Object.keys(store)[index] ?? null;
    },
    removeItem(key: string) {
      delete store[key];
    },
    setItem(key: string, value: string) {
      store[key] = value;
    },
  };
}

async function selectPageSize(size: number): Promise<void> {
  const pageSizeSelect = await screen.findByRole("combobox", {
    name: "Rows per page",
  });
  fireEvent.click(pageSizeSelect);
  await waitFor(() => {
    expect(screen.getByRole("listbox")).toBeInTheDocument();
  });

  const option = screen.getByRole("option", { name: String(size) });
  fireEvent.pointerDown(option, {
    buttons: 1,
    pointerId: 1,
    pointerType: "mouse",
  });
  fireEvent.pointerUp(option, {
    pointerId: 1,
    pointerType: "mouse",
  });
  fireEvent.click(option);
}

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
  let localStorageMock: Storage;

  beforeEach(() => {
    localStorageMock = createLocalStorageMock();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: localStorageMock,
    });
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
    expect(await screen.findByText("Aug 7, 2026, 10:00:00.000")).toBeTruthy();
    expect(await screen.findByText("Aug 7, 2026, 10:05:00.000")).toBeTruthy();
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
    expect(document.querySelector('[data-slot="empty"]')).toBeTruthy();
    expect(
      await screen.findByText(
        "Runs appear here after the scheduler or a manual poll completes.",
      ),
    ).toBeTruthy();
  });

  async function openRunDetail(): Promise<HTMLElement> {
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

    return screen.findByRole("dialog");
  }

  it("opens a dialog with events from GET /api/v1/poll/runs/{id}", async () => {
    const dialog = await openRunDetail();

    expect(dialog.getAttribute("data-slot")).toBe("dialog-popup");
    expect(await within(dialog).findByText("Poll run details")).toBeTruthy();
    expect(await within(dialog).findByText("Create ticket")).toBeTruthy();
    expect(within(dialog).getByText("Skip")).toBeTruthy();
    expect(within(dialog).getByText("created ticket")).toBeTruthy();
    expect(within(dialog).getByText("Aug 7, 2026, 10:04:00.000")).toBeTruthy();
    expect(within(dialog).getByText("Aug 7, 2026, 10:04:30.000")).toBeTruthy();
  });

  it("shows repo, tag and ticket columns per event", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    for (const column of [
      "Time",
      "Repo",
      "Action",
      "Tag",
      "Ticket",
      "Detail",
    ]) {
      expect(
        within(dialog).getByRole("columnheader", { name: column }),
      ).toBeTruthy();
    }

    const createRow = within(dialog).getByText("Create ticket").closest("tr")!;
    expect(within(createRow).getByText("acme/widget")).toBeTruthy();
    expect(within(createRow).getByText("github")).toBeTruthy();
    expect(within(createRow).getByText("v1.2.0")).toBeTruthy();

    const link = within(createRow).getByRole("link", { name: "TASK-99" });
    expect(link.getAttribute("href")).toBe("https://tracker.example/TASK-99");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(link.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("renders a dash for repo, tag and ticket of events without them", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    const skipRow = within(dialog).getByText("Skip").closest("tr")!;
    expect(within(skipRow).queryByRole("link")).toBeNull();
    // repo, tag, ticket and detail are all empty
    expect(within(skipRow).getAllByText("—")).toHaveLength(4);
  });

  it("does not render a non-http(s) ticket URL as a link", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    const mergeRow = within(dialog).getByText("Merge").closest("tr")!;
    expect(within(mergeRow).getByText("EVIL-1")).toBeTruthy();
    expect(within(mergeRow).queryByRole("link")).toBeNull();
    expect(dialog.querySelector('a[href^="javascript:"]')).toBeNull();
  });

  it("shows the ticket id as text when only the id was recorded", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    const supersedeRow = within(dialog).getByText("Supersede").closest("tr")!;
    expect(within(supersedeRow).getByText("OLD-7")).toBeTruthy();
    expect(within(supersedeRow).queryByRole("link")).toBeNull();
  });

  it("lists run errors by repo path and falls back to the repo id", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    const errors = within(dialog).getByText("Errors").parentElement!;
    expect(within(errors).getByText("acme/widget")).toBeTruthy();
    expect(within(errors).queryByText("repo-1")).toBeNull();
    expect(within(errors).getByText("repo-gone")).toBeTruthy();
  });

  it("shows the trigger once and the superseded count", async () => {
    const dialog = await openRunDetail();
    await within(dialog).findByText("Create ticket");

    expect(within(dialog).getAllByText("Manual")).toHaveLength(1);
    const superseded = within(dialog).getByText("Tickets superseded");
    expect(superseded.nextElementSibling?.textContent).toBe("2");
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

  it("hydrates page size from localStorage", async () => {
    localStorageMock.setItem(POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY, "25");

    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "25", offset: "0" },
      })
      .reply(200, sampleRuns);

    renderPollRunHistory();

    expect(await screen.findByText("Success")).toBeTruthy();
  });

  it("persists page size changes and refetches with the new limit", async () => {
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
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "25", offset: "0" },
      })
      .reply(200, sampleRuns);

    renderPollRunHistory();

    await screen.findByText("Success");

    await selectPageSize(25);

    await waitFor(() => {
      expect(
        localStorageMock.getItem(POLL_RUN_HISTORY_PAGE_SIZE_STORAGE_KEY),
      ).toBe("25");
    });
  });

  it("resets offset to 0 when page size changes", async () => {
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
    pool
      .intercept({
        path: "/api/go/api/v1/poll/runs",
        method: "GET",
        query: { limit: "25", offset: "0" },
      })
      .reply(200, sampleRuns);

    renderPollRunHistory();

    await screen.findAllByText("Success");
    fireEvent.click(await screen.findByRole("button", { name: "Next" }));
    await waitFor(async () => {
      expect(await screen.findByText("Failed")).toBeTruthy();
    });

    await selectPageSize(25);

    await waitFor(async () => {
      expect(await screen.findByText("Partial")).toBeTruthy();
    });
    expect(screen.queryByText("Failed")).toBeNull();
  });
});
