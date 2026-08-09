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
import { AppSidebar } from "@/components/app-sidebar";
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
