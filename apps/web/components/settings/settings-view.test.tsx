import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
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

const defaultSettings = {
  pollIntervalMinutes: 60,
};

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

function mockSettingsRoutes(
  pool: MockAgent["get"] extends (origin: string) => infer R ? R : never,
) {
  pool
    .intercept({
      path: "/api/go/api/v1/settings",
      method: "GET",
    })
    .reply(200, defaultSettings);
}

describe("SettingsView", () => {
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

  afterEach(() => {
    cleanup();
    globalThis.fetch = originalFetch;
    setGlobalDispatcher(mockAgent);
    mockAgent.close();
  });

  it("saves the poll interval via PATCH and offers no token expiry fields", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSettingsRoutes(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "PATCH",
        body: JSON.stringify({
          pollIntervalMinutes: 60,
        }),
      })
      .reply(200, defaultSettings);
    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "GET",
      })
      .reply(200, defaultSettings);
    pool
      .intercept({
        path: "/api/go/api/v1/status",
        method: "GET",
      })
      .reply(200, {
        pollIntervalMinutes: 60,
        lastRun: null,
        repos: [],
        isPolling: false,
      });

    renderWithProviders(<SettingsView />);

    expect(
      await screen.findByText(messages.settings.pollIntervalSection),
    ).toBeInTheDocument();
    expect(screen.queryByText(/expiry/i)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: messages.common.save }));

    await waitFor(() =>
      expect(screen.getByText(messages.settings.saved)).toBeInTheDocument(),
    );
  });
});
