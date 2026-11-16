import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { MockAgent, setGlobalDispatcher } from "undici";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import messages from "@/messages/en.json";
import RepositoriesPage from "./page";

const ORIGIN = "http://localhost:3000";

describe("RepositoriesPage", () => {
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

  it("shows the repository list to a viewer", async () => {
    const pool = mockAgent.get(ORIGIN);
    const headers = { "content-type": "application/json" };
    pool
      .intercept({ path: "/api/go/api/v1/auth/session", method: "GET" })
      .reply(
        200,
        { user: { id: 1, login: "octocat", role: "viewer" } },
        { headers },
      )
      .persist();
    pool
      .intercept({
        path: /^\/api\/go\/api\/v1\/repositories\?/,
        method: "GET",
      })
      .reply(
        200,
        {
          items: [
            {
              id: "r1",
              fullName: "acme/api",
              defaultBranch: "main",
              enabled: true,
              configState: "present",
              htmlUrl: "https://github.com/acme/api",
              dashboardUrl: null,
              openUpdatePrs: 1,
              lastJob: null,
            },
          ],
          page: 1,
          pageSize: 25,
          total: 1,
        },
        { headers },
      )
      .persist();

    render(
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <QueryClientProvider client={new QueryClient()}>
          <RepositoriesPage />
        </QueryClientProvider>
      </NextIntlClientProvider>,
    );

    expect(screen.getByRole("heading", { name: "Repositories" })).toBeTruthy();
    expect(await screen.findByRole("link", { name: "acme/api" })).toBeTruthy();
  });
});
