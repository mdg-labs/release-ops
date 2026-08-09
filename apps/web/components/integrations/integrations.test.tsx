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
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
  {
    id: "int-2",
    kind: "jira",
    name: "Company Jira",
    baseUrl: "https://company.atlassian.net",
    hasSecret: true,
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
});
