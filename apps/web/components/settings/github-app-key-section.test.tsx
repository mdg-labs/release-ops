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
import { GitHubAppKeySection } from "@/components/settings/github-app-key-section";
import { SettingsView } from "@/components/settings/settings-view";
import messages from "@/messages/en.json";

vi.mock("next/navigation", () => ({
  usePathname: () => "/settings",
}));

const ORIGIN = "http://localhost:3000";
const ROTATE_PATH = "/api/go/api/v1/github-app/private-key";
const KEY = "throwaway-key-body";

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

describe("GitHubAppKeySection", () => {
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

  function typeKey(value: string): HTMLElement {
    const field = screen.getByLabelText(messages.githubAppKey.keyLabel);
    fireEvent.change(field, { target: { value } });
    return field;
  }

  const submitName = messages.githubAppKey.submit;

  it("sends the key once, confirms, and empties the field", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: ROTATE_PATH,
        method: "PUT",
        body: JSON.stringify({ privateKey: KEY }),
      })
      .reply(204, "");
    renderWithProviders(<GitHubAppKeySection />);

    expect(screen.getByRole("button", { name: submitName })).toBeDisabled();
    const field = typeKey(KEY);
    fireEvent.click(screen.getByRole("button", { name: submitName }));

    expect(
      await screen.findByText(messages.githubAppKey.rotated),
    ).toBeInTheDocument();
    expect(field).toHaveValue("");
    expect(screen.queryByText(KEY)).not.toBeInTheDocument();
  });

  it("shows a loading state while GitHub verifies the key", async () => {
    let release: (response: Response) => void = () => undefined;
    globalThis.fetch = (() =>
      new Promise<Response>((resolve) => {
        release = resolve;
      })) as typeof fetch;
    renderWithProviders(<GitHubAppKeySection />);

    typeKey(KEY);
    fireEvent.click(screen.getByRole("button", { name: submitName }));

    const pending = await screen.findByRole("button", {
      name: new RegExp(messages.githubAppKey.verifying),
    });
    expect(pending).toBeDisabled();
    expect(
      screen.getByLabelText(messages.githubAppKey.keyLabel),
    ).toBeDisabled();
    expect(
      screen.queryByText(messages.githubAppKey.rotated),
    ).not.toBeInTheDocument();

    release(new Response(null, { status: 204 }));
    expect(
      await screen.findByText(messages.githubAppKey.rotated),
    ).toBeInTheDocument();
  });

  it.each([
    ["VALIDATION_ERROR", 422, messages.githubAppKey.errors.keyRejected],
    ["GITHUB_UNAVAILABLE", 502, messages.githubAppKey.errors.githubUnavailable],
    ["FORBIDDEN", 403, messages.githubAppKey.errors.forbidden],
  ])(
    "shows an inline error for %s and keeps what was typed",
    async (code, status, text) => {
      mockAgent
        .get(ORIGIN)
        .intercept({ path: ROTATE_PATH, method: "PUT" })
        .reply(status, { error: { code, message: "server text" } });
      renderWithProviders(<GitHubAppKeySection />);

      const field = typeKey(KEY);
      fireEvent.click(screen.getByRole("button", { name: submitName }));

      expect(await screen.findByText(text)).toBeInTheDocument();
      expect(
        screen.getByText(messages.githubAppKey.failed),
      ).toBeInTheDocument();
      expect(
        screen.queryByText(messages.githubAppKey.rotated),
      ).not.toBeInTheDocument();
      expect(field).toHaveValue(KEY);
    },
  );

  it("does not claim the key is unchanged when a proxy error hides the outcome", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: ROTATE_PATH, method: "PUT" })
      .reply(502, {
        error: { code: "UPSTREAM_ERROR", message: "Failed to connect" },
      });
    renderWithProviders(<GitHubAppKeySection />);

    typeKey(KEY);
    fireEvent.click(screen.getByRole("button", { name: submitName }));

    const description = await screen.findByText(
      messages.githubAppKey.errors.unknown,
    );
    expect(description).toBeInTheDocument();
    expect(
      screen.getByText(messages.githubAppKey.unconfirmed),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(messages.githubAppKey.failed),
    ).not.toBeInTheDocument();
    const alert = description.closest('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).not.toMatch(/not replaced|unchanged/i);
    expect(messages.githubAppKey.errors.network).not.toMatch(/unchanged/);
  });

  it("shows an error when the request never reaches the server", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: ROTATE_PATH, method: "PUT" })
      .replyWithError(new Error("connection reset"));
    renderWithProviders(<GitHubAppKeySection />);

    typeKey(KEY);
    fireEvent.click(screen.getByRole("button", { name: submitName }));

    const description = await screen.findByText(
      messages.githubAppKey.errors.network,
    );
    expect(description).toBeInTheDocument();
    expect(
      screen.getByText(messages.githubAppKey.unconfirmed),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(messages.githubAppKey.failed),
    ).not.toBeInTheDocument();
    const alert = description.closest('[role="alert"]');
    expect(alert).not.toBeNull();
    expect(alert?.textContent).not.toMatch(/not replaced|unchanged/i);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: submitName })).toBeEnabled(),
    );
  });

  it("is part of the settings page", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/settings", method: "GET" })
      .reply(200, { pollIntervalMinutes: 60 });
    renderWithProviders(<SettingsView />);

    expect(
      await screen.findByText(messages.githubAppKey.title),
    ).toBeInTheDocument();
  });
});
