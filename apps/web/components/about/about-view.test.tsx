import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { AboutView } from "@/components/about/about-view";
import messages from "@/messages/en.json";

const ORIGIN = "http://localhost:3000";
const REPO_URL = "https://github.com/mdg-labs/release-ops";

function renderView() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <QueryClientProvider client={queryClient}>
        <AboutView />
      </QueryClientProvider>
    </NextIntlClientProvider>,
  );
}

describe("AboutView", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
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

  afterEach(async () => {
    cleanup();
    globalThis.fetch = originalFetch;
    await mockAgent.close();
  });

  function intercept(status: number, body: object) {
    mockAgent
      .get(ORIGIN)
      .intercept({ path: "/api/go/api/v1/about", method: "GET" })
      .reply(status, body, { headers: { "content-type": "application/json" } });
  }

  it("shows the version, the pinned Renovate and the licence links", async () => {
    intercept(200, {
      version: "v0.2.0",
      renovate: {
        version: "44.239.1",
        license: "AGPL-3.0-only",
        sourceUrl: "https://github.com/renovatebot/renovate/tree/44.239.1",
      },
    });
    renderView();

    expect(await screen.findByTestId("about-version")).toHaveTextContent(
      "v0.2.0",
    );
    expect(screen.getByTestId("about-renovate-version")).toHaveTextContent(
      "44.239.1",
    );
    expect(screen.getByTestId("about-renovate-license")).toHaveTextContent(
      "AGPL-3.0-only",
    );
    expect(
      screen.getByRole("link", { name: messages.about.renovate.sourceLink }),
    ).toHaveAttribute(
      "href",
      "https://github.com/renovatebot/renovate/tree/44.239.1",
    );
    for (const link of screen.getAllByRole("link", {
      name: messages.about.app.noticeLink,
    })) {
      expect(link).toHaveAttribute("href", `${REPO_URL}/blob/main/NOTICE`);
    }
    expect(
      screen.getByRole("link", { name: messages.about.app.licenseLink }),
    ).toHaveAttribute("href", `${REPO_URL}/blob/main/LICENSE`);
  });

  it("shows an error, not an empty page, when the request fails", async () => {
    intercept(500, { error: {} });
    renderView();

    expect(
      await screen.findByText(messages.about.loadFailed),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("about-version")).toBeNull();
  });
});
