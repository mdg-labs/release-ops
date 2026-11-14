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
import { LoginLinkForm } from "./login-link-form";

const ORIGIN = "http://localhost:3000";

const pushMock = vi.fn();
const refreshMock = vi.fn();
let searchParams = new URLSearchParams();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: pushMock, refresh: refreshMock }),
  useSearchParams: () => searchParams,
}));

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
}));

function renderForm() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <LoginLinkForm />
    </QueryClientProvider>,
  );
}

describe("LoginLinkForm", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    pushMock.mockReset();
    refreshMock.mockReset();
    searchParams = new URLSearchParams({ token: "abc123" });
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

  afterEach(() => {
    cleanup();
    globalThis.fetch = originalFetch;
    void mockAgent.close();
  });

  it("sends nothing until the button is clicked, then posts the token and goes home", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({
        path: "/api/go/api/v1/auth/login-link",
        method: "POST",
        body: JSON.stringify({ token: "abc123" }),
      })
      .reply(200, { user: { id: 1, login: "octocat", role: "admin" } });

    renderForm();
    expect(mockAgent.pendingInterceptors()).toHaveLength(1);

    fireEvent.click(
      screen.getByRole("button", { name: "auth.loginLinkConfirm" }),
    );

    await waitFor(() => expect(pushMock).toHaveBeenCalledWith("/"));
    expect(refreshMock).toHaveBeenCalled();
  });

  it("shows the invalid-link message for a refused token and stays on the page", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/auth/login-link", method: "POST" })
      .reply(400, { error: { code: "INVALID_LINK", message: "x" } });

    renderForm();
    fireEvent.click(
      screen.getByRole("button", { name: "auth.loginLinkConfirm" }),
    );

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "auth.loginLinkInvalid",
    );
    expect(pushMock).not.toHaveBeenCalled();
  });

  it("shows the rate-limited message on 429 and a generic one on a server error", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({ path: "/api/go/api/v1/auth/login-link", method: "POST" })
      .reply(429, { error: { code: "RATE_LIMITED", message: "x" } });
    pool
      .intercept({ path: "/api/go/api/v1/auth/login-link", method: "POST" })
      .reply(500, { error: { code: "INTERNAL_ERROR", message: "x" } });

    renderForm();
    const button = screen.getByRole("button", {
      name: "auth.loginLinkConfirm",
    });
    fireEvent.click(button);
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("auth.rateLimited"),
    );
    fireEvent.click(button);
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("auth.loginFailed"),
    );
    expect(pushMock).not.toHaveBeenCalled();
  });

  it("disables the button and explains when the link has no token", () => {
    searchParams = new URLSearchParams();

    renderForm();

    expect(screen.getByRole("alert")).toHaveTextContent(
      "auth.loginLinkInvalid",
    );
    expect(
      screen.getByRole("button", { name: "auth.loginLinkConfirm" }),
    ).toBeDisabled();
  });
});
