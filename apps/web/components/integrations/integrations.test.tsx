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
import { IntegrationsView } from "@/components/integrations/integrations-view";
import { ToastProvider } from "@/components/ui/toast";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const sampleIntegrations = [
  {
    id: "int-1",
    kind: "github",
    name: "GitHub Org",
    baseUrl: null,
    hasSecret: true,
    isDefault: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
  {
    id: "int-2",
    kind: "jira",
    name: "Company Jira",
    baseUrl: "https://company.atlassian.net",
    hasSecret: true,
    isDefault: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

function renderIntegrationsPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <IntegrationsView />
        </ToastProvider>
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("IntegrationsView", () => {
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
    vi.unstubAllGlobals();
  });

  it("renders empty state when no integrations exist", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, []);

    renderIntegrationsPage();

    expect(await screen.findByText("No integrations yet")).toBeInTheDocument();
    expect(
      screen.getByText("Connect a source or ticket provider to get started."),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "Add integration" }),
    ).toHaveLength(2);
  });

  it("lists integrations from the API", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    expect(await screen.findByText("GitHub Org")).toBeInTheDocument();
    expect(screen.getByText("Company Jira")).toBeInTheDocument();
    expect(screen.getAllByText("Configured")).toHaveLength(2);
  });

  it("shows base URL when editing gitlab integration", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, [
        {
          id: "int-3",
          kind: "gitlab",
          name: "GitLab CE",
          baseUrl: "https://gitlab.example.com",
          hasSecret: true,
          createdAt: "2026-01-01T00:00:00.000Z",
          updatedAt: "2026-01-01T00:00:00.000Z",
        },
      ]);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit GitLab CE" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText(/Base URL/)).toHaveValue(
      "https://gitlab.example.com",
    );
  });

  it("shows Jira email field when editing", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Company Jira" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText(/^Email/)).toBeInTheDocument();
    expect(within(dialog).getByLabelText(/API token/)).toBeInTheDocument();
  });

  it("refuses a new Jira API token with a blank email", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    let patchSent = false;
    pool
      .intercept({ path: "/api/go/api/v1/integrations/int-2", method: "PATCH" })
      .reply(() => {
        patchSent = true;
        return { statusCode: 200, data: "{}" };
      });

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Company Jira" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog)
        .getByText(/^Email/)
        .querySelector("span"),
    ).toBeNull();

    fireEvent.change(within(dialog).getByLabelText(/API token/), {
      target: { value: "new-token" },
    });
    expect(
      within(dialog)
        .getByText(/^Email/)
        .querySelector("span"),
    ).not.toBeNull();

    fireEvent.submit(
      within(dialog).getByRole("button", { name: "Save" }).closest("form")!,
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Enter the Jira email when you replace the API token.",
    );
    expect(patchSent).toBe(false);
  });

  it("sends a new Jira secret with both email and token", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    let patchBody: string | undefined;
    pool
      .intercept({ path: "/api/go/api/v1/integrations/int-2", method: "PATCH" })
      .reply((opts) => {
        patchBody = opts.body?.toString();
        return {
          statusCode: 200,
          data: JSON.stringify(sampleIntegrations[1]),
          responseOptions: { headers: { "content-type": "application/json" } },
        };
      });
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Company Jira" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/^Email/), {
      target: { value: "me@example.com" },
    });
    fireEvent.change(within(dialog).getByLabelText(/API token/), {
      target: { value: "new-token" },
    });
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(patchBody).toBeDefined();
    });
    expect(JSON.parse(patchBody ?? "{}").secret).toBe(
      JSON.stringify({ email: "me@example.com", api_token: "new-token" }),
    );
  });

  it("does not re-display stored secret when editing", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit GitHub Org" }),
    );

    const dialog = await screen.findByRole("dialog");
    const secretInput = within(dialog).getByLabelText(
      /Token/,
    ) as HTMLInputElement;
    expect(secretInput.value).toBe("");
    expect(secretInput.type).toBe("password");
    expect(within(dialog).getByText("Configured")).toBeInTheDocument();
  });

  it("shows delete confirmation copy", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete GitHub Org" }),
    );

    expect(
      await screen.findByText("Delete “GitHub Org”? This cannot be undone."),
    ).toBeInTheDocument();
  });

  it("keeps the delete dialog open and explains an in-use integration", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1",
        method: "DELETE",
      })
      .reply(409, {
        error: {
          code: "CONFLICT",
          message: "integration is referenced by repos or ticket projects",
        },
      });

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete GitHub Org" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText(messages.integrations.deleteInUse),
    ).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    await waitFor(() => {
      expect(
        within(dialog).getByRole("button", { name: "Delete" }),
      ).toBeEnabled();
    });
  });

  it("shows the server message when deleting an integration fails", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1",
        method: "DELETE",
      })
      .reply(500, {
        error: { code: "INTERNAL", message: "database is locked" },
      });
    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1",
        method: "DELETE",
      })
      .replyWithError(new Error("socket hang up"));

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete GitHub Org" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText("database is locked"),
    ).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));

    expect(
      await within(dialog).findByText(messages.integrations.deleteFailed),
    ).toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("drops a delete failure that settles after its dialog was closed", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1",
        method: "DELETE",
      })
      .reply(500, {
        error: { code: "INTERNAL", message: "database is locked" },
      })
      .delay(200);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete GitHub Org" }),
    );
    const first = await screen.findByRole("alertdialog");
    fireEvent.click(within(first).getByRole("button", { name: "Delete" }));
    fireEvent.click(within(first).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Delete Company Jira" }),
    );
    const second = await screen.findByRole("alertdialog");
    await waitFor(() => {
      expect(
        within(second).getByRole("button", { name: "Delete" }),
      ).toBeEnabled();
    });

    expect(within(second).queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
  });

  it("runs test connection with async toast feedback", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/integrations",
        method: "GET",
      })
      .reply(200, sampleIntegrations);

    pool
      .intercept({
        path: "/api/go/api/v1/integrations/int-1/test",
        method: "POST",
      })
      .reply(200, { success: true, message: "ok" });

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Test connection for GitHub Org",
      }),
    );

    expect(await screen.findByText("Testing connection")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText("Connection successful")).toBeInTheDocument(),
    );
  });

  it("marks the default integration with a badge", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    const githubRow = (await screen.findByText("GitHub Org")).closest("tr");
    const jiraRow = screen.getByText("Company Jira").closest("tr");
    expect(githubRow).not.toBeNull();
    expect(jiraRow).not.toBeNull();
    expect(within(githubRow as HTMLElement).getByText("Default")).toBeVisible();
    expect(within(jiraRow as HTMLElement).queryByText("Default")).toBeNull();
  });

  it("offers the default switch for source kinds only", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit GitHub Org" }),
    );
    let dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByRole("switch", {
        name: "Default for this source type",
      }),
    ).toHaveAttribute("aria-checked", "true");

    cleanup();
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);
    renderIntegrationsPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Company Jira" }),
    );
    dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).queryByRole("switch", {
        name: "Default for this source type",
      }),
    ).toBeNull();
  });

  it("sends isDefault when saving a source integration", async () => {
    vi.stubGlobal("PointerEvent", MouseEvent);
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, [{ ...sampleIntegrations[0], isDefault: false }]);

    let patchBody: string | undefined;
    pool
      .intercept({ path: "/api/go/api/v1/integrations/int-1", method: "PATCH" })
      .reply((opts) => {
        patchBody = opts.body?.toString();
        return {
          statusCode: 200,
          data: JSON.stringify({ ...sampleIntegrations[0], isDefault: true }),
          responseOptions: { headers: { "content-type": "application/json" } },
        };
      });
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit GitHub Org" }),
    );
    const dialog = await screen.findByRole("dialog");
    const toggle = within(dialog).getByRole("switch", {
      name: "Default for this source type",
    });
    expect(toggle).toHaveAttribute("aria-checked", "false");
    fireEvent.click(toggle);
    fireEvent.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => {
      expect(patchBody).toBeDefined();
    });
    expect(JSON.parse(patchBody ?? "{}")).toMatchObject({
      name: "GitHub Org",
      isDefault: true,
    });
  });
  describe("token on Add", () => {
    async function openAddDrawer() {
      const pool = mockAgent.get(ORIGIN);
      pool
        .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
        .reply(200, []);
      renderIntegrationsPage();
      await screen.findByText("No integrations yet");
      fireEvent.click(
        screen.getAllByRole("button", { name: "Add integration" })[0],
      );
      return { pool, dialog: await screen.findByRole("dialog") };
    }

    async function pickKind(dialog: HTMLElement, kindLabel: string) {
      fireEvent.click(within(dialog).getByRole("combobox"));
      await waitFor(() => {
        expect(screen.getByRole("listbox")).toBeInTheDocument();
      });
      const option = screen.getByRole("option", { name: kindLabel });
      fireEvent.pointerDown(option, {
        pointerId: 1,
        pointerType: "mouse",
        buttons: 1,
      });
      fireEvent.pointerUp(option, { pointerId: 1, pointerType: "mouse" });
      fireEvent.click(option);
    }

    it("creates a GitHub integration without a token", async () => {
      const { pool, dialog } = await openAddDrawer();
      let postBody: string | undefined;
      pool
        .intercept({ path: "/api/go/api/v1/integrations", method: "POST" })
        .reply((opts) => {
          postBody = opts.body?.toString();
          return {
            statusCode: 201,
            data: JSON.stringify({
              ...sampleIntegrations[0],
              hasSecret: false,
            }),
            responseOptions: {
              headers: { "content-type": "application/json" },
            },
          };
        });
      pool
        .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
        .reply(200, [{ ...sampleIntegrations[0], hasSecret: false }]);

      const token = within(dialog).getByLabelText(/Token/);
      expect(token).not.toBeRequired();
      expect(
        within(dialog)
          .getByText(/^Token/)
          .querySelector("span"),
      ).toBeNull();

      fireEvent.change(within(dialog).getByLabelText(/^Name/), {
        target: { value: "Public GitHub" },
      });
      fireEvent.submit(
        within(dialog).getByRole("button", { name: "Save" }).closest("form")!,
      );

      await waitFor(() => {
        expect(postBody).toBeDefined();
      });
      expect(JSON.parse(postBody ?? "{}")).toMatchObject({
        kind: "github",
        name: "Public GitHub",
        secret: "",
      });
      const row = (await screen.findByText("GitHub Org")).closest("tr")!;
      expect(within(row).getByText("Missing")).toBeInTheDocument();
      expect(within(row).queryByText("Configured")).toBeNull();
    });

    it("refuses Linear without an API key", async () => {
      const { pool, dialog } = await openAddDrawer();
      let postSent = false;
      pool
        .intercept({ path: "/api/go/api/v1/integrations", method: "POST" })
        .reply(() => {
          postSent = true;
          return { statusCode: 201, data: "{}" };
        });

      await pickKind(dialog, "Linear");
      const secret = await within(dialog).findByLabelText(/API key/);
      expect(secret).toBeRequired();
      fireEvent.change(within(dialog).getByLabelText(/^Name/), {
        target: { value: "Team Linear" },
      });
      fireEvent.submit(
        within(dialog).getByRole("button", { name: "Save" }).closest("form")!,
      );

      expect(await within(dialog).findByRole("alert")).toHaveTextContent(
        "Token or API key is required.",
      );
      expect(postSent).toBe(false);
    });

    it("refuses GitLab without a token", async () => {
      const { dialog } = await openAddDrawer();

      await pickKind(dialog, "GitLab");
      expect(await within(dialog).findByLabelText(/Token/)).toBeRequired();
    });
  });

  it("lets a tokenless GitHub integration be renamed without a token", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, [{ ...sampleIntegrations[0], hasSecret: false }]);
    let patchBody: string | undefined;
    pool
      .intercept({ path: "/api/go/api/v1/integrations/int-1", method: "PATCH" })
      .reply((opts) => {
        patchBody = opts.body?.toString();
        return {
          statusCode: 200,
          data: JSON.stringify(sampleIntegrations[0]),
          responseOptions: { headers: { "content-type": "application/json" } },
        };
      });
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, sampleIntegrations);

    renderIntegrationsPage();
    const row = (await screen.findByText("GitHub Org")).closest("tr")!;
    expect(within(row).getByText("Missing")).toBeInTheDocument();
    expect(within(row).queryByText("Configured")).toBeNull();
    fireEvent.click(
      await screen.findByRole("button", { name: "Edit GitHub Org" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByText("Configured")).toBeNull();
    expect(within(dialog).getByLabelText(/Token/)).not.toBeRequired();
    fireEvent.change(within(dialog).getByLabelText(/^Name/), {
      target: { value: "Renamed" },
    });
    fireEvent.submit(
      within(dialog).getByRole("button", { name: "Save" }).closest("form")!,
    );

    await waitFor(() => {
      expect(patchBody).toBeDefined();
    });
    expect(JSON.parse(patchBody ?? "{}")).toMatchObject({
      name: "Renamed",
      secret: null,
    });
  });

  it("still requires a token when a tokenless Gitea base URL changes", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/integrations", method: "GET" })
      .reply(200, [
        {
          id: "int-4",
          kind: "gitea",
          name: "Gitea Lab",
          baseUrl: "https://gitea.example.com",
          hasSecret: false,
          isDefault: false,
          createdAt: "2026-01-01T00:00:00.000Z",
          updatedAt: "2026-01-01T00:00:00.000Z",
        },
      ]);
    let patchSent = false;
    pool
      .intercept({ path: "/api/go/api/v1/integrations/int-4", method: "PATCH" })
      .reply(() => {
        patchSent = true;
        return { statusCode: 200, data: "{}" };
      });

    renderIntegrationsPage();
    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Gitea Lab" }),
    );
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText(/Base URL/), {
      target: { value: "https://attacker.example" },
    });
    fireEvent.submit(
      within(dialog).getByRole("button", { name: "Save" }).closest("form")!,
    );

    expect(await within(dialog).findByRole("alert")).toHaveTextContent(
      "Re-enter the token or API key when you change the base URL.",
    );
    expect(patchSent).toBe(false);
  });
});
