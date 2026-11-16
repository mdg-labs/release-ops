import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { queryKeys } from "@/lib/query/keys";
import type { Settings } from "@/lib/query/types";
import { useSettings, useUpdateSettings } from "./use-settings";

const JSON_HEADERS = { headers: { "content-type": "application/json" } };

const before = { concurrencyCap: 2, mode: "webhook" } as Settings;
const after = { concurrencyCap: 5, mode: "webhook" } as Settings;

describe("useUpdateSettings", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("keeps the saved settings when a GET that started earlier resolves after the PATCH", async () => {
    let releaseGet: () => void = () => undefined;
    globalThis.fetch = ((_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "PATCH") {
        return Promise.resolve(
          new Response(JSON.stringify(after), { status: 200, ...JSON_HEADERS }),
        );
      }
      return new Promise<Response>((resolve) => {
        releaseGet = () =>
          resolve(
            new Response(JSON.stringify(before), {
              status: 200,
              ...JSON_HEADERS,
            }),
          );
      });
    }) as typeof fetch;

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const { result } = renderHook(
      () => ({ settings: useSettings(), update: useUpdateSettings() }),
      {
        wrapper: ({ children }: { children: React.ReactNode }) => (
          <QueryClientProvider client={queryClient}>
            {children}
          </QueryClientProvider>
        ),
      },
    );
    await waitFor(() => expect(result.current.settings.isFetching).toBe(true));

    await act(async () => {
      await result.current.update.mutateAsync({ concurrencyCap: 5 });
    });
    releaseGet();
    await new Promise((resolve) => setTimeout(resolve, 20));

    expect(queryClient.getQueryData(queryKeys.settings())).toEqual(after);
  });
});
