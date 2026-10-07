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
import { AppSidebar, versionHref } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";

const ORIGIN = "http://localhost:3000";
const NAV_ROUTES = [
  "/",
  "/poll-runs",
  "/repos",
  "/integrations",
  "/ticket-projects",
  "/notifications",
  "/settings",
] as const;

const pushMock = vi.fn();

vi.mock("next/navigation", () => ({
  usePathname: () => "/repos",
  useRouter: () => ({ push: pushMock }),
}));

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
}));

vi.mock("next/link", () => ({
  default: ({
    children,
    href,
    ...props
  }: {
    children: React.ReactNode;
    href: string;
  }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("@/hooks/use-media-query", () => ({
  useMediaQuery: () => false,
}));

vi.mock("@/lib/hooks/use-session", () => ({
  useSession: () => ({
    data: { user: { id: "user-1", email: "admin@example.com" } },
    isLoading: false,
    isError: false,
  }),
}));

const REPO_URL = "https://github.com/mdg-labs/release-ops";

function interceptStatus(
  pool: ReturnType<MockAgent["get"]>,
  reply: { version?: string; statusCode?: number },
) {
  const statusCode = reply.statusCode ?? 200;
  pool
    .intercept({ path: "/api/go/api/v1/status", method: "GET" })
    .reply(
      statusCode,
      statusCode === 200 ? { version: reply.version } : { error: {} },
      { headers: { "content-type": "application/json" } },
    );
}

function renderSidebar() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SidebarProvider>
        <AppSidebar />
      </SidebarProvider>
    </QueryClientProvider>,
  );
}

describe("AppSidebar", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    pushMock.mockReset();
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

    vi.stubGlobal("cookieStore", {
      set: vi.fn().mockResolvedValue(undefined),
    });
  });

  afterEach(async () => {
    cleanup();
    globalThis.fetch = originalFetch;
    await mockAgent.close();
    vi.unstubAllGlobals();
  });

  it("renders sidebar links for all §8.1 routes", () => {
    renderSidebar();

    for (const href of NAV_ROUTES) {
      expect(
        screen.getByRole("link", { name: `nav.${hrefToKey(href)}` }),
      ).toHaveAttribute("href", href);
    }
  });

  it("renders profile link with session email", () => {
    renderSidebar();

    const profileLink = screen.getByRole("link", { name: "admin@example.com" });
    expect(profileLink).toHaveAttribute("href", "/profile");
  });

  it("links the wordmark to the repository in a new tab", () => {
    renderSidebar();

    const wordmark = screen.getByTestId("sidebar-wordmark");
    expect(wordmark).toHaveTextContent("common.appTitle");
    expect(wordmark).toHaveAttribute("href", REPO_URL);
    expect(wordmark).toHaveAttribute("target", "_blank");
    expect(wordmark).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("shows a release version linked to its release page", async () => {
    interceptStatus(mockAgent.get(ORIGIN), { version: "v0.1.0" });

    renderSidebar();

    const badge = await screen.findByTestId("sidebar-version");
    expect(badge).toHaveTextContent("v0.1.0");
    expect(badge).toHaveAttribute("href", `${REPO_URL}/releases/tag/v0.1.0`);
    expect(badge).toHaveAttribute("target", "_blank");
    expect(badge).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("shows a nightly version linked to its commit", async () => {
    interceptStatus(mockAgent.get(ORIGIN), { version: "nightly-abc1234" });

    renderSidebar();

    const badge = await screen.findByTestId("sidebar-version");
    expect(badge).toHaveTextContent("nightly-abc1234");
    expect(badge).toHaveAttribute("href", `${REPO_URL}/commit/abc1234`);
  });

  it("shows the dev version without a link", async () => {
    interceptStatus(mockAgent.get(ORIGIN), { version: "dev" });

    renderSidebar();

    const badge = await screen.findByTestId("sidebar-version");
    expect(badge).toHaveTextContent("dev");
    expect(badge).not.toHaveAttribute("href");
  });

  it("renders no version badge when the status request fails", async () => {
    interceptStatus(mockAgent.get(ORIGIN), { statusCode: 500 });

    renderSidebar();

    await waitFor(() =>
      expect(mockAgent.pendingInterceptors()).toHaveLength(0),
    );
    expect(screen.queryByTestId("sidebar-version")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-wordmark")).toHaveAttribute(
      "href",
      REPO_URL,
    );
  });

  it("posts to auth logout when logout is clicked", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/logout",
        method: "POST",
      })
      .reply(204);

    renderSidebar();

    fireEvent.click(screen.getByTestId("sidebar-logout"));

    await waitFor(() => expect(pushMock).toHaveBeenCalledWith("/login"));
  });
});

describe("versionHref", () => {
  it.each([
    ["v0.1.0", `${REPO_URL}/releases/tag/v0.1.0`],
    ["v1.2.3-rc.1", `${REPO_URL}/releases/tag/v1.2.3-rc.1`],
    ["v1.2.3+build.5", `${REPO_URL}/releases/tag/v1.2.3+build.5`],
    ["v1.2.3-rc.1+build.5", `${REPO_URL}/releases/tag/v1.2.3-rc.1+build.5`],
    ["v1.2.3+build.5-rc.1+x", null],
    ["nightly-abc1234", `${REPO_URL}/commit/abc1234`],
    ["dev", null],
    ["", null],
    ["0.1.0", null],
    ["v1.0.0/../../x", null],
    ["nightly-xyz", null],
  ])("maps %j to %j", (version, expected) => {
    expect(versionHref(version)).toBe(expected);
  });
});

function hrefToKey(href: (typeof NAV_ROUTES)[number]): string {
  if (href === "/") {
    return "dashboard";
  }

  if (href === "/ticket-projects") {
    return "ticketProjects";
  }

  if (href === "/poll-runs") {
    return "pollRuns";
  }

  return href.slice(1);
}
