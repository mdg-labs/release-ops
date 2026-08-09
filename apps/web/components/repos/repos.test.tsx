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
import { ReposView } from "@/components/repos/repos-view";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const sampleIntegrations = [
  {
    id: "int-github",
    kind: "github",
    name: "GitHub Org",
    baseUrl: null,
    hasSecret: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
  {
    id: "int-gitlab",
    kind: "gitlab",
    name: "Self-hosted GitLab",
    baseUrl: "https://gitlab.example.com",
    hasSecret: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

const sampleTicketProjects = [
  {
    id: "tp-1",
    integrationId: "int-jira",
    externalProjectId: "DEV",
    name: "Jira — DEV",
    createConfig: {},
    statusMapping: {},
    onOpenTicketPolicy: "supersede",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

const sampleNotificationTargets = [
  {
    id: "nt-1",
    name: "Slack alerts",
    hasSecret: true,
    events: ["create", "error", "supersede"],
    enabled: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

const sampleRepos = [
  {
    id: "repo-1",
    sourceKind: "github",
    projectPath: "org/app",
    enabled: true,
    sourceIntegrationId: null,
    ticketProjectId: "tp-1",
    notificationTargetIds: [],
    openTicketExternalId: null,
    openTicketTag: null,
    lastKnownTag: "v1.0.0",
    lastPolledAt: "2026-01-02T12:00:00.000Z",
    lastError: null,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-02T12:00:00.000Z",
  },
  {
    id: "repo-2",
    sourceKind: "gitlab",
    projectPath: "group/service",
    enabled: false,
    sourceIntegrationId: "int-gitlab",
    ticketProjectId: "tp-1",
    notificationTargetIds: ["nt-1"],
    openTicketExternalId: null,
    openTicketTag: null,
    lastKnownTag: null,
    lastPolledAt: null,
    lastError: "rate limit exceeded",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

function renderReposPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
      <QueryClientProvider client={queryClient}>
        <ReposView />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

function mockListEndpoints(
  pool: ReturnType<MockAgent["get"]>,
  repos = sampleRepos,
) {
  pool
    .intercept({ path: "/api/go/api/v1/repos", method: "GET" })
    .reply(200, repos);
  pool
    .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
    .reply(200, sampleTicketProjects);
  pool
    .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
    .reply(200, sampleIntegrations);
  pool
    .intercept({
      path: "/api/go/api/v1/notification-targets",
      method: "GET",
    })
    .reply(200, sampleNotificationTargets);
}

describe("ReposView", () => {
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

  it("renders empty state when no repos exist", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool, []);

    renderReposPage();

    expect(
      await screen.findByText("No monitored repos yet"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Add a repository to monitor for new releases."),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Add repo" })).toHaveLength(2);
  });

  it("lists repos with last polled and last error columns", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool);

    renderReposPage();

    expect(await screen.findByText("org/app")).toBeInTheDocument();
    expect(screen.getByText("group/service")).toBeInTheDocument();
    expect(screen.getAllByText("Jira — DEV")).toHaveLength(2);
    expect(screen.getByText("rate limit exceeded")).toBeInTheDocument();
    expect(screen.getByText("Never")).toBeInTheDocument();
    expect(screen.getByText("Self-hosted GitLab")).toBeInTheDocument();
  });

  it("shows source integration field when editing gitlab repo", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool);

    renderReposPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit group/service" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Source integration")).toBeInTheDocument();
    expect(within(dialog).getByText("Self-hosted GitLab")).toBeInTheDocument();
  });

  it("hides source integration select for github", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool, []);

    renderReposPage();

    fireEvent.click(await screen.findByRole("button", { name: "Add repo" }));

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).queryByRole("combobox", { name: "Source integration" }),
    ).not.toBeInTheDocument();
  });

  it("opens edit dialog with existing repo values", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool);

    renderReposPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit group/service" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText(/Project path/)).toHaveValue(
      "group/service",
    );
    expect(within(dialog).getByText("Self-hosted GitLab")).toBeInTheDocument();
    expect(
      within(dialog).getByRole("switch", { name: "Enabled" }),
    ).toHaveAttribute("aria-checked", "false");
    expect(within(dialog).getByText("Slack alerts")).toBeInTheDocument();
  });

  it("confirms delete in alert dialog", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockListEndpoints(pool);

    pool
      .intercept({
        path: "/api/go/api/v1/repos/repo-1",
        method: "DELETE",
      })
      .reply(204);

    pool
      .intercept({ path: "/api/go/api/v1/repos", method: "GET" })
      .reply(200, [sampleRepos[1]]);

    pool
      .intercept({ path: "/api/go/api/v1/status", method: "GET" })
      .reply(200, {
        pollIntervalMinutes: 360,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    renderReposPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete org/app" }),
    );

    const alertDialog = await screen.findByRole("alertdialog");
    expect(
      within(alertDialog).getByText(
        /Delete .org\/app.?\? This cannot be undone\./,
      ),
    ).toBeInTheDocument();

    fireEvent.click(
      within(alertDialog).getByRole("button", { name: "Delete" }),
    );

    await waitFor(() => {
      expect(screen.queryByText("org/app")).not.toBeInTheDocument();
    });
  });
});
