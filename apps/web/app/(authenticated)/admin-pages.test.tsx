import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import type React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import messages from "@/messages/en.json";
import IntegrationsPage from "./integrations/page";
import SettingsPage from "./settings/page";
import TicketProjectsPage from "./ticket-projects/page";

// The views have their own tests; these only say what each page puts around its view.
vi.mock("@/components/integrations/integrations-view", () => ({
  IntegrationsView: () => <div data-testid="view-integrations" />,
}));
vi.mock("@/components/settings/settings-view", () => ({
  SettingsView: () => <div data-testid="view-settings" />,
}));
vi.mock("@/components/ticket-projects/ticket-projects-view", () => ({
  TicketProjectsView: () => <div data-testid="view-ticket-projects" />,
}));

const ORIGIN = "http://localhost:3000";

const pages: {
  name: string;
  Page: () => React.ReactElement;
  view: string;
}[] = [
  { name: "integrations", Page: IntegrationsPage, view: "view-integrations" },
  { name: "settings", Page: SettingsPage, view: "view-settings" },
  {
    name: "ticket projects",
    Page: TicketProjectsPage,
    view: "view-ticket-projects",
  },
];

describe("admin-only pages", () => {
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

  function session(role: "admin" | "viewer") {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(
        200,
        { user: { id: 1, login: "octocat", role } },
        { headers: { "content-type": "application/json" } },
      );
  }

  function renderPage(Page: () => React.ReactElement) {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <NextIntlClientProvider locale="en" messages={messages}>
        <QueryClientProvider client={queryClient}>
          <Page />
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );
  }

  it.each(pages)(
    "shows a viewer the forbidden state and none of the $name view",
    async ({ Page, view }) => {
      session("viewer");
      renderPage(Page);

      expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
      expect(screen.queryByTestId(view)).not.toBeInTheDocument();
    },
  );

  it.each(pages)(
    "renders the $name view for an admin",
    async ({ Page, view }) => {
      session("admin");
      renderPage(Page);

      expect(await screen.findByTestId(view)).toBeInTheDocument();
      expect(screen.queryByTestId("forbidden-state")).not.toBeInTheDocument();
    },
  );
});
