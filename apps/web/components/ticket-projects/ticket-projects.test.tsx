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
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { TicketProjectsView } from "@/components/ticket-projects/ticket-projects-view";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const ticketIntegrations = [
  {
    id: "int-jira",
    kind: "jira",
    name: "Company Jira",
    baseUrl: "https://company.atlassian.net",
    hasSecret: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
  {
    id: "int-github",
    kind: "github",
    name: "GitHub Org",
    baseUrl: null,
    hasSecret: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

const sampleProjects = [
  {
    id: "tp-1",
    integrationId: "int-jira",
    externalProjectId: "DEV",
    name: "Jira — DEV",
    createConfig: {
      issueType: "Task",
      priority: "Medium",
      initialStatus: "To Do",
    },
    statusMapping: {
      open: ["To Do", "In Progress"],
      done: ["Done"],
      cancelled: ["Cancelled"],
      superseded: "Cancelled",
    },
    onOpenTicketPolicy: "supersede",
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

function renderTicketProjectsPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <QueryClientProvider client={queryClient}>
        <TicketProjectsView />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("TicketProjectsView", () => {
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
    vi.restoreAllMocks();
  });

  it("lists ticket projects grouped by integration", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, sampleProjects);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    expect(await screen.findByText("Jira — DEV")).toBeInTheDocument();
    expect(screen.getByText("Company Jira")).toBeInTheDocument();
    expect(screen.getByText("Supersede")).toBeInTheDocument();
  });

  it("shows only ticket integrations in create drawer", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, []);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add ticket project" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Company Jira")).toBeInTheDocument();
    expect(within(dialog).queryByText("GitHub Org")).not.toBeInTheDocument();
  });

  it("shows API validation errors on save", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, []);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "POST",
      })
      .reply(400, {
        error: {
          code: "VALIDATION_ERROR",
          message: "statusMapping must be valid JSON",
        },
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add ticket project" }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/External project ID/), {
      target: { value: "DEV" },
    });
    fireEvent.change(within(dialog).getByLabelText(/Display name/), {
      target: { value: "Jira — DEV" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Open statuses/), {
      target: { value: "To Do, In Progress" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Done statuses/), {
      target: { value: "Done" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Cancelled statuses/), {
      target: { value: "Cancelled" },
    });
    fireEvent.change(within(dialog).getByLabelText(/^Superseded status/), {
      target: { value: "Cancelled" },
    });

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(
      await within(dialog).findByText("statusMapping must be valid JSON"),
    ).toBeInTheDocument();
  });

  it("shows delete confirmation copy", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, sampleProjects);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete Jira — DEV" }),
    );

    expect(
      await screen.findByText("Delete “Jira — DEV”? This cannot be undone."),
    ).toBeInTheDocument();
  });

  it("pre-fills status mapping when editing", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, sampleProjects);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText(/^Open statuses/)).toHaveValue(
      "To Do, In Progress",
    );
    expect(within(dialog).getByLabelText(/^Superseded status/)).toHaveValue(
      "Cancelled",
    );
    await waitFor(() =>
      expect(within(dialog).getByLabelText(/Issue type/)).toHaveValue("Task"),
    );
  });
});
