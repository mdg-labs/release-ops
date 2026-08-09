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
import { UsersView } from "@/components/users/users-view";
import messages from "@/messages/en.json";

vi.mock("next/navigation", () => ({
  usePathname: () => "/settings",
}));

const ORIGIN = "http://localhost:3000";

const defaultSettings = {
  pollIntervalMinutes: 60,
  inviteTokenExpiryHours: 168,
  passwordResetTokenExpiryMinutes: 60,
  smtpConfigured: false,
};

const sessionUser = {
  user: {
    id: "user-1",
    email: "admin@example.com",
  },
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

  it("renders token expiry fields and saves via PATCH", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSettingsRoutes(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/settings",
        method: "PATCH",
        body: JSON.stringify({
          pollIntervalMinutes: 60,
          inviteTokenExpiryHours: 168,
          passwordResetTokenExpiryMinutes: 60,
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
      await screen.findByText(messages.settings.tokenExpirySection),
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getAllByRole("button", { name: messages.common.save })[0],
    );

    await waitFor(() =>
      expect(screen.getByText(messages.settings.saved)).toBeInTheDocument(),
    );
  });
});

describe("UsersView", () => {
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

    Object.assign(navigator, {
      clipboard: {
        writeText: vi.fn().mockResolvedValue(undefined),
      },
    });
  });

  afterEach(() => {
    cleanup();
    globalThis.fetch = originalFetch;
    setGlobalDispatcher(mockAgent);
    mockAgent.close();
    vi.restoreAllMocks();
  });

  it("lists users and disables remove for self", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSettingsRoutes(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/session",
        method: "GET",
      })
      .reply(200, sessionUser);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "user-1",
            email: "admin@example.com",
            createdAt: "2026-01-01T00:00:00.000Z",
          },
          {
            id: "user-2",
            email: "other@example.com",
            createdAt: "2026-01-02T00:00:00.000Z",
          },
        ],
      });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, { items: [] });

    renderWithProviders(<UsersView />);

    expect(await screen.findByText("admin@example.com")).toBeInTheDocument();
    expect(screen.getByText(messages.users.you)).toBeInTheDocument();

    const selfRemoveButton = screen.getByRole("button", {
      name: messages.users.removeAria.replace("{email}", "admin@example.com"),
    });
    expect(selfRemoveButton).toBeDisabled();
  });

  it("creates invitation and shows copy link", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSettingsRoutes(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/session",
        method: "GET",
      })
      .reply(200, sessionUser);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "user-1",
            email: "admin@example.com",
            createdAt: "2026-01-01T00:00:00.000Z",
          },
        ],
      });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, { items: [] });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "POST",
        body: JSON.stringify({ email: "invitee@example.com" }),
      })
      .reply(201, {
        id: "inv-1",
        email: "invitee@example.com",
        expiresAt: "2026-02-01T00:00:00.000Z",
        inviteUrl: "https://example.com/accept-invitation?token=abc",
      });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, { items: [] });

    renderWithProviders(<UsersView />);

    fireEvent.click(
      await screen.findByRole("button", { name: messages.users.invite }),
    );

    const dialog = await screen.findByRole("dialog");
    fireEvent.change(
      within(dialog).getByLabelText(messages.users.inviteEmail),
      {
        target: { value: "invitee@example.com" },
      },
    );
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: messages.users.inviteSubmit,
      }),
    );

    expect(
      await within(dialog).findByDisplayValue(
        "https://example.com/accept-invitation?token=abc",
      ),
    ).toBeInTheDocument();
  });

  it("disables send-email when SMTP is not configured", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSettingsRoutes(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/session",
        method: "GET",
      })
      .reply(200, sessionUser);
    pool
      .intercept({
        path: "/api/go/api/v1/users",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "user-1",
            email: "admin@example.com",
            createdAt: "2026-01-01T00:00:00.000Z",
          },
        ],
      });
    pool
      .intercept({
        path: "/api/go/api/v1/users/invitations",
        method: "GET",
      })
      .reply(200, {
        items: [
          {
            id: "inv-1",
            email: "pending@example.com",
            expiresAt: "2026-02-01T00:00:00.000Z",
            createdAt: "2026-01-01T00:00:00.000Z",
            invitedByUserId: "user-1",
          },
        ],
      });

    renderWithProviders(<UsersView />);

    const sendButton = await screen.findByRole("button", {
      name: messages.users.sendEmailAria.replace(
        "{email}",
        "pending@example.com",
      ),
    });
    expect(sendButton).toBeDisabled();
  });
});
