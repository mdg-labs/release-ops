import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
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
const MANIFEST_PATH = "/api/go/api/v1/setup/manifest";

const submitManifestForm = vi.fn();
vi.mock("@/lib/setup/submit-manifest", () => ({
  submitManifestForm: (...args: unknown[]) => submitManifestForm(...args),
}));

let searchParams = new URLSearchParams();
vi.mock("next/navigation", () => ({
  useSearchParams: () => searchParams,
}));

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

  function openSession() {
    statusIs(true);
    pool()
      .intercept({ path: SESSION_PATH, method: "GET" })
      .reply(200, { expiresAt: "2026-11-14T12:15:00.000Z" });
  }

  beforeEach(() => {
    submitManifestForm.mockReset();
    searchParams = new URLSearchParams();
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

  it.each([
    ["invalid_state", "setup.callbackInvalidState"],
    ["conversion_rejected", "setup.callbackRejected"],
    ["conversion_failed", "setup.callbackFailed"],
    ["session_expired", "setup.callbackSessionExpired"],
    ["something_else", "setup.requestFailed"],
  ])(
    "shows an inline error and the form again when GitHub's callback ends in %s",
    async (code, message) => {
      searchParams = new URLSearchParams({ error: code });
      openSession();

      renderSetupForm();

      expect(await screen.findByText(message)).toBeInTheDocument();
      expect(
        screen.getByRole("button", { name: "setup.createApp" }),
      ).toBeEnabled();
    },
  );

  it("tells the admin to restart the server when GitHub's callback finds the setup session gone", async () => {
    searchParams = new URLSearchParams({ error: "session_expired" });
    statusIs(true);
    noSession();

    renderSetupForm();

    expect(
      await screen.findByText("setup.callbackSessionExpiredTitle"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("setup.callbackSessionExpired"),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/setup\.tokenLabel/)).toBeInTheDocument();
  });

  it("posts the manifest to GitHub for a personal App", async () => {
    openSession();
    pool()
      .intercept({
        path: MANIFEST_PATH,
        method: "POST",
        body: JSON.stringify({ owner: { type: "user" } }),
      })
      .reply(200, {
        url: "https://github.com/settings/apps/new?state=s1",
        manifest: { name: "Release Ops", public: false },
      });

    renderSetupForm();
    fireEvent.click(
      await screen.findByRole("button", { name: "setup.createApp" }),
    );

    await waitFor(() =>
      expect(submitManifestForm).toHaveBeenCalledWith(
        "https://github.com/settings/apps/new?state=s1",
        { name: "Release Ops", public: false },
      ),
    );
    expect(
      screen.getByRole("button", { name: /setup\.createApp/ }),
    ).toBeDisabled();
  });

  it("re-enables the form when the page is restored from the back-forward cache", async () => {
    openSession();
    pool()
      .intercept({ path: MANIFEST_PATH, method: "POST" })
      .reply(200, {
        url: "https://github.com/settings/apps/new?state=s1",
        manifest: { name: "Release Ops", public: false },
      });

    renderSetupForm();
    fireEvent.click(
      await screen.findByRole("button", { name: "setup.createApp" }),
    );
    await waitFor(() => expect(submitManifestForm).toHaveBeenCalled());
    const button = screen.getByRole("button", { name: /setup\.createApp/ });
    expect(button).toBeDisabled();

    act(() => {
      window.dispatchEvent(new PageTransitionEvent("pageshow"));
    });
    expect(button).toBeDisabled();

    act(() => {
      window.dispatchEvent(
        new PageTransitionEvent("pageshow", { persisted: true }),
      );
    });
    await waitFor(() => expect(button).toBeEnabled());
  });

  it("asks for the organization name and sends it with the App name", async () => {
    openSession();
    pool()
      .intercept({
        path: MANIFEST_PATH,
        method: "POST",
        body: JSON.stringify({
          owner: { type: "organization", login: "acme" },
          name: "Acme Releases",
        }),
      })
      .reply(200, {
        url: "https://github.com/organizations/acme/settings/apps/new?state=s2",
        manifest: { name: "Acme Releases" },
      });

    renderSetupForm();
    expect(screen.queryByLabelText(/setup\.organizationLabel/)).toBeNull();
    fireEvent.click(await screen.findByRole("combobox"));
    await waitFor(() => {
      expect(screen.getByRole("listbox")).toBeInTheDocument();
    });
    const option = screen.getByRole("option", {
      name: "setup.ownerOrganization",
    });
    fireEvent.pointerDown(option, {
      pointerId: 1,
      pointerType: "mouse",
      buttons: 1,
    });
    fireEvent.pointerUp(option, { pointerId: 1, pointerType: "mouse" });
    fireEvent.click(option);
    fireEvent.change(await screen.findByLabelText(/setup\.organizationLabel/), {
      target: { value: " acme " },
    });
    fireEvent.change(screen.getByLabelText("setup.nameLabel"), {
      target: { value: "Acme Releases" },
    });
    fireEvent.click(screen.getByRole("button", { name: "setup.createApp" }));

    await waitFor(() =>
      expect(submitManifestForm).toHaveBeenCalledWith(
        "https://github.com/organizations/acme/settings/apps/new?state=s2",
        { name: "Acme Releases" },
      ),
    );
  });

  it("shows an inline error and does not leave for GitHub when the manifest request fails", async () => {
    openSession();
    pool()
      .intercept({ path: MANIFEST_PATH, method: "POST" })
      .reply(400, {
        error: {
          code: "VALIDATION_ERROR",
          message: "Enter the organization's GitHub name.",
        },
      });

    renderSetupForm();
    fireEvent.click(
      await screen.findByRole("button", { name: "setup.createApp" }),
    );

    expect(
      await screen.findByText("setup.invalidOwnerOrName"),
    ).toBeInTheDocument();
    expect(submitManifestForm).not.toHaveBeenCalled();
    expect(
      screen.getByRole("button", { name: "setup.createApp" }),
    ).toBeEnabled();
  });

  it("returns to the token form when the setup session has ended", async () => {
    openSession();
    pool()
      .intercept({ path: MANIFEST_PATH, method: "POST" })
      .reply(401, {
        error: {
          code: "UNAUTHORIZED",
          message: "A setup session is required.",
        },
      });
    statusIs(true);
    noSession();

    renderSetupForm();
    fireEvent.click(
      await screen.findByRole("button", { name: "setup.createApp" }),
    );

    expect(
      await screen.findByLabelText(/setup\.tokenLabel/),
    ).toBeInTheDocument();
    expect(submitManifestForm).not.toHaveBeenCalled();
  });

  it("points to the App's installation page when the App exists but is not installed", async () => {
    pool().intercept({ path: STATUS_PATH, method: "GET" }).reply(200, {
      setupRequired: false,
      installUrl: "https://github.com/apps/release-ops/installations/new",
    });

    renderSetupForm();

    expect(
      await screen.findByRole("link", { name: "setup.installApp" }),
    ).toHaveAttribute(
      "href",
      "https://github.com/apps/release-ops/installations/new",
    );
    expect(
      screen.getByRole("link", { name: "setup.goToLogin" }),
    ).toHaveAttribute("href", "/login");
  });
});
