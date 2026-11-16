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
import { SettingsView } from "@/components/settings/settings-view";
import messages from "@/messages/en.json";

vi.mock("next/navigation", () => ({
  usePathname: () => "/settings",
}));

const ORIGIN = "http://localhost:3000";
const V1 = "/api/go/api/v1";
const JSON_HEADERS = { headers: { "content-type": "application/json" } };

const settings = {
  concurrencyCap: 2,
  mode: "webhook",
  presetRepositories: ["acme/presets"],
  globalConfig: {
    source: "repository",
    repository: "acme/renovate",
    ref: "main",
  },
};

// The config text is shown as it is: a CRLF, a tab, a quote and a placeholder where the API
// hid a token. It is a fixture, so it holds no real secret.
const configText =
  "module.exports = {\r\n\tallowedCommands: ['^/opt/tools/run\\\\.sh$'],\r\n\ttoken: '[REDACTED]',\r\n};";

const globalConfig = {
  source: "repository",
  repository: "acme/renovate",
  ref: "main",
  capturedAt: "2026-11-14T12:00:00.000Z",
  content: configText,
};

const installation = {
  id: "inst-1",
  accountLogin: "acme",
  accountType: "Organization",
  status: "active",
  repositorySelection: "selected",
  repositoryCount: 14,
  permissions: { contents: "read", metadata: "read" },
  missingPermissions: ["contents", "members"],
  rateLimit: {
    remaining: 4812,
    limit: 5000,
    resetAt: "2026-11-14T13:00:00.000Z",
  },
  defaultTicketProjectId: null,
};

const githubApp = {
  app: {
    appId: 1234567,
    slug: "release-ops-acme",
    clientId: "Iv1.example",
    htmlUrl: "https://github.com/apps/release-ops-acme",
    ownerLogin: "acme",
    ownerType: "Organization",
    webhookActive: true,
    hasPrivateKey: true,
    hasClientSecret: true,
    hasWebhookSecret: true,
  },
  installation,
  roleMapping: {
    adminTeam: "acme/release-ops-admins",
    viewerTeam: null,
    problems: [],
  },
};

const ticketProject = (id: string, name: string) => ({
  id,
  integrationId: "int-1",
  externalProjectId: name,
  name,
  createConfig: {},
  statusMapping: {},
  contentTemplates: { title: "", description: "", supersedeComment: "" },
  createdAt: "2026-11-14T12:00:00.000Z",
  updatedAt: "2026-11-14T12:00:00.000Z",
});

type Pool = ReturnType<MockAgent["get"]>;

async function pick(trigger: HTMLElement, name: string) {
  fireEvent.click(trigger);
  const option = await screen.findByRole("option", { name });
  fireEvent.pointerDown(option, {
    pointerId: 1,
    pointerType: "mouse",
    buttons: 1,
  });
  fireEvent.pointerUp(option, { pointerId: 1, pointerType: "mouse" });
  fireEvent.click(option);
}

function reply(
  pool: Pool,
  method: "GET" | "PATCH",
  path: string,
  status: number,
  body: object,
) {
  pool
    .intercept({ path: `${V1}${path}`, method })
    .reply(status, body, JSON_HEADERS)
    .persist();
}

function replyAll(
  pool: Pool,
  overrides: Record<string, { status: number; body: object }> = {},
) {
  const defaults: Record<string, { status: number; body: object }> = {
    "/settings": { status: 200, body: settings },
    "/settings/global-config": { status: 200, body: globalConfig },
    "/github-app": { status: 200, body: githubApp },
    "/installations": { status: 200, body: { items: [installation] } },
    "/ticket-projects": {
      status: 200,
      body: [ticketProject("tp-1", "DEV"), ticketProject("tp-2", "OPS")],
    },
  };
  for (const [path, { status, body }] of Object.entries({
    ...defaults,
    ...overrides,
  })) {
    reply(pool, "GET", path, status, body);
  }
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
        <SettingsView />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("SettingsView", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
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

  it("shows the App, the installation with its missing permissions, and the role mapping", async () => {
    replyAll(mockAgent.get(ORIGIN));
    renderView();

    expect(await screen.findByTestId("app-id")).toHaveTextContent("1234567");
    expect(screen.getByTestId("app-slug")).toHaveTextContent(
      "release-ops-acme",
    );
    expect(screen.getByTestId("app-owner")).toHaveTextContent(
      "acme (organization)",
    );
    expect(screen.getByTestId("app-webhook")).toHaveTextContent("Active");
    expect(screen.getByTestId("installation-account")).toHaveTextContent(
      "acme (Organization)",
    );
    expect(screen.getByTestId("installation-repositories")).toHaveTextContent(
      "14 repositories, selected repositories",
    );
    expect(screen.getByTestId("installation-rate-limit")).toHaveTextContent(
      "4812 of 5000 left",
    );
    expect(screen.getByTestId("missing-permissions")).toHaveTextContent(
      "contents, members",
    );
    expect(screen.getByTestId("admin-team")).toHaveTextContent(
      "acme/release-ops-admins",
    );
    expect(screen.getByTestId("viewer-team")).toHaveTextContent("Not set");
    expect(screen.getByTestId("role-rules")).toHaveTextContent(
      "every other active member of the organization",
    );
    expect(screen.getByText(messages.githubAppKey.title)).toBeInTheDocument();
  });

  it("flags a role mapping problem and the polling mode", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/settings": { status: 200, body: { ...settings, mode: "polling" } },
      "/github-app": {
        status: 200,
        body: {
          ...githubApp,
          roleMapping: {
            adminTeam: "other/admins",
            viewerTeam: null,
            problems: [
              { variable: "ADMIN_TEAM", code: "team_of_another_organization" },
            ],
          },
        },
      },
    });
    renderView();

    expect(await screen.findByTestId("polling-notice")).toBeInTheDocument();
    expect(
      await screen.findByText(
        /ADMIN_TEAM names a team of another organization/,
      ),
    ).toBeInTheDocument();
  });

  it("shows the global config as the raw text with its source and no edit control", async () => {
    replyAll(mockAgent.get(ORIGIN));
    renderView();

    const content = await screen.findByTestId("global-config-content");
    expect(content.textContent).toBe(configText);
    expect(screen.getByTestId("global-config-source")).toHaveTextContent(
      "Repository acme/renovate at main",
    );
    expect(within(content).queryByRole("textbox")).toBeNull();
    expect(content.tagName).toBe("PRE");
    expect(screen.queryByText(/allowed commands/i)).not.toBeInTheDocument();
  });

  it("says no job has returned the config yet when the content is null", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/settings/global-config": {
        status: 200,
        body: { ...globalConfig, content: null, capturedAt: null },
      },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.globalConfig.notCaptured),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("global-config-content")).toBeNull();
  });

  it("keeps the other sections when the global config fails to load, and says so", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/settings/global-config": { status: 500, body: { error: {} } },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.globalConfig.loadFailed),
    ).toBeInTheDocument();
    expect(await screen.findByTestId("app-id")).toBeInTheDocument();
    expect(screen.queryByTestId("global-config-content")).toBeNull();
    expect(
      screen.queryByText(messages.settings.globalConfig.notCaptured),
    ).toBeNull();
  });

  it("shows the empty state before an installation exists", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/github-app": {
        status: 200,
        body: { ...githubApp, installation: null },
      },
      "/installations": { status: 200, body: { items: [] } },
    });
    renderView();

    expect(await screen.findByTestId("no-installation")).toBeInTheDocument();
    expect(
      screen.getByText(messages.settings.defaultProject.noInstallation),
    ).toBeInTheDocument();
    expect(
      screen.getByText(messages.settings.roles.noInstallation),
    ).toBeInTheDocument();
  });

  it("says the App is not set up on a 404 and an error on any other failure", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/github-app": {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "x" } },
      },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.app.notConfigured),
    ).toBeInTheDocument();
  });

  it("shows an error, not an empty App, when the App request fails", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/github-app": { status: 500, body: { error: {} } },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.app.loadFailed),
    ).toBeInTheDocument();
    expect(screen.queryByText(messages.settings.app.notConfigured)).toBeNull();
  });

  it("saves the concurrency cap through PATCH and offers no poll interval", async () => {
    const pool = mockAgent.get(ORIGIN);
    replyAll(pool);
    pool
      .intercept({
        path: `${V1}/settings`,
        method: "PATCH",
        body: JSON.stringify({ concurrencyCap: 5 }),
      })
      .reply(200, { ...settings, concurrencyCap: 5 }, JSON_HEADERS);
    renderView();

    const field = await screen.findByLabelText(
      messages.settings.instance.concurrencyCap,
    );
    expect(field).toHaveValue("2");
    expect(screen.queryByText(/poll interval/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/expiry/i)).not.toBeInTheDocument();

    fireEvent.change(field, { target: { value: "5" } });
    fireEvent.click(
      screen.getAllByRole("button", { name: messages.common.save })[0],
    );

    expect(
      await screen.findByText(messages.settings.instance.saved),
    ).toBeInTheDocument();
  });

  it("refuses an empty cap without sending it", async () => {
    replyAll(mockAgent.get(ORIGIN));
    renderView();

    const field = await screen.findByLabelText(
      messages.settings.instance.concurrencyCap,
    );
    fireEvent.change(field, { target: { value: "" } });
    fireEvent.click(
      screen.getAllByRole("button", { name: messages.common.save })[0],
    );

    expect(
      await screen.findByText(messages.settings.instance.minCap),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(messages.settings.instance.saved),
    ).not.toBeInTheDocument();
  });

  it("shows the server's refusal when saving the cap fails", async () => {
    const pool = mockAgent.get(ORIGIN);
    replyAll(pool);
    pool
      .intercept({ path: `${V1}/settings`, method: "PATCH" })
      .reply(
        422,
        { error: { code: "VALIDATION_ERROR", message: "cap refused" } },
        JSON_HEADERS,
      );
    renderView();

    const field = await screen.findByLabelText(
      messages.settings.instance.concurrencyCap,
    );
    fireEvent.change(field, { target: { value: "3" } });
    fireEvent.click(
      screen.getAllByRole("button", { name: messages.common.save })[0],
    );

    expect(await screen.findByText("cap refused")).toBeInTheDocument();
    expect(
      screen.queryByText(messages.settings.instance.saved),
    ).not.toBeInTheDocument();
  });

  it("sets the installation's default ticket project through PATCH", async () => {
    const pool = mockAgent.get(ORIGIN);
    replyAll(pool);
    pool
      .intercept({
        path: `${V1}/installations/inst-1`,
        method: "PATCH",
        body: JSON.stringify({ defaultTicketProjectId: "tp-2" }),
      })
      .reply(
        200,
        { ...installation, defaultTicketProjectId: "tp-2" },
        JSON_HEADERS,
      );
    renderView();

    const trigger = await screen.findByTestId("default-ticket-project-select");
    await pick(trigger, "OPS");
    const section = trigger.closest("[data-slot='settings-section']");
    fireEvent.click(
      within(section as HTMLElement).getByRole("button", {
        name: messages.common.save,
      }),
    );

    expect(
      await screen.findByText(messages.settings.defaultProject.saved),
    ).toBeInTheDocument();
  });

  it("clears the default ticket project with null", async () => {
    const pool = mockAgent.get(ORIGIN);
    replyAll(pool, {
      "/installations": {
        status: 200,
        body: { items: [{ ...installation, defaultTicketProjectId: "tp-1" }] },
      },
    });
    pool
      .intercept({
        path: `${V1}/installations/inst-1`,
        method: "PATCH",
        body: JSON.stringify({ defaultTicketProjectId: null }),
      })
      .reply(200, installation, JSON_HEADERS);
    renderView();

    const trigger = await screen.findByTestId("default-ticket-project-select");
    await waitFor(() => expect(trigger).toHaveTextContent("DEV"));
    await pick(trigger, messages.settings.defaultProject.none);
    const section = trigger.closest("[data-slot='settings-section']");
    fireEvent.click(
      within(section as HTMLElement).getByRole("button", {
        name: messages.common.save,
      }),
    );

    expect(
      await screen.findByText(messages.settings.defaultProject.saved),
    ).toBeInTheDocument();
  });

  it("shows the empty state when there are no ticket projects", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/ticket-projects": { status: 200, body: [] },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.defaultProject.noProjects),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("default-ticket-project-select")).toBeNull();
  });

  it("shows an error, not an empty select, when the ticket projects fail to load", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/ticket-projects": { status: 500, body: { error: {} } },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.defaultProject.loadFailed),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(messages.settings.defaultProject.noProjects),
    ).toBeNull();
  });

  it("shows the load error and no section when the settings fail to load", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/settings": { status: 500, body: { error: {} } },
    });
    renderView();

    expect(
      await screen.findByText(messages.settings.loadFailed),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("app-id")).toBeNull();
  });

  it("shows the forbidden state, and no section, for a 403", async () => {
    replyAll(mockAgent.get(ORIGIN), {
      "/settings": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "no" } },
      },
    });
    renderView();

    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByTestId("app-id")).toBeNull();
  });
});
