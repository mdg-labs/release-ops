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
import { MockAgent, setGlobalDispatcher } from "undici";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { UpdatePr, UpdatePrsResponse } from "@/lib/query/types";
import messages from "@/messages/en.json";
import DashboardPage from "./page";

const ORIGIN = "http://localhost:3000";
const PATH = "/api/go/api/v1/update-prs";
const REPOSITORIES = "/api/go/api/v1/repositories";

function pr(overrides: Partial<UpdatePr> & { id: string }): UpdatePr {
  return {
    repository: { id: "repo-1", fullName: "acme/api" },
    number: 1,
    url: "https://github.com/acme/api/pull/1",
    title: "Update dependency foo to v2",
    state: "open",
    openedAt: new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString(),
    mergedAt: null,
    closedAt: null,
    supersededBy: null,
    updateTypes: ["major"],
    dependencies: [],
    ticket: null,
    ...overrides,
  };
}

function page(items: UpdatePr[], total = items.length): UpdatePrsResponse {
  return { items, page: 1, pageSize: 10, total };
}

describe("DashboardPage", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;
  let requests: URLSearchParams[];

  beforeEach(() => {
    requests = [];
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

  // Answers every list request: the state decides which body, and the request is recorded.
  function serve(open: UpdatePrsResponse | number, merged: UpdatePrsResponse) {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: (path: string) => {
          if (!path.startsWith(`${PATH}?`)) {
            return false;
          }
          requests.push(new URLSearchParams(path.split("?")[1]));
          return true;
        },
        method: "GET",
      })
      .reply((opts) => {
        const state = new URLSearchParams(String(opts.path).split("?")[1]).get(
          "state",
        );
        const body = state === "merged" ? merged : open;
        if (typeof body === "number") {
          return {
            statusCode: body,
            data: JSON.stringify({ error: { code: "INTERNAL_ERROR" } }),
            responseOptions: {
              headers: { "content-type": "application/json" },
            },
          };
        }
        return {
          statusCode: 200,
          data: JSON.stringify(body),
          responseOptions: { headers: { "content-type": "application/json" } },
        };
      })
      .persist();
  }

  // Answers GET /repositories, the source of the repository filter's options.
  function serveRepositories(items: { id: string; fullName: string }[]) {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: (path: string) => path.startsWith(`${REPOSITORIES}?`),
        method: "GET",
      })
      .reply(() => ({
        statusCode: 200,
        data: JSON.stringify({
          items,
          page: 1,
          pageSize: 100,
          total: items.length,
        }),
        responseOptions: { headers: { "content-type": "application/json" } },
      }))
      .persist();
  }

  function openSelect(name: string) {
    const trigger = screen.getByRole("combobox", { name });
    fireEvent.pointerDown(trigger, { button: 0, pointerType: "mouse" });
    fireEvent.mouseDown(trigger, { button: 0 });
    fireEvent.click(trigger);
  }

  function choose(option: HTMLElement) {
    fireEvent.pointerMove(option, { pointerType: "mouse" });
    fireEvent.pointerDown(option, { button: 0, pointerType: "mouse" });
    fireEvent.mouseDown(option, { button: 0 });
    fireEvent.pointerUp(option, { button: 0, pointerType: "mouse" });
    fireEvent.mouseUp(option, { button: 0 });
    fireEvent.click(option);
  }

  function renderPage() {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    return render(
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <QueryClientProvider client={queryClient}>
          <DashboardPage />
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );
  }

  it("shows the open and the recently merged update PRs with badges and ticket links", async () => {
    serve(
      page([
        pr({
          id: "pr-1",
          title: "Update dependency foo to v2",
          updateTypes: ["major", "minor"],
          ticket: {
            state: "open",
            externalId: "DEV-12",
            url: "https://tickets.example.com/DEV-12",
            hasError: true,
          },
        }),
        pr({
          id: "pr-2",
          number: 2,
          title: "Update bar",
          updateTypes: ["patch"],
        }),
      ]),
      page([
        pr({
          id: "pr-3",
          number: 3,
          state: "merged",
          title: "Update baz",
          mergedAt: "2026-11-14T08:30:00.000Z",
          ticket: {
            state: "done",
            externalId: "DEV-9",
            url: "https://tickets.example.com/DEV-9",
            hasError: false,
          },
        }),
      ]),
    );

    const { container } = renderPage();

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeTruthy();
    expect(container.querySelector(".min-w-0.w-full")).toBeTruthy();
    expect(await screen.findByText("Update dependency foo to v2")).toBeTruthy();

    const openSection = screen.getByRole("region", {
      name: "Open update pull requests",
    });
    const row = within(openSection)
      .getByText("Update dependency foo to v2")
      .closest("tr") as HTMLElement;
    expect(within(row).getByText("Major")).toBeTruthy();
    expect(within(row).getByText("Minor")).toBeTruthy();
    const ticketLink = within(row).getByRole("link", { name: "DEV-12" });
    expect(ticketLink.getAttribute("href")).toBe(
      "https://tickets.example.com/DEV-12",
    );
    expect(ticketLink.getAttribute("target")).toBe("_blank");
    expect(within(row).getByText("Open")).toBeTruthy();
    expect(within(row).getByText("Ticket error")).toBeTruthy();
    expect(within(row).getByText("3 days ago")).toBeTruthy();
    const prLink = within(row).getByRole("link", {
      name: "Update dependency foo to v2",
    });
    expect(prLink.getAttribute("rel")).toContain("noopener");
    const noTicketRow = within(openSection)
      .getByText("Update bar")
      .closest("tr") as HTMLElement;
    expect(within(noTicketRow).getByText("No ticket")).toBeTruthy();

    const mergedSection = screen.getByRole("region", {
      name: "Recently merged",
    });
    expect(await within(mergedSection).findByText("Update baz")).toBeTruthy();
    expect(within(mergedSection).getByText("DEV-9")).toBeTruthy();
    expect(within(mergedSection).getByText("Done")).toBeTruthy();
    expect(within(mergedSection).getByText(/Nov 14, 2026/)).toBeTruthy();

    const open = requests.find((r) => r.get("state") === "open");
    const merged = requests.find((r) => r.get("state") === "merged");
    expect(open?.get("sort")).toBe("-openedAt");
    expect(merged?.get("sort")).toBe("-mergedAt");
    expect(merged?.get("pageSize")).toBe("10");
  });

  it("offers no admin action on the page", async () => {
    serve(page([pr({ id: "pr-1" })]), page([]));
    renderPage();
    await screen.findByText("Update dependency foo to v2");

    expect(screen.queryByRole("button", { name: /run/i })).toBeNull();
    expect(screen.queryByText(/poll/i)).toBeNull();
  });

  it("sends the update type filter with the request and resets to the first page", async () => {
    serve(page([pr({ id: "pr-1" })]), page([]));
    renderPage();
    await screen.findByText("Update dependency foo to v2");

    openSelect("Update type");
    choose(await screen.findByRole("option", { name: "Major" }));

    await waitFor(() =>
      expect(
        requests.some(
          (r) => r.get("state") === "open" && r.get("updateType") === "major",
        ),
      ).toBe(true),
    );
    expect(
      requests.some(
        (r) => r.get("state") === "merged" && r.get("updateType") === "major",
      ),
    ).toBe(true);
    expect(
      requests
        .filter((r) => r.get("updateType") === "major")
        .every((r) => r.get("page") === "1"),
    ).toBe(true);
  });

  it("offers every repository the caller can see, also one with no pull request on screen", async () => {
    serveRepositories([
      { id: "repo-1", fullName: "acme/api" },
      { id: "repo-2", fullName: "acme/web" },
    ]);
    serve(page([pr({ id: "pr-1" })]), page([]));
    renderPage();
    await screen.findAllByText("Update dependency foo to v2");

    openSelect("Repository");
    choose(await screen.findByRole("option", { name: "acme/web" }));

    await waitFor(() =>
      expect(requests.some((r) => r.get("repositoryId") === "repo-2")).toBe(
        true,
      ),
    );
  });

  it("shows the empty state when there are no update PRs yet", async () => {
    serve(page([]), page([]));
    renderPage();

    expect(await screen.findByText("No update pull requests yet")).toBeTruthy();
    expect(
      screen
        .getByRole("link", { name: "View repositories" })
        .getAttribute("href"),
    ).toBe("/repositories");
  });

  it("shows loading placeholders until the lists arrive", async () => {
    serve(page([pr({ id: "pr-1" })]), page([]));
    renderPage();

    expect(screen.getAllByTestId("dashboard-skeleton").length).toBeGreaterThan(
      0,
    );
    await screen.findByText("Update dependency foo to v2");
    expect(screen.queryAllByTestId("dashboard-skeleton")).toHaveLength(0);
  });

  it("shows an error for a failed list without hiding the other one", async () => {
    serve(
      500,
      page([
        pr({
          id: "pr-3",
          number: 3,
          state: "merged",
          title: "Update baz",
          mergedAt: "2026-11-14T08:30:00.000Z",
        }),
      ]),
    );
    renderPage();

    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain(
      "Could not load the update pull requests.",
    );
    expect(screen.queryByText("No update pull requests yet")).toBeNull();
    expect(await screen.findByText("Update baz")).toBeTruthy();
  });
});
