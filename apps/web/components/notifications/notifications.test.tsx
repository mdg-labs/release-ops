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
import { NotificationsView } from "@/components/notifications/notifications-view";
import { ToastProvider } from "@/components/ui/toast";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

const sampleTargets = [
  {
    id: "nt-1",
    name: "Slack alerts",
    hasSecret: true,
    events: ["create", "error", "supersede"],
    enabled: true,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
  {
    id: "nt-2",
    name: "Ntfy errors",
    hasSecret: true,
    events: ["error"],
    enabled: false,
    createdAt: "2026-01-01T00:00:00.000Z",
    updatedAt: "2026-01-01T00:00:00.000Z",
  },
];

function renderNotificationsPage() {
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
          <NotificationsView />
        </ToastProvider>
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("NotificationsView", () => {
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

  it("renders empty state when no notification targets exist", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, []);

    renderNotificationsPage();

    expect(
      await screen.findByText("No notification targets yet"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Add a Shoutrrr target for release and error alerts."),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "Add notification target" }),
    ).toHaveLength(2);
  });

  it("lists notification targets from the API", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, sampleTargets);

    renderNotificationsPage();

    expect(await screen.findByText("Slack alerts")).toBeInTheDocument();
    expect(screen.getByText("Ntfy errors")).toBeInTheDocument();
    expect(screen.getAllByText("Configured")).toHaveLength(2);
    expect(screen.getByText("Disabled")).toBeInTheDocument();
  });

  it("does not re-display stored Shoutrrr URL when editing", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, sampleTargets);

    renderNotificationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Edit Slack alerts" }),
    );

    const dialog = await screen.findByRole("dialog");
    const urlInput = within(dialog).getByLabelText(
      /Shoutrrr URL/,
    ) as HTMLInputElement;
    expect(urlInput.value).toBe("");
    expect(urlInput.type).toBe("password");
    expect(within(dialog).getByText("Configured")).toBeInTheDocument();
  });

  it("shows events checkbox group in the drawer", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, sampleTargets);

    renderNotificationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Add notification target" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Events")).toBeInTheDocument();
    expect(within(dialog).getAllByRole("checkbox")).toHaveLength(3);
  });

  it("shows delete confirmation copy", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, sampleTargets);

    renderNotificationsPage();

    fireEvent.click(
      await screen.findByRole("button", { name: "Delete Slack alerts" }),
    );

    expect(
      await screen.findByText("Delete “Slack alerts”? This cannot be undone."),
    ).toBeInTheDocument();
  });

  it("runs test notification with async toast feedback", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets",
        method: "GET",
      })
      .reply(200, sampleTargets);

    pool
      .intercept({
        path: "/api/go/api/v1/notification-targets/nt-1/test",
        method: "POST",
      })
      .reply(200, { success: true, message: "ok" });

    renderNotificationsPage();

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Test notification for Slack alerts",
      }),
    );

    expect(
      await screen.findByText("Sending test notification"),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText("Test notification sent")).toBeInTheDocument(),
    );
  });
});
