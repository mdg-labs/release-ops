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
let searchParams = new URLSearchParams();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: pushMock, refresh: refreshMock }),
  useSearchParams: () => searchParams,
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

function replySetupStatus(setupRequired: boolean) {
  mockAgent
    .get(ORIGIN)
    .intercept({ path: "/api/go/api/v1/setup/status", method: "GET" })
    .reply(200, { setupRequired });
}

let mockAgent: MockAgent;

describe("LoginForm", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    pushMock.mockReset();
    refreshMock.mockReset();
    searchParams = new URLSearchParams();
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

  it("shows a loading state while the setup status loads, and no button yet", () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/setup/status", method: "GET" })
      .reply(200, { setupRequired: false });

    renderLoginForm();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "auth.signInWithGitHub" }),
    ).not.toBeInTheDocument();
  });

  it("offers Sign in with GitHub as a navigation to the Go login route", async () => {
    replySetupStatus(false);

    renderLoginForm();

    const button = await screen.findByRole("link", {
      name: "auth.signInWithGitHub",
    });
    expect(button).toHaveAttribute(
      "href",
      "/api/go/api/v1/auth/github/login?redirect=%2F",
    );
    expect(screen.queryByLabelText(/auth\.email/)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/auth\.password/)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("passes ?redirect= through safeRedirectPath", async () => {
    replySetupStatus(false);
    searchParams = new URLSearchParams({ redirect: "/jobs?view=problems" });

    renderLoginForm();

    expect(
      await screen.findByRole("link", { name: "auth.signInWithGitHub" }),
    ).toHaveAttribute(
      "href",
      "/api/go/api/v1/auth/github/login?redirect=%2Fjobs%3Fview%3Dproblems",
    );
  });

  it.each(["//evil.example", "/\\evil.example", "https://evil.example/"])(
    "sends a crafted redirect %s to /",
    async (redirect) => {
      replySetupStatus(false);
      searchParams = new URLSearchParams({ redirect });

      renderLoginForm();

      expect(
        await screen.findByRole("link", { name: "auth.signInWithGitHub" }),
      ).toHaveAttribute(
        "href",
        "/api/go/api/v1/auth/github/login?redirect=%2F",
      );
    },
  );

  it("shows the loading state of the button after the click, and clears it when the page is restored from the back-forward cache", async () => {
    replySetupStatus(false);

    renderLoginForm();

    const button = await screen.findByRole("link", {
      name: "auth.signInWithGitHub",
    });
    // The navigation itself is not performed by jsdom.
    button.addEventListener("click", (event) => event.preventDefault());
    fireEvent.click(button);
    await waitFor(() => expect(button).toHaveAttribute("data-loading"));

    const restored = new Event("pageshow") as PageTransitionEvent;
    Object.defineProperty(restored, "persisted", { value: true });
    window.dispatchEvent(restored);
    await waitFor(() => expect(button).not.toHaveAttribute("data-loading"));
  });

  it("shows setup required with a link to /setup and no sign-in button while no App is configured", async () => {
    replySetupStatus(true);

    renderLoginForm();

    expect(await screen.findByText("auth.setupRequired")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "auth.goToSetup" }),
    ).toHaveAttribute("href", "/setup");
    expect(
      screen.queryByRole("link", { name: "auth.signInWithGitHub" }),
    ).not.toBeInTheDocument();
  });

  it("shows an error, never the sign-in button or setup required, when the status cannot be read", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/setup/status", method: "GET" })
      .reply(500, { error: { code: "INTERNAL_ERROR", message: "x" } });

    renderLoginForm();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "auth.loginStatusFailed",
    );
    expect(screen.queryByText("auth.setupRequired")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("link", { name: "auth.signInWithGitHub" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "auth.tryAgain" }),
    ).toBeInTheDocument();
  });

  it("shows the rate-limited message when the status request is limited", async () => {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/setup/status", method: "GET" })
      .reply(429, { error: { code: "RATE_LIMITED", message: "x" } });

    renderLoginForm();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "auth.rateLimited",
    );
  });

  it.each([
    ["denied", "auth.loginErrorDenied"],
    ["no_role", "auth.loginErrorNoRole"],
    ["github_unavailable", "auth.loginErrorGitHubUnavailable"],
    ["exchange_failed", "auth.loginErrorExchangeFailed"],
    ["invalid_state", "auth.loginErrorInvalidState"],
    ["rate_limited", "auth.rateLimited"],
    ["something-else", "auth.loginFailed"],
  ])(
    "shows the fixed message for ?error=%s above the button",
    async (code, message) => {
      replySetupStatus(false);
      searchParams = new URLSearchParams({ error: code });

      renderLoginForm();

      const alert = await screen.findByRole("alert");
      expect(alert).toHaveTextContent(message);
      expect(alert).toHaveTextContent("auth.loginFailedTitle");
      expect(
        screen.getByRole("link", { name: "auth.signInWithGitHub" }),
      ).toBeInTheDocument();
    },
  );
});
