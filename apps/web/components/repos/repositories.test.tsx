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
import { RepositoriesView } from "@/components/repos/repositories-view";
import { ToastProvider } from "@/components/ui/toast";
import type {
  RepositoriesResponse,
  Repository,
  UserRole,
} from "@/lib/query/types";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";
const API = "/api/go/api/v1";

// Base UI's checkbox and switch read PointerEvent when clicked, which jsdom lacks.
if (!("PointerEvent" in window)) {
  Object.defineProperty(window, "PointerEvent", {
    configurable: true,
    value: MouseEvent,
  });
}

function repo(
  overrides: Partial<Repository> & { id: string; fullName: string },
): Repository {
  return {
    defaultBranch: "main",
    enabled: true,
    configState: "present",
    htmlUrl: `https://github.com/${overrides.fullName}`,
    dashboardUrl: null,
    openUpdatePrs: 0,
    lastJob: null,
    ticketProjectId: null,
    effectiveTicketProjectId: "tp-dev",
    effectiveTicketProjectName: "Jira — DEV",
    ...overrides,
  };
}

function list(items: Repository[], total = items.length): RepositoriesResponse {
  return { items, page: 1, pageSize: 25, total };
}

type Call = { method: string; path: string; body: unknown };

describe("RepositoriesView", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;
  let calls: Call[];

  beforeEach(() => {
    calls = [];
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

  // Answers a request of the method on a path (a string prefix of the path and query), and
  // records it.
  function serve(
    method: string,
    path: string,
    reply: number | object,
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
        const raw = typeof opts.body === "string" ? opts.body : "";
        calls.push({
          method,
          path: String(opts.path),
          body: raw ? JSON.parse(raw) : null,
        });
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
          data: JSON.stringify(reply),
          responseOptions: json,
        };
      })
      .persist();
  }

  function session(role: UserRole) {
    serve("GET", "/auth/session", { user: { id: 1, login: "octocat", role } });
  }

  function admin(items: Repository[]) {
    session("admin");
    serve("GET", "/repositories", list(items));
    serve("GET", "/ticket-projects", [
      { id: "tp-dev", name: "Jira — DEV" },
      { id: "tp-ops", name: "Jira — OPS" },
    ]);
    serve("GET", "/installations", {
      items: [
        {
          id: "inst-1",
          status: "active",
          defaultTicketProjectId: "tp-dev",
        },
      ],
    });
    serve("GET", "/settings", {
      concurrencyCap: 2,
      mode: "webhook",
      presetRepositories: ["acme/presets"],
      globalConfig: { source: null, repository: null, ref: null },
    });
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
            <RepositoriesView />
          </ToastProvider>
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );
  }

  function row(name: string): HTMLElement {
    return screen
      .getByRole("link", { name, hidden: true })
      .closest("tr") as HTMLElement;
  }

  const writes = () => calls.filter((c) => c.method !== "GET");

  it("lists each repository with its config, open PRs, last job and dashboard link", async () => {
    admin([
      repo({
        id: "r1",
        fullName: "acme/api",
        openUpdatePrs: 3,
        dashboardUrl: "https://github.com/acme/api/issues/7",
        lastJob: {
          id: "j1",
          state: "succeeded",
          finishedAt: "2026-11-14T12:00:00.000Z",
        },
      }),
      repo({
        id: "r2",
        fullName: "acme/web",
        configState: "missing",
        lastJob: { id: "j2", state: "running", finishedAt: null },
      }),
      repo({ id: "r3", fullName: "acme/docs", configState: "unknown" }),
    ]);
    renderView();

    await screen.findByRole("link", { name: "acme/api" });
    const api = row("acme/api");
    expect(
      within(api).getByRole("link", { name: "acme/api" }).getAttribute("href"),
    ).toBe("https://github.com/acme/api");
    expect(within(api).getByText("Present")).toBeTruthy();
    expect(within(api).getByText("3")).toBeTruthy();
    expect(within(api).getByText("Succeeded")).toBeTruthy();
    expect(within(api).getByText(/Nov 14, 2026/)).toBeTruthy();
    const dashboard = within(api).getByRole("link", { name: "Open" });
    expect(dashboard.getAttribute("href")).toBe(
      "https://github.com/acme/api/issues/7",
    );
    expect(dashboard.getAttribute("rel")).toContain("noopener");

    const web = row("acme/web");
    expect(within(web).getByText("Missing")).toBeTruthy();
    expect(within(web).getByText("Running")).toBeTruthy();
    expect(within(web).queryByRole("link", { name: "Open" })).toBeNull();
    expect(within(web).getByText("Unknown")).toBeTruthy();

    const docs = row("acme/docs");
    expect(within(docs).getAllByText("Unknown").length).toBe(2);
    expect(within(docs).getByText("No job yet")).toBeTruthy();
  });

  it("runs now and reports the queued run, with the failure when the API refuses", async () => {
    admin([repo({ id: "r1", fullName: "acme/api" })]);
    serve("POST", "/repositories/r1/run", { jobId: "job-1" }, 202);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    fireEvent.click(screen.getByRole("button", { name: "Run now" }));

    expect(await screen.findByText("Run queued for acme/api")).toBeTruthy();
    expect(
      screen.getByRole("link", { name: "View jobs" }).getAttribute("href"),
    ).toBe("/jobs");
    expect(writes().map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /api/go/api/v1/repositories/r1/run",
    ]);
  });

  it("shows why a run could not be started", async () => {
    admin([repo({ id: "r1", fullName: "acme/api" })]);
    serve("POST", "/repositories/r1/run", 409);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    fireEvent.click(screen.getByRole("button", { name: "Run now" }));

    expect(
      await screen.findByText("Could not start a run for acme/api: refused"),
    ).toBeTruthy();
    expect(screen.queryByText("Run queued for acme/api")).toBeNull();
  });

  it("disables run now for a paused repository", async () => {
    admin([repo({ id: "r1", fullName: "acme/api", enabled: false })]);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    expect(
      (screen.getByRole("button", { name: "Run now" }) as HTMLButtonElement)
        .disabled,
    ).toBe(true);
  });

  it("pauses a repository and leaves the switch alone when the API refuses", async () => {
    admin([repo({ id: "r1", fullName: "acme/api" })]);
    serve("PATCH", "/repositories/r1", 500);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });
    const toggle = screen.getByRole("switch", {
      name: "Run Renovate for acme/api",
    });
    expect(toggle.getAttribute("aria-checked")).toBe("true");

    fireEvent.click(toggle);

    expect(
      await screen.findByText("Could not change acme/api: refused"),
    ).toBeTruthy();
    expect(writes()[0]).toMatchObject({
      method: "PATCH",
      body: { enabled: false },
    });
    expect(
      screen
        .getByRole("switch", { name: "Run Renovate for acme/api" })
        .getAttribute("aria-checked"),
    ).toBe("true");
  });

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

  it("sets and clears the ticket project override, naming what the default resolves to", async () => {
    admin([repo({ id: "r1", fullName: "acme/api" })]);
    serve(
      "PATCH",
      "/repositories/r1",
      repo({ id: "r1", fullName: "acme/api" }),
    );
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    const select = await screen.findByRole("combobox", {
      name: "Ticket project of acme/api",
    });
    expect(select.textContent).toContain("Installation default (Jira — DEV)");

    openSelect("Ticket project of acme/api");
    choose(await screen.findByRole("option", { name: "Jira — OPS" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toMatchObject({
      method: "PATCH",
      body: { ticketProjectId: "tp-ops" },
    });
  });

  it("warns about a repository that resolves to no ticket project", async () => {
    admin([
      repo({
        id: "r1",
        fullName: "acme/api",
        effectiveTicketProjectId: null,
        effectiveTicketProjectName: null,
      }),
      repo({ id: "r2", fullName: "acme/web" }),
    ]);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    const warning =
      "No ticket project: its update pull requests get no tickets.";
    expect(await within(row("acme/api")).findByText(warning)).toBeTruthy();
    expect(within(row("acme/web")).queryByText(warning)).toBeNull();
  });

  it("offers Enable repo only for a repository without a config, confirming the preset", async () => {
    admin([
      repo({ id: "r1", fullName: "acme/api", configState: "present" }),
      repo({ id: "r2", fullName: "acme/web", configState: "missing" }),
    ]);
    serve(
      "POST",
      "/repositories/r2/enable-renovate",
      { pullRequestUrl: "https://github.com/acme/web/pull/9", number: 9 },
      201,
    );
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    expect(
      within(row("acme/api")).queryByRole("button", { name: "Enable repo" }),
    ).toBeNull();
    fireEvent.click(
      within(row("acme/web")).getByRole("button", { name: "Enable repo" }),
    );

    expect(
      await screen.findByText(/extending local>acme\/presets/),
    ).toBeTruthy();
    expect(writes()).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Open pull request" }));

    const prLink = await within(row("acme/web")).findByRole("link", {
      name: "Pull request #9",
    });
    expect(prLink.getAttribute("href")).toBe(
      "https://github.com/acme/web/pull/9",
    );
    expect(
      within(row("acme/web")).queryByRole("button", { name: "Enable repo" }),
    ).toBeNull();
    expect(writes().map((c) => `${c.method} ${c.path}`)).toEqual([
      "POST /api/go/api/v1/repositories/r2/enable-renovate",
    ]);
  });

  it("shows the error and no pull request when opening it fails", async () => {
    admin([repo({ id: "r2", fullName: "acme/web", configState: "missing" })]);
    serve("POST", "/repositories/r2/enable-renovate", 502);
    renderView();
    await screen.findByRole("link", { name: "acme/web" });

    fireEvent.click(screen.getByRole("button", { name: "Enable repo" }));
    await screen.findByRole("alertdialog");
    fireEvent.click(screen.getByRole("button", { name: "Open pull request" }));

    expect(
      await screen.findByText("The pull request could not be opened: refused"),
    ).toBeTruthy();
    expect(screen.queryByRole("link", { name: /Pull request #/ })).toBeNull();
    expect(
      screen.getByRole("switch", { hidden: true }).getAttribute("aria-checked"),
    ).toBe("true");
    expect(writes().every((c) => c.method === "POST")).toBe(true);
  });

  it("gives a viewer the state and no action, and reads no admin endpoint", async () => {
    session("viewer");
    serve(
      "GET",
      "/repositories",
      list([
        {
          ...repo({
            id: "r1",
            fullName: "acme/api",
            enabled: false,
            configState: "missing",
          }),
          ticketProjectId: undefined,
          effectiveTicketProjectId: undefined,
          effectiveTicketProjectName: undefined,
        },
      ]),
    );
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    expect(screen.getByText("Paused")).toBeTruthy();
    expect(within(row("acme/api")).queryByRole("button")).toBeNull();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(
      screen.queryByRole("combobox", { name: /Ticket project/ }),
    ).toBeNull();
    expect(screen.queryByText(/No ticket project/)).toBeNull();
    const paths = calls.map((c) => c.path);
    expect(paths.some((p) => p.includes("/ticket-projects"))).toBe(false);
    expect(paths.some((p) => p.includes("/settings"))).toBe(false);
    expect(paths.some((p) => p.includes("/installations"))).toBe(false);
  });

  it("shows a skeleton while loading", () => {
    admin([]);
    renderView();
    expect(
      screen.getAllByTestId("repositories-skeleton").length,
    ).toBeGreaterThan(0);
  });

  it("points an admin to the GitHub App installation when no repository is installed", async () => {
    admin([]);
    serve("GET", "/github-app", {
      app: { htmlUrl: "https://github.com/apps/release-ops" },
      installation: null,
    });
    renderView();

    expect(await screen.findByText("No repositories yet")).toBeTruthy();
    const link = await screen.findByRole("link", {
      name: "Install the GitHub App on repositories",
    });
    expect(link.getAttribute("href")).toBe(
      "https://github.com/apps/release-ops/installations/new",
    );
  });

  it("tells a viewer there is nothing to see, without an install link", async () => {
    session("viewer");
    serve("GET", "/repositories", list([]));
    renderView();

    expect(await screen.findByText("No repositories yet")).toBeTruthy();
    expect(
      screen.getByText("There are no repositories you can see yet."),
    ).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
    expect(calls.some((c) => c.path.includes("/github-app"))).toBe(false);
  });

  it("shows an error with a retry when the list cannot be loaded, never an empty list", async () => {
    session("admin");
    serve("GET", "/repositories", 500);
    renderView();

    expect(
      await screen.findByText("Could not load the repositories."),
    ).toBeTruthy();
    expect(screen.queryByText("No repositories yet")).toBeNull();
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy();
  });

  it("searches by name and starts again from the first page", async () => {
    admin([repo({ id: "r1", fullName: "acme/api" })]);
    renderView();
    await screen.findByRole("link", { name: "acme/api" });

    fireEvent.change(
      screen.getByRole("searchbox", { name: "Search repositories" }),
      {
        target: { value: "web" },
      },
    );

    await waitFor(() =>
      expect(
        calls.some(
          (c) => c.path.includes("search=web") && c.path.includes("page=1"),
        ),
      ).toBe(true),
    );
  });
});
