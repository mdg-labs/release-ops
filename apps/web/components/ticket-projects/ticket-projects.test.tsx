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
import { integrationDefaultTemplates } from "@/lib/ticket-projects/content-templates";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

// Base UI's checkbox and switch read PointerEvent when clicked, which jsdom lacks.
if (!("PointerEvent" in window)) {
  Object.defineProperty(window, "PointerEvent", {
    configurable: true,
    value: MouseEvent,
  });
}

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
    contentTemplates: {
      title: "{{ .PullRequest.Title }} in {{ .Repository }}",
      description: "**Notes:** {{ .ReleaseNotes }}",
      updateComment: "Updated: {{ .PullRequest.URL }}",
      supersedeComment: "Superseded by {{ .Supersede.NewPullRequest.URL }}",
    },
    filter: { updateTypes: ["major"], labels: ["deps"], skipAutomerged: false },
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
    const mappingPool = mockAgent.get(ORIGIN);
    mappingPool
      .intercept({ path: "/api/go/api/v1/installations", method: "GET" })
      .reply(200, { items: [] })
      .persist();
    mappingPool
      .intercept({
        path: /^\/api\/go\/api\/v1\/repositories\?/,
        method: "GET",
      })
      .reply(200, { items: [], page: 1, pageSize: 25, total: 0 })
      .persist();

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

  it("renders empty state when no ticket projects exist", async () => {
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

    expect(
      await screen.findByText("No ticket projects yet"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Define a project target with status mapping and ticket content.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "Add ticket project" }),
    ).toHaveLength(2);
  });

  it("shows the forbidden state, not the load error, when the projects request is refused with 403", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(403, { error: { code: "FORBIDDEN", message: "admin only" } });
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    expect(await screen.findByTestId("forbidden-state")).toHaveTextContent(
      "You do not have access to this page",
    );
    expect(
      screen.queryByText("Failed to load ticket projects."),
    ).not.toBeInTheDocument();
  });

  it("shows the load error, not the forbidden state, when the projects request fails otherwise", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(500, {
        error: { code: "INTERNAL", message: "database is locked" },
      });
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Failed to load ticket projects.",
    );
    expect(screen.queryByTestId("forbidden-state")).not.toBeInTheDocument();
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
    expect(
      screen.queryByRole("columnheader", { name: "Policy" }),
    ).not.toBeInTheDocument();
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

  it("shows top-level form tabs in create drawer", async () => {
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
    expect(
      within(dialog).getByRole("tab", { name: "General" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "Filter" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "Create config" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "Status mapping" }),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    ).toBeInTheDocument();
  });

  it("uses a wider drawer for ticket projects", async () => {
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
    expect(dialog.className).toContain("max-w-4xl");
  });

  it("switches to the tab with validation errors on save", async () => {
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
    fireEvent.change(within(dialog).getByLabelText(/Display name/), {
      target: { value: "" },
    });
    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(
      await within(dialog).findByText("Display name is required."),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "General" }),
    ).toHaveAttribute("data-active");
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
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, { items: [] });
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
    fireEvent.change(within(dialog).getByLabelText(/Display name/), {
      target: { value: "Jira — DEV" },
    });

    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(
      await within(dialog).findByText("External project ID is required."),
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

  it("keeps the delete dialog open and explains an in-use ticket project", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: `/api/go/api/v1/ticket-projects/${sampleProjects[0].id}`,
        method: "DELETE",
      })
      .reply(409, {
        error: {
          code: "CONFLICT",
          message: "ticket project is in use",
        },
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete Jira — DEV" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText(messages["ticket-projects"].deleteInUse),
    ).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    await waitFor(() => {
      expect(
        within(dialog).getByRole("button", { name: "Delete" }),
      ).toBeEnabled();
    });
  });

  it("shows the server message when deleting a ticket project fails", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: `/api/go/api/v1/ticket-projects/${sampleProjects[0].id}`,
        method: "DELETE",
      })
      .reply(500, {
        error: { code: "INTERNAL", message: "database is locked" },
      });
    pool
      .intercept({
        path: `/api/go/api/v1/ticket-projects/${sampleProjects[0].id}`,
        method: "DELETE",
      })
      .replyWithError(new Error("socket hang up"));

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete Jira — DEV" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText("database is locked"),
    ).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText(messages["ticket-projects"].deleteFailed),
    ).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("drops a delete failure that settles after its dialog was closed", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: `/api/go/api/v1/ticket-projects/${sampleProjects[0].id}`,
        method: "DELETE",
      })
      .reply(500, {
        error: { code: "INTERNAL", message: "database is locked" },
      })
      .delay(200);

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete Jira — DEV" }),
    );
    const first = await screen.findByRole("alertdialog");
    fireEvent.click(within(first).getByRole("button", { name: "Delete" }));
    fireEvent.click(within(first).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole("button", { name: "Delete Jira — DEV" }));
    const second = await screen.findByRole("alertdialog");
    await waitFor(() => {
      expect(
        within(second).getByRole("button", { name: "Delete" }),
      ).toBeEnabled();
    });

    expect(within(second).queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("shows checkbox-style hints under the status mapping lists", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, {
        items: [
          { id: "To Do", name: "To Do" },
          { id: "Done", name: "Done" },
        ],
      })
      .persist();

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Status mapping" }),
    );

    expect(
      await within(dialog).findByText(
        "Select every status that counts as open.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText("Select every status that counts as done."),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText("Select every status that counts as cancelled."),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByText("Comma-separated status values."),
    ).not.toBeInTheDocument();
  });

  it("names each status checkbox by its status and each group by its label", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, {
        items: [
          { id: "To Do", name: "To Do" },
          { id: "Done", name: "Done" },
        ],
      })
      .persist();

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Status mapping" }),
    );

    for (const groupName of [
      "Open statuses",
      "Done statuses",
      "Cancelled statuses",
    ]) {
      const group = await within(dialog).findByRole("group", {
        name: groupName,
      });
      expect(
        within(group).getByRole("checkbox", { name: "To Do" }),
      ).toBeInTheDocument();
      expect(
        within(group).getByRole("checkbox", { name: "Done" }),
      ).toBeInTheDocument();
      expect(within(group).getAllByRole("checkbox")).toHaveLength(2);
    }
  });

  it("pre-fills edit drawer values", async () => {
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
    expect(within(dialog).getByLabelText(/Display name/)).toHaveValue(
      "Jira — DEV",
    );
    expect(within(dialog).getByText("DEV")).toBeInTheDocument();

    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    );

    expect(within(dialog).getByLabelText("Title template")).toHaveValue(
      "{{ .PullRequest.Title }} in {{ .Repository }}",
    );
    expect(within(dialog).getByLabelText("Description template")).toHaveValue(
      "**Notes:** {{ .ReleaseNotes }}",
    );
    expect(
      within(dialog).getByLabelText("Update comment template"),
    ).toHaveValue("Updated: {{ .PullRequest.URL }}");
    expect(
      within(dialog).getByLabelText("Supersede comment template"),
    ).toHaveValue("Superseded by {{ .Supersede.NewPullRequest.URL }}");
    expect(
      within(dialog).getByText("{{ .Supersede.NewTicketURL }}"),
    ).toBeInTheDocument();
  });

  it("shows the integration default as placeholder for empty templates", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects",
        method: "GET",
      })
      .reply(200, [
        {
          ...sampleProjects[0],
          contentTemplates: {
            title: "",
            description: "",
            updateComment: "",
            supersedeComment: "",
          },
        },
      ]);
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
    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    );

    expect(within(dialog).getByLabelText("Title template")).toHaveAttribute(
      "placeholder",
      integrationDefaultTemplates("jira").title,
    );
    expect(
      within(dialog).getByLabelText("Description template"),
    ).toHaveAttribute(
      "placeholder",
      integrationDefaultTemplates("jira").description,
    );
    expect(
      within(dialog)
        .getByLabelText("Description template")
        .getAttribute("placeholder"),
    ).toMatch(/^Repository: /);
  });

  it("saves edited content templates", async () => {
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
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, {
        items: [
          { id: "To Do", name: "To Do" },
          { id: "In Progress", name: "In Progress" },
          { id: "Done", name: "Done" },
          { id: "Cancelled", name: "Cancelled" },
        ],
      });

    let patchBody: Record<string, unknown> | undefined;
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects/tp-1",
        method: "PATCH",
      })
      .reply(200, (opts) => {
        patchBody = JSON.parse(opts.body as string) as Record<string, unknown>;
        return {
          ...sampleProjects[0],
          contentTemplates: {
            ...sampleProjects[0].contentTemplates,
            title: "Updated: {{ .Repository }}",
          },
        };
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.click(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    );
    fireEvent.change(within(dialog).getByLabelText("Title template"), {
      target: { value: "Updated: {{ .Repository }}" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await vi.waitFor(() => {
      expect(patchBody).toBeDefined();
    });

    expect(patchBody).not.toHaveProperty("onOpenTicketPolicy");
    expect(patchBody?.contentTemplates).toEqual({
      title: "Updated: {{ .Repository }}",
      description: "**Notes:** {{ .ReleaseNotes }}",
      updateComment: "Updated: {{ .PullRequest.URL }}",
      supersedeComment: "Superseded by {{ .Supersede.NewPullRequest.URL }}",
    });
    expect(patchBody?.filter).toEqual({
      updateTypes: ["major"],
      labels: ["deps"],
      skipAutomerged: false,
    });
  });

  it("shows the default filter for a project that sets none", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, [
        {
          ...sampleProjects[0],
          filter: { updateTypes: [], labels: [], skipAutomerged: true },
        },
      ]);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("tab", { name: "Filter" }));

    expect(
      within(dialog).getByText(messages["ticket-projects"].filter.noteDefault),
    ).toBeInTheDocument();
    for (const type of ["Major", "Minor", "Patch", "Digest", "Lockfile"]) {
      expect(
        within(dialog).getByRole("checkbox", { name: type }),
      ).not.toBeChecked();
    }
    expect(within(dialog).getByLabelText("Labels")).toHaveValue("");
    expect(
      within(dialog).getByRole("switch", {
        name: "Skip automerged pull requests",
      }),
    ).toBeChecked();
  });

  it("saves the edited filter through the API", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, [
        {
          ...sampleProjects[0],
          filter: { updateTypes: [], labels: [], skipAutomerged: true },
        },
      ]);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, { items: [] })
      .persist();

    let patchBody: Record<string, unknown> | undefined;
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects/tp-1",
        method: "PATCH",
      })
      .reply(200, (opts) => {
        patchBody = JSON.parse(opts.body as string) as Record<string, unknown>;
        return sampleProjects[0];
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("tab", { name: "Filter" }));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Minor" }));
    fireEvent.click(within(dialog).getByRole("checkbox", { name: "Patch" }));
    fireEvent.change(within(dialog).getByLabelText("Labels"), {
      target: { value: "deps, security, " },
    });
    expect(within(dialog).getByLabelText("Labels")).toHaveValue(
      "deps, security, ",
    );
    fireEvent.click(
      within(dialog).getByRole("switch", {
        name: "Skip automerged pull requests",
      }),
    );
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await vi.waitFor(() => {
      expect(patchBody).toBeDefined();
    });
    expect(patchBody?.filter).toEqual({
      updateTypes: ["minor", "patch"],
      labels: ["deps", "security"],
      skipAutomerged: false,
    });
  });

  it("saves a stored label containing a comma unchanged when only the name is edited", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, [
        {
          ...sampleProjects[0],
          filter: {
            updateTypes: [],
            labels: ["needs, review"],
            skipAutomerged: true,
          },
        },
      ]);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, { items: [] })
      .persist();

    let patchBody: Record<string, unknown> | undefined;
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects/tp-1",
        method: "PATCH",
      })
      .reply(200, (opts) => {
        patchBody = JSON.parse(opts.body as string) as Record<string, unknown>;
        return sampleProjects[0];
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/Display name/), {
      target: { value: "DEV renamed" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await vi.waitFor(() => {
      expect(patchBody).toBeDefined();
    });
    expect(patchBody?.name).toBe("DEV renamed");
    expect(patchBody?.filter).toEqual({
      updateTypes: [],
      labels: ["needs, review"],
      skipAutomerged: true,
    });
  });

  it("shows the server's template validation error and opens the templates tab", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/ticket-projects", method: "GET" })
      .reply(200, sampleProjects);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, ticketIntegrations);
    pool
      .intercept({
        path: /\/api\/go\/api\/v1\/integrations\/int-jira\/ticket-metadata\/.*/,
        method: "GET",
      })
      .reply(200, { items: [] })
      .persist();
    pool
      .intercept({
        path: "/api/go/api/v1/ticket-projects/tp-1",
        method: "PATCH",
      })
      .reply(422, {
        error: {
          code: "VALIDATION_ERROR",
          message:
            "content_templates.title: template: ticket:1: unexpected EOF",
        },
      });

    renderTicketProjectsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Jira — DEV" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/Display name/), {
      target: { value: "Jira — DEV 2" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(
      await within(dialog).findByText(
        "content_templates.title: template: ticket:1: unexpected EOF",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("tab", { name: "Content templates" }),
    ).toHaveAttribute("data-active");
    expect(
      within(dialog).getByLabelText("Update comment template"),
    ).toBeInTheDocument();
  });
});
