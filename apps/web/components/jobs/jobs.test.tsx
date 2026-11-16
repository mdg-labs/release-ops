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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { JobsView } from "@/components/jobs/jobs-view";
import { ToastProvider } from "@/components/ui/toast";
import type {
  Job,
  JobDetail,
  JobsResponse,
  Problem,
  ProblemsResponse,
  UserRole,
} from "@/lib/query/types";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";
const API = "/api/go/api/v1";

let searchParams = new URLSearchParams();
const replaceMock = vi.fn();

vi.mock("next/navigation", () => ({
  usePathname: () => "/jobs",
  useRouter: () => ({ replace: replaceMock, push: vi.fn() }),
  useSearchParams: () => searchParams,
}));

vi.mock("next/link", () => ({
  default: ({
    children,
    href,
    ...props
  }: {
    children: React.ReactNode;
    href: string;
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

// Base UI's tabs and buttons read PointerEvent, which jsdom lacks.
if (!("PointerEvent" in window)) {
  Object.defineProperty(window, "PointerEvent", {
    configurable: true,
    value: MouseEvent,
  });
}

function job(overrides: Partial<Job> & { id: string }): Job {
  return {
    repository: { id: "r1", fullName: "acme/api" },
    kind: "renovate_run",
    state: "succeeded",
    priority: "normal",
    trigger: "schedule",
    matchedMarkers: [],
    attempts: 1,
    createdAt: "2026-11-16T08:00:00.000Z",
    startedAt: "2026-11-16T08:00:05.000Z",
    finishedAt: "2026-11-16T08:01:35.000Z",
    startLatencyMs: null,
    ...overrides,
  };
}

function jobs(items: Job[], total = items.length): JobsResponse {
  return { items, page: 1, pageSize: 10, total };
}

function problems(items: Problem[]): ProblemsResponse {
  return { items, page: 1, pageSize: 10, total: items.length };
}

describe("JobsView", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;
  let paths: string[];
  let writes: { method: string; path: string }[];

  beforeEach(() => {
    paths = [];
    writes = [];
    searchParams = new URLSearchParams();
    replaceMock.mockClear();
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

  const json = { headers: { "content-type": "application/json" } };

  // Answers requests of a method whose path is the given one or starts with it plus "?". The
  // reply is a status number (an error envelope) or a function of the request path.
  function serve(
    method: string,
    path: string,
    reply: number | object | ((requested: string) => object),
    status = 200,
  ) {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: (p: string) =>
          p === `${API}${path}` || p.startsWith(`${API}${path}?`),
        method,
      })
      .reply((opts) => {
        const requested = String(opts.path);
        if (method === "GET") {
          paths.push(requested.slice(API.length));
        } else {
          writes.push({ method, path: requested });
        }
        if (typeof reply === "number") {
          return {
            statusCode: reply,
            data: JSON.stringify({
              error: { code: "ERR", message: "refused" },
            }),
            responseOptions: json,
          };
        }
        return {
          statusCode: status,
          data: JSON.stringify(
            typeof reply === "function" ? reply(requested) : reply,
          ),
          responseOptions: json,
        };
      })
      .persist();
  }

  function session(role: UserRole) {
    serve("GET", "/auth/session", { user: { id: 1, login: "octocat", role } });
  }

  function repositories() {
    serve("GET", "/repositories", {
      items: [
        { id: "r1", fullName: "acme/api" },
        { id: "r2", fullName: "acme/web" },
      ],
      page: 1,
      pageSize: 100,
      total: 2,
    });
  }

  // Answers GET /repositories page by page: a page is a list of ids and a status.
  function serveRepositoryPages(
    total: number,
    pageOf: (page: number) => { ids: string[]; status?: number },
  ) {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: (p: string) => p.startsWith(`${API}/repositories?`),
        method: "GET",
      })
      .reply((opts) => {
        const requested = String(opts.path);
        paths.push(requested.slice(API.length));
        const page = Number(
          new URLSearchParams(requested.split("?")[1]).get("page"),
        );
        const { ids, status = 200 } = pageOf(page);
        return {
          statusCode: status,
          data: JSON.stringify(
            status === 200
              ? {
                  items: ids.map((id) => ({ id, fullName: `acme/${id}` })),
                  page,
                  pageSize: 100,
                  total,
                }
              : { error: { code: "ERR", message: "boom" } },
          ),
          responseOptions: json,
        };
      })
      .persist();
  }

  function renderView() {
    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    return render(
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <QueryClientProvider client={queryClient}>
          <ToastProvider>
            <JobsView />
          </ToastProvider>
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );
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

  function row(text: string): HTMLElement {
    return screen.getByText(text).closest("tr") as HTMLElement;
  }

  function openProblemsTab() {
    fireEvent.click(screen.getByRole("tab", { name: "Problems" }));
  }

  describe("job history", () => {
    it("lists each job with its trigger, markers, state, duration and latency", async () => {
      session("viewer");
      repositories();
      serve(
        "GET",
        "/jobs",
        jobs([
          job({
            id: "j1",
            trigger: "dashboard_checkbox",
            priority: "high",
            matchedMarkers: ["rebase-branch=renovate/foo-2.x"],
            startLatencyMs: 3200,
          }),
          job({
            id: "j2",
            repository: { id: "r2", fullName: "acme/web" },
            state: "failed",
            trigger: "run_now",
            attempts: 3,
          }),
          job({
            id: "j3",
            repository: { id: "r3", fullName: "acme/docs" },
            state: "queued",
            trigger: "push",
            startedAt: null,
            finishedAt: null,
          }),
        ]),
      );
      renderView();

      await screen.findByText("acme/api");
      const api = row("acme/api");
      expect(within(api).getByText("Succeeded")).toBeTruthy();
      expect(
        within(api).getByText("Dependency Dashboard checkbox"),
      ).toBeTruthy();
      expect(
        within(api).getByText("Markers: rebase-branch=renovate/foo-2.x"),
      ).toBeTruthy();
      expect(within(api).getByText("High")).toBeTruthy();
      expect(within(api).getByText("1m 30s")).toBeTruthy();
      expect(within(api).getByText("3s")).toBeTruthy();

      const web = row("acme/web");
      expect(within(web).getByText("Failed")).toBeTruthy();
      expect(within(web).getByText("Run now")).toBeTruthy();
      expect(within(web).getByText("3")).toBeTruthy();

      const docs = row("acme/docs");
      expect(within(docs).getByText("Queued")).toBeTruthy();
      expect(within(docs).getByText("Push")).toBeTruthy();
      expect(within(docs).getAllByText("Not yet")).toHaveLength(2);
    });

    it("filters by repository and state and goes back to the first page", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", (requested) =>
        requested.includes("repositoryId=r2")
          ? jobs([
              job({ id: "j2", repository: { id: "r2", fullName: "acme/web" } }),
            ])
          : jobs([job({ id: "j1" })], 30),
      );
      renderView();
      await screen.findByText("acme/api");

      fireEvent.click(screen.getByRole("button", { name: "Next" }));
      await waitFor(() =>
        expect(paths.some((p) => p.includes("page=2"))).toBe(true),
      );

      await waitFor(() =>
        expect(
          screen.getByRole("combobox", { name: "Repository" }),
        ).toBeTruthy(),
      );
      openSelect("Repository");
      choose(await screen.findByRole("option", { name: "acme/web" }));
      expect(
        await screen.findByText("acme/web", { selector: "td" }),
      ).toBeTruthy();
      const filtered = paths.filter((p) => p.includes("repositoryId=r2"));
      expect(filtered.at(-1)).toContain("page=1");

      openSelect("State");
      choose(await screen.findByRole("option", { name: "Failed" }));
      await waitFor(() =>
        expect(
          paths.some(
            (p) => p.includes("repositoryId=r2") && p.includes("state=failed"),
          ),
        ).toBe(true),
      );
    });

    it("offers every repository when more than one page of them exists", async () => {
      session("viewer");
      const ids = Array.from({ length: 101 }, (_, i) => `repo-${i + 1}`);
      serveRepositoryPages(101, (page) => ({
        ids: page === 1 ? ids.slice(0, 100) : ids.slice(100),
      }));
      serve("GET", "/jobs", jobs([job({ id: "j1" })]));
      renderView();
      await screen.findByText("acme/api");

      await waitFor(() =>
        expect(paths.some((p) => p.startsWith("/repositories?page=2"))).toBe(
          true,
        ),
      );
      await waitFor(() => {
        openSelect("Repository");
        expect(
          screen.getByRole("option", { name: "acme/repo-101" }),
        ).toBeTruthy();
      });
      expect(screen.getByRole("option", { name: "acme/repo-1" })).toBeTruthy();
    });

    it("offers no repository rather than a partial list when a later page fails", async () => {
      session("viewer");
      const ids = Array.from({ length: 100 }, (_, i) => `repo-${i + 1}`);
      serveRepositoryPages(101, (page) =>
        page === 1 ? { ids } : { ids: [], status: 500 },
      );
      serve("GET", "/jobs", jobs([job({ id: "j1" })]));
      renderView();
      await screen.findByText("acme/api");

      await waitFor(() =>
        expect(paths.some((p) => p.startsWith("/repositories?page=2"))).toBe(
          true,
        ),
      );
      openSelect("Repository");
      expect(
        await screen.findByRole("option", { name: "All repositories" }),
      ).toBeTruthy();
      expect(screen.queryByRole("option", { name: "acme/repo-1" })).toBeNull();
    });

    it("moves to the last page that exists when the jobs shrink under the reader", async () => {
      session("viewer");
      repositories();
      const all = Array.from({ length: 21 }, (_, i) =>
        job({
          id: `j${i + 1}`,
          repository: { id: "r1", fullName: `acme/job-${i + 1}` },
        }),
      );
      let shrunk = false;
      serve("GET", "/jobs", (requested) => {
        const page = Number(
          new URLSearchParams(requested.split("?")[1]).get("page"),
        );
        const total = shrunk ? 20 : 21;
        return jobs(
          all.slice(0, total).slice((page - 1) * 10, page * 10),
          total,
        );
      });
      renderView();
      await screen.findByText("acme/job-1");

      fireEvent.click(screen.getByRole("button", { name: "Next" }));
      await screen.findByText("acme/job-11");
      shrunk = true;
      fireEvent.click(screen.getByRole("button", { name: "Next" }));

      expect(await screen.findByText("11–20 of 20")).toBeTruthy();
      expect(screen.getByText("acme/job-20")).toBeTruthy();
      expect(screen.queryByText("No jobs match these filters.")).toBeNull();
      expect(
        (screen.getByRole("button", { name: "Previous" }) as HTMLButtonElement)
          .disabled,
      ).toBe(false);
    });

    it("shows the empty state when no job exists yet", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([]));
      renderView();

      expect(await screen.findByText("No jobs yet")).toBeTruthy();
      expect(
        screen
          .getByRole("link", { name: "Go to repositories" })
          .getAttribute("href"),
      ).toBe("/repositories");
    });

    it("says nothing matches a filter, not that no job exists", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", (requested) =>
        requested.includes("state=failed")
          ? jobs([])
          : jobs([job({ id: "j1" })]),
      );
      renderView();
      await screen.findByText("acme/api");

      openSelect("State");
      choose(await screen.findByRole("option", { name: "Failed" }));

      expect(
        await screen.findByText("No jobs match these filters."),
      ).toBeTruthy();
      expect(screen.queryByText("No jobs yet")).toBeNull();
    });

    it("shows a loading state, then a failure that can be retried", async () => {
      session("viewer");
      repositories();
      let answers = 0;
      mockAgent
        .get(ORIGIN)
        .intercept({
          path: (p: string) => p.startsWith(`${API}/jobs?`),
          method: "GET",
        })
        .reply(() => {
          answers += 1;
          return {
            statusCode: answers === 1 ? 500 : 200,
            data: JSON.stringify(
              answers === 1
                ? { error: { code: "ERR", message: "boom" } }
                : jobs([job({ id: "j1" })]),
            ),
            responseOptions: json,
          };
        })
        .persist();
      renderView();

      expect(screen.getAllByTestId("jobs-skeleton").length).toBeGreaterThan(0);
      expect(await screen.findByText("Could not load the jobs.")).toBeTruthy();
      expect(screen.queryByText("No jobs yet")).toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      expect(await screen.findByText("acme/api")).toBeTruthy();
      expect(screen.queryByText("Could not load the jobs.")).toBeNull();
    });
  });

  describe("job detail", () => {
    const detail: JobDetail = {
      ...job({ id: "j2", state: "failed", attempts: 2 }),
      runs: [
        {
          attempt: 1,
          startedAt: "2026-11-16T08:00:05.000Z",
          finishedAt: "2026-11-16T08:01:00.000Z",
          outcome: "failed",
          exitCode: 3,
          errorCode: "renovate_failed",
          errorMessage: "renovate exited with status 3",
          reportStatus: "ok",
          logExcerpt: "cloning with [REDACTED] done\nstack trace line",
        },
        {
          attempt: 2,
          startedAt: "2026-11-16T08:05:00.000Z",
          finishedAt: null,
          outcome: "aborted",
          exitCode: null,
          errorCode: null,
          errorMessage: null,
          reportStatus: null,
          logExcerpt: null,
        },
      ],
      events: [
        {
          at: "2026-11-16T08:01:00.000Z",
          kind: "retry_scheduled",
          message: "attempt 1 of 3 failed",
        },
      ],
    };

    it("opens from the table and shows the attempts, the log excerpt and the events", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([job({ id: "j2", state: "failed" })]));
      serve("GET", "/jobs/j2", detail);
      renderView();
      await screen.findByText("acme/api");

      fireEvent.click(
        screen.getByRole("button", { name: "View job acme/api" }),
      );

      const dialog = await screen.findByRole("dialog");
      expect(await within(dialog).findByText("Attempt 1")).toBeTruthy();
      expect(within(dialog).getByText("Exit code")).toBeTruthy();
      expect(
        within(dialog).getByText("renovate exited with status 3"),
      ).toBeTruthy();
      const log = within(dialog).getByLabelText("Log excerpt", {
        selector: "pre",
      });
      expect(log.textContent).toBe(
        "cloning with [REDACTED] done\nstack trace line",
      );
      expect(
        within(dialog).getByText("No log was stored for this attempt."),
      ).toBeTruthy();
      expect(within(dialog).getByText("retry_scheduled")).toBeTruthy();
      expect(within(dialog).getByText("attempt 1 of 3 failed")).toBeTruthy();
      expect(replaceMock).toHaveBeenLastCalledWith("/jobs?job=j2", {
        scroll: false,
      });

      fireEvent.click(
        within(dialog).getAllByRole("button", { name: "Close" })[0],
      );
      await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
      expect(replaceMock).toHaveBeenLastCalledWith("/jobs", { scroll: false });
    });

    it("opens a job from a deep link", async () => {
      searchParams = new URLSearchParams("job=j2");
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([job({ id: "j2", state: "failed" })]));
      serve("GET", "/jobs/j2", detail);
      renderView();

      const dialog = await screen.findByRole("dialog");
      expect(await within(dialog).findByText("Attempt 1")).toBeTruthy();
    });

    it("says a job is not available when the API answers 404, and shows a failure otherwise", async () => {
      searchParams = new URLSearchParams("job=gone");
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([]));
      serve("GET", "/jobs/gone", 404);
      renderView();

      expect(
        await screen.findByText(
          "This job does not exist or is not one of your repositories.",
        ),
      ).toBeTruthy();
      expect(screen.queryByText("Attempt 1")).toBeNull();
    });

    it("shows a failure to load a job with a retry", async () => {
      searchParams = new URLSearchParams("job=j9");
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([]));
      serve("GET", "/jobs/j9", 500);
      renderView();

      expect(await screen.findByText("Could not load this job.")).toBeTruthy();
      const dialog = screen.getByRole("dialog");
      expect(
        within(dialog).getByRole("button", { name: "Try again" }),
      ).toBeTruthy();
    });
  });

  describe("problems", () => {
    const jobFailed: Problem = {
      kind: "job_failed",
      repository: { id: "r1", fullName: "acme/api" },
      since: "2026-11-16T08:05:00.000Z",
      message: "renovate exited with status 3",
      ref: { jobId: "j2" },
    };
    const stuck: Problem = {
      kind: "ticket_failed",
      repository: { id: "r1", fullName: "acme/api" },
      since: "2026-11-16T09:00:00.000Z",
      message: "create ticket: unexpected status 503",
      ref: {
        updatePrId: "pr-1",
        updatePrUrl: "https://github.com/acme/api/pull/1",
        updateTicketId: "ut-1",
      },
    };
    const limited: Problem = {
      kind: "rate_limited",
      repository: null,
      since: "2026-11-16T10:00:00.000Z",
      message:
        "GitHub's rate limit is used up; jobs wait until 2026-11-16T11:00:00.000Z",
      ref: null,
    };
    const lost: Problem = {
      kind: "permissions_lost",
      repository: null,
      since: "2026-11-16T10:30:00.000Z",
      message: "The GitHub App installation lacks permissions: contents",
      ref: null,
    };

    it("lists every kind for an admin, with the repository, time, message and a link", async () => {
      session("admin");
      repositories();
      serve("GET", "/jobs", jobs([job({ id: "j1" })]));
      serve("GET", "/problems", problems([lost, limited, stuck, jobFailed]));
      renderView();
      await screen.findByText("acme/api");

      openProblemsTab();

      expect(await screen.findByText("Permissions lost")).toBeTruthy();
      expect(screen.getByText("Rate limited")).toBeTruthy();
      expect(screen.getByText("Ticket error")).toBeTruthy();
      expect(screen.getByText("Job failed")).toBeTruthy();
      expect(
        screen.getByText(
          "The GitHub App installation lacks permissions: contents",
        ),
      ).toBeTruthy();
      expect(screen.getAllByText("All repositories").length).toBeGreaterThan(0);
      const failedRow = screen
        .getByText("Job failed")
        .closest("tr") as HTMLElement;
      expect(within(failedRow).getByText("acme/api")).toBeTruthy();
      expect(within(failedRow).getByText(/Nov 16, 2026/)).toBeTruthy();
      expect(
        within(failedRow).getByText("renovate exited with status 3"),
      ).toBeTruthy();
      const ticketRow = screen
        .getByText("Ticket error")
        .closest("tr") as HTMLElement;
      const link = within(ticketRow).getByRole("link", {
        name: "View pull request",
      });
      expect(link.getAttribute("href")).toBe(
        "https://github.com/acme/api/pull/1",
      );
      expect(link.getAttribute("rel")).toContain("noopener");
      expect(replaceMock).toHaveBeenLastCalledWith("/jobs?view=problems", {
        scroll: false,
      });
    });

    it("opens the failed job from its problem", async () => {
      searchParams = new URLSearchParams("view=problems");
      session("admin");
      repositories();
      serve("GET", "/jobs", jobs([]));
      serve("GET", "/problems", problems([jobFailed]));
      serve("GET", "/jobs/j2", {
        ...job({ id: "j2", state: "failed" }),
        runs: [],
        events: [],
      });
      renderView();

      fireEvent.click(await screen.findByRole("button", { name: "View job" }));

      const dialog = await screen.findByRole("dialog");
      expect(
        await within(dialog).findByText("No attempt has finished yet."),
      ).toBeTruthy();
    });

    it("offers an admin the clear action only on a stuck pending ticket, after a confirmation naming the tracker check", async () => {
      session("admin");
      repositories();
      serve("GET", "/jobs", jobs([]));
      serve("GET", "/problems", problems([jobFailed, stuck]));
      serve("POST", "/update-tickets/ut-1/clear-pending-marker", {
        id: "ut-1",
      });
      renderView();
      openProblemsTab();
      await screen.findByText("Ticket error");

      expect(
        screen.getAllByRole("button", { name: "Clear pending marker" }),
      ).toHaveLength(1);
      const jobRow = screen
        .getByText("Job failed")
        .closest("tr") as HTMLElement;
      expect(
        within(jobRow).queryByRole("button", { name: "Clear pending marker" }),
      ).toBeNull();

      fireEvent.click(
        screen.getByRole("button", { name: "Clear pending marker" }),
      );

      const dialog = await screen.findByRole("alertdialog");
      expect(dialog.textContent).toContain("Release Ops key: ut-1");
      expect(dialog.textContent).toContain("duplicates one that exists");
      expect(writes).toHaveLength(0);

      fireEvent.click(
        within(dialog).getByRole("button", { name: "Clear marker" }),
      );
      await waitFor(() => expect(writes).toHaveLength(1));
      expect(writes[0]).toEqual({
        method: "POST",
        path: `${API}/update-tickets/ut-1/clear-pending-marker`,
      });
      expect(
        await screen.findByText("Pending marker cleared for acme/api"),
      ).toBeTruthy();
      await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
      // The problem list is read again, so the cleared ticket leaves it.
      await waitFor(() =>
        expect(
          paths.filter((p) => p.startsWith("/problems?")).length,
        ).toBeGreaterThan(1),
      );
    });

    it("keeps the dialog open and says why when the API refuses the clear", async () => {
      session("admin");
      repositories();
      serve("GET", "/jobs", jobs([]));
      serve("GET", "/problems", problems([stuck]));
      serve("POST", "/update-tickets/ut-1/clear-pending-marker", 409);
      renderView();
      openProblemsTab();
      fireEvent.click(
        await screen.findByRole("button", { name: "Clear pending marker" }),
      );

      const dialog = await screen.findByRole("alertdialog");
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Clear marker" }),
      );

      expect(
        await within(dialog).findByText(
          "The marker could not be cleared: refused",
        ),
      ).toBeTruthy();
      expect(
        screen.queryByText("Pending marker cleared for acme/api"),
      ).toBeNull();
    });

    it("gives a viewer the problem without the error text or the clear action", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([]));
      // A viewer's response carries no message and no ticket id; even a response that did would
      // not get the action, because only an admin is offered it.
      serve(
        "GET",
        "/problems",
        problems([
          {
            ...stuck,
            message: null,
            ref: { ...stuck.ref, updateTicketId: "ut-1" },
          },
          { ...jobFailed },
        ]),
      );
      renderView();
      openProblemsTab();

      expect(
        await screen.findByText("Only admins see the error text"),
      ).toBeTruthy();
      expect(screen.queryByText(/unexpected status 503/)).toBeNull();
      expect(
        screen.queryByRole("button", { name: "Clear pending marker" }),
      ).toBeNull();
      expect(screen.getByText("renovate exited with status 3")).toBeTruthy();
    });

    it("shows the empty state, a loading state and a failure that can be retried", async () => {
      session("viewer");
      repositories();
      serve("GET", "/jobs", jobs([]));
      let answers = 0;
      mockAgent
        .get(ORIGIN)
        .intercept({
          path: (p: string) => p.startsWith(`${API}/problems?`),
          method: "GET",
        })
        .reply(() => {
          answers += 1;
          return {
            statusCode: answers === 1 ? 500 : 200,
            data: JSON.stringify(
              answers === 1
                ? { error: { code: "ERR", message: "boom" } }
                : problems([]),
            ),
            responseOptions: json,
          };
        })
        .persist();
      renderView();
      openProblemsTab();

      expect(screen.getAllByTestId("problems-skeleton").length).toBeGreaterThan(
        0,
      );
      expect(
        await screen.findByText("Could not load the problems."),
      ).toBeTruthy();
      expect(screen.queryByText("No problems")).toBeNull();

      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
      expect(await screen.findByText("No problems")).toBeTruthy();
      expect(
        screen.getByText("Nothing needs attention right now."),
      ).toBeTruthy();
    });
  });
});
