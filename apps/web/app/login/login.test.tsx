import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { LoginForm } from "./login-form";

const ORIGIN = "http://localhost:3000";

const pushMock = vi.fn();
const refreshMock = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: pushMock, refresh: refreshMock }),
  useSearchParams: () => new URLSearchParams(),
}));

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
}));

function renderLoginForm() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <LoginForm />
    </QueryClientProvider>,
  );
}

describe("LoginForm", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    pushMock.mockReset();
    refreshMock.mockReset();
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
    void mockAgent.close();
  });

  it("renders email and password fields with accessible labels", () => {
    renderLoginForm();

    expect(screen.getByLabelText(/auth\.email/)).toBeInTheDocument();
    expect(screen.getByLabelText(/auth\.password/)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "auth.signIn" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "auth.forgotPasswordLink" }),
    ).toHaveAttribute("href", "/forgot-password");
  });

  it("calls login mutation and redirects to / on success", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login",
        method: "POST",
        body: JSON.stringify({
          email: "admin@example.com",
          password: "secret",
        }),
      })
      .reply(200, {
        user: { id: "u1", email: "admin@example.com" },
      });

    renderLoginForm();

    fireEvent.change(screen.getByLabelText(/auth\.email/), {
      target: { value: "admin@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/auth\.password/), {
      target: { value: "secret" },
    });
    fireEvent.click(screen.getByRole("button", { name: "auth.signIn" }));

    await waitFor(() => expect(pushMock).toHaveBeenCalledWith("/"));
    expect(refreshMock).toHaveBeenCalled();
  });

  it("shows p-alert-7 error alert on invalid credentials", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/login",
        method: "POST",
      })
      .reply(401, {
        error: {
          code: "invalid_credentials",
          message: "invalid email or password",
        },
      });

    renderLoginForm();

    fireEvent.change(screen.getByLabelText(/auth\.email/), {
      target: { value: "bad@example.com" },
    });
    fireEvent.change(screen.getByLabelText(/auth\.password/), {
      target: { value: "wrong" },
    });
    fireEvent.click(screen.getByRole("button", { name: "auth.signIn" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "auth.invalidCredentials",
      ),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "auth.loginFailedTitle",
    );
  });
});
