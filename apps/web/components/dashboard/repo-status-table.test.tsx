import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { RepoStatusTable } from "@/components/dashboard/repo-status-table";
import {
  PAGE_SIZE_OPTIONS,
  REPO_STATUS_PAGE_SIZE_STORAGE_KEY,
} from "@/lib/pagination/constants";
import messages from "@/messages/en.json";
import type { StatusRepo } from "@/lib/query/types";

function createRepos(count: number): StatusRepo[] {
  return Array.from({ length: count }, (_, index) => ({
    enabled: true,
    id: `repo-${index}`,
    lastError: null,
    lastKnownTag: `v1.${index}.0`,
    lastPolledAt: "2026-08-07T10:05:00.000Z",
    openTicketExternalId: null,
    openTicketTag: null,
    projectPath: `org/repo-${index}`,
    sourceKind: "github",
    ticketProjectId: "tp-1",
    ticketProjectName: "Jira — DEV",
  }));
}

function createLocalStorageMock(): Storage {
  let store: Record<string, string> = {};

  return {
    get length() {
      return Object.keys(store).length;
    },
    clear() {
      store = {};
    },
    getItem(key: string) {
      return store[key] ?? null;
    },
    key(index: number) {
      return Object.keys(store)[index] ?? null;
    },
    removeItem(key: string) {
      delete store[key];
    },
    setItem(key: string, value: string) {
      store[key] = value;
    },
  };
}

async function selectPageSize(size: number): Promise<void> {
  const pageSizeSelect = await screen.findByRole("combobox", {
    name: "Rows per page",
  });
  fireEvent.click(pageSizeSelect);
  await waitFor(() => {
    expect(screen.getByRole("listbox")).toBeInTheDocument();
  });

  const option = screen.getByRole("option", { name: String(size) });
  fireEvent.pointerDown(option, {
    buttons: 1,
    pointerId: 1,
    pointerType: "mouse",
  });
  fireEvent.pointerUp(option, {
    pointerId: 1,
    pointerType: "mouse",
  });
  fireEvent.click(option);
}

function renderRepoStatusTable(repos: StatusRepo[]): void {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  render(
    <QueryClientProvider client={queryClient}>
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <RepoStatusTable isLoading={false} repos={repos} />
      </NextIntlClientProvider>
    </QueryClientProvider>,
  );
}

describe("RepoStatusTable", () => {
  let localStorageMock: Storage;

  beforeEach(() => {
    localStorageMock = createLocalStorageMock();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: localStorageMock,
    });
  });

  afterEach(() => {
    cleanup();
  });

  it("hydrates page size from localStorage", async () => {
    localStorageMock.setItem(REPO_STATUS_PAGE_SIZE_STORAGE_KEY, "25");

    renderRepoStatusTable(createRepos(30));

    await waitFor(() => {
      expect(screen.getAllByText(/org\/repo-/)).toHaveLength(25);
    });
  });

  it("persists page size changes to localStorage", async () => {
    renderRepoStatusTable(createRepos(30));

    await selectPageSize(25);

    await waitFor(() => {
      expect(localStorageMock.getItem(REPO_STATUS_PAGE_SIZE_STORAGE_KEY)).toBe(
        "25",
      );
    });
    expect(screen.getAllByText(/org\/repo-/)).toHaveLength(25);
  });

  it("resets to the first page when page size changes", async () => {
    renderRepoStatusTable(createRepos(30));

    const nextButton = await screen.findByRole("button", { name: "Next" });
    fireEvent.click(nextButton);

    await waitFor(() => {
      expect(screen.getByText("org/repo-10")).toBeTruthy();
    });

    await selectPageSize(25);

    await waitFor(() => {
      expect(screen.getByText("org/repo-0")).toBeTruthy();
    });
    expect(screen.queryByText("org/repo-25")).toBeNull();
  });
});

describe("RepoStatusTable pagination options", () => {
  it("exposes the configured page size options", () => {
    expect(PAGE_SIZE_OPTIONS).toEqual([10, 25, 50]);
  });
});
