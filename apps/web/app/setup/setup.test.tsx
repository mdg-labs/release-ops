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
import { SetupForm } from "./setup-form";

const ORIGIN = "http://localhost:3000";
const STATUS_PATH = "/api/go/api/v1/setup/status";
const SESSION_PATH = "/api/go/api/v1/setup/session";

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
  useFormatter: () => ({
    dateTime: (value: Date) => `at ${value.toISOString()}`,
  }),
}));

function renderSetupForm() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SetupForm />
    </QueryClientProvider>,
  );
}

function submitToken(token: string) {
  fireEvent.change(screen.getByLabelText(/setup\.tokenLabel/), {
    target: { value: token },
  });
  fireEvent.click(screen.getByRole("button", { name: "setup.submit" }));
}

describe("SetupForm", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  function pool() {
    return mockAgent.get(ORIGIN);
  }

  function statusIs(setupRequired: boolean) {
    pool()
      .intercept({ path: STATUS_PATH, method: "GET" })
      .reply(200, { setupRequired });
  }

  function noSession() {
    pool()
      .intercept({ path: SESSION_PATH, method: "GET" })
      .reply(401, { error: { code: "UNAUTHORIZED", message: "no session" } });
  }

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
    void mockAgent.close();
  });

  it("shows a loading state while the setup status is fetched", () => {
    statusIs(true);

    renderSetupForm();

    expect(screen.getByText("common.loading")).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/setup\.tokenLabel/),
    ).not.toBeInTheDocument();
  });

  it("opens a setup session when the token is accepted", async () => {
    statusIs(true);
    noSession();
    pool()
      .intercept({
        path: SESSION_PATH,
        method: "POST",
        body: JSON.stringify({ token: "the-token" }),
      })
      .reply(204, "");
    pool()
      .intercept({ path: SESSION_PATH, method: "GET" })
      .reply(200, { expiresAt: "2026-11-14T12:15:00.000Z" });

    renderSetupForm();
    await screen.findByLabelText(/setup\.tokenLabel/);
    submitToken("the-token");

    expect(
      await screen.findByText("setup.sessionOpenTitle"),
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/setup\.tokenLabel/),
    ).not.toBeInTheDocument();
  });

  it("shows an inline error and keeps the form for a wrong token", async () => {
    statusIs(true);
    noSession();
    pool()
      .intercept({ path: SESSION_PATH, method: "POST" })
      .reply(401, {
        error: { code: "INVALID_TOKEN", message: "Invalid setup token." },
      });

    renderSetupForm();
    await screen.findByLabelText(/setup\.tokenLabel/);
    submitToken("wrong");

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("setup.invalidToken"),
    );
    expect(screen.getByLabelText(/setup\.tokenLabel/)).toBeInTheDocument();
  });

  it("shows the rate-limited message on 429, not the invalid-token one", async () => {
    statusIs(true);
    noSession();
    pool()
      .intercept({ path: SESSION_PATH, method: "POST" })
      .reply(429, {
        error: { code: "RATE_LIMITED", message: "Too many requests." },
      });

    renderSetupForm();
    await screen.findByLabelText(/setup\.tokenLabel/);
    submitToken("whatever");

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("setup.rateLimited"),
    );
    expect(screen.getByRole("alert")).not.toHaveTextContent(
      "setup.invalidToken",
    );
  });

  it("shows a generic error when the request fails on the server", async () => {
    statusIs(true);
    noSession();
    pool()
      .intercept({ path: SESSION_PATH, method: "POST" })
      .reply(500, { error: { code: "INTERNAL_ERROR", message: "boom" } });

    renderSetupForm();
    await screen.findByLabelText(/setup\.tokenLabel/);
    submitToken("whatever");

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "setup.requestFailed",
      ),
    );
  });

  it("says the instance is already set up and links to sign-in", async () => {
    statusIs(false);

    renderSetupForm();

    expect(await screen.findByText("setup.alreadyTitle")).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "setup.goToLogin" }),
    ).toHaveAttribute("href", "/login");
    expect(
      screen.queryByLabelText(/setup\.tokenLabel/),
    ).not.toBeInTheDocument();
  });

  it("moves to the already-set-up message when the session route answers 404", async () => {
    statusIs(true);
    noSession();
    pool()
      .intercept({ path: SESSION_PATH, method: "POST" })
      .reply(404, { error: { code: "NOT_FOUND", message: "not found" } });
    statusIs(false);

    renderSetupForm();
    await screen.findByLabelText(/setup\.tokenLabel/);
    submitToken("late");

    expect(await screen.findByText("setup.alreadyTitle")).toBeInTheDocument();
  });

  it("shows an error, not the form or already-set-up, when the status cannot be read", async () => {
    pool()
      .intercept({ path: STATUS_PATH, method: "GET" })
      .reply(500, { error: { code: "INTERNAL_ERROR", message: "boom" } });

    renderSetupForm();

    expect(
      await screen.findByText("setup.statusFailedTitle"),
    ).toBeInTheDocument();
    expect(screen.queryByText("setup.alreadyTitle")).not.toBeInTheDocument();
    expect(
      screen.queryByLabelText(/setup\.tokenLabel/),
    ).not.toBeInTheDocument();
  });

  it("shows an error when the setup session cannot be read", async () => {
    statusIs(true);
    pool()
      .intercept({ path: SESSION_PATH, method: "GET" })
      .reply(500, { error: { code: "INTERNAL_ERROR", message: "boom" } });

    renderSetupForm();

    expect(
      await screen.findByText("setup.requestFailedTitle"),
    ).toBeInTheDocument();
    expect(
      screen.queryByLabelText(/setup\.tokenLabel/),
    ).not.toBeInTheDocument();
  });

  it("shows the open session when one already exists", async () => {
    statusIs(true);
    pool()
      .intercept({ path: SESSION_PATH, method: "GET" })
      .reply(200, { expiresAt: "2026-11-14T12:15:00.000Z" });

    renderSetupForm();

    expect(
      await screen.findByText("setup.sessionOpenTitle"),
    ).toBeInTheDocument();
  });
});
