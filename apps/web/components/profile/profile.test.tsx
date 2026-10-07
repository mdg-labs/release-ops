import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { ProfileView } from "@/components/profile/profile-view";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";

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

function mockSession(
  pool: MockAgent["get"] extends (origin: string) => infer R ? R : never,
) {
  pool
    .intercept({
      path: "/api/go/api/v1/auth/session",
      method: "GET",
    })
    .reply(200, sessionUser);
}

describe("ProfileView", () => {
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

  it("shows current email from session", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);

    renderWithProviders(<ProfileView />);

    expect(
      await screen.findByDisplayValue("admin@example.com"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: messages.profile.title }),
    ).toBeInTheDocument();
  });

  it("submits email change request", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/email-change-request",
        method: "POST",
        body: JSON.stringify({
          newEmail: "new@example.com",
          currentPassword: "secret",
        }),
      })
      .reply(200, {});

    renderWithProviders(<ProfileView />);

    fireEvent.change(
      await screen.findByLabelText(messages.profile.changeEmail.newEmail),
      { target: { value: "new@example.com" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changeEmail.currentPassword),
      { target: { value: "secret" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: messages.profile.changeEmail.submit,
      }),
    );

    await waitFor(() =>
      expect(
        screen.getByText(messages.profile.changeEmail.pendingTitle),
      ).toBeInTheDocument(),
    );
  });

  it("shows email change validation errors", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);

    renderWithProviders(<ProfileView />);

    fireEvent.click(
      await screen.findByRole("button", {
        name: messages.profile.changeEmail.submit,
      }),
    );

    expect(
      await screen.findByText(
        messages.profile.changeEmail.validation.newEmailRequired,
      ),
    ).toBeInTheDocument();
  });

  it("shows email change API error for wrong password", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/email-change-request",
        method: "POST",
      })
      .reply(401, { code: "UNAUTHORIZED", message: "wrong password" });

    renderWithProviders(<ProfileView />);

    fireEvent.change(
      await screen.findByLabelText(messages.profile.changeEmail.newEmail),
      { target: { value: "new@example.com" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changeEmail.currentPassword),
      { target: { value: "wrong" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: messages.profile.changeEmail.submit,
      }),
    );

    await waitFor(() =>
      expect(
        screen.getByText(messages.profile.changeEmail.invalidPassword),
      ).toBeInTheDocument(),
    );
  });

  it("validates password change form", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);

    renderWithProviders(<ProfileView />);

    fireEvent.click(
      await screen.findByRole("button", {
        name: messages.profile.changePassword.submit,
      }),
    );

    expect(
      await screen.findByText(
        messages.profile.changePassword.validation.currentPasswordRequired,
      ),
    ).toBeInTheDocument();
  });

  it("shows password mismatch validation", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);

    renderWithProviders(<ProfileView />);

    fireEvent.change(
      await screen.findByLabelText(
        messages.profile.changePassword.currentPassword,
      ),
      { target: { value: "secret" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.newPassword),
      { target: { value: "newpassword" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.confirmPassword),
      { target: { value: "different" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: messages.profile.changePassword.submit,
      }),
    );

    expect(
      await screen.findByText(
        messages.profile.changePassword.validation.passwordMismatch,
      ),
    ).toBeInTheDocument();
  });

  it("submits password change request", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/password-change",
        method: "POST",
        body: JSON.stringify({
          currentPassword: "secret",
          newPassword: "newpassword",
        }),
      })
      .reply(200, {});

    renderWithProviders(<ProfileView />);

    fireEvent.change(
      await screen.findByLabelText(
        messages.profile.changePassword.currentPassword,
      ),
      { target: { value: "secret" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.newPassword),
      { target: { value: "newpassword" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.confirmPassword),
      { target: { value: "newpassword" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: messages.profile.changePassword.submit,
      }),
    );

    await waitFor(() =>
      expect(
        screen.getByText(messages.profile.changePassword.successTitle),
      ).toBeInTheDocument(),
    );
  });

  it("shows password change API error for wrong current password", async () => {
    const pool = mockAgent.get(ORIGIN);
    mockSession(pool);
    pool
      .intercept({
        path: "/api/go/api/v1/users/me/password-change",
        method: "POST",
      })
      .reply(401, { code: "UNAUTHORIZED", message: "wrong password" });

    renderWithProviders(<ProfileView />);

    fireEvent.change(
      await screen.findByLabelText(
        messages.profile.changePassword.currentPassword,
      ),
      { target: { value: "wrong" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.newPassword),
      { target: { value: "newpassword" } },
    );
    fireEvent.change(
      screen.getByLabelText(messages.profile.changePassword.confirmPassword),
      { target: { value: "newpassword" } },
    );
    fireEvent.click(
      screen.getByRole("button", {
        name: messages.profile.changePassword.submit,
      }),
    );

    await waitFor(() =>
      expect(
        screen.getByText(messages.profile.changePassword.invalidPassword),
      ).toBeInTheDocument(),
    );
  });
});
