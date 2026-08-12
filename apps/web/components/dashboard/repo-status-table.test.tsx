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
    lastReleasePublishedAt: "2026-08-06T12:00:00.000Z",
    lastPolledAt: "2026-08-07T10:05:00.000Z",
    openTicketExternalId: null,
    openTicketTag: null,
    openTicketUrl: null,
    projectPath: `org/repo-${index}`,
    releaseUrl: `https://github.com/org/repo-${index}/releases/tag/v1.${index}.0`,
    repoUrl: `https://github.com/org/repo-${index}`,
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

  it("renders release date column with fallback when absent", () => {
    renderRepoStatusTable([
      {
        ...createRepos(1)[0],
        lastKnownTag: "v1.0.0",
        lastReleasePublishedAt: null,
        openTicketExternalId: "TICKET-1",
      },
    ]);

    expect(screen.getByText("Release date")).toBeTruthy();
    expect(screen.getAllByText("—")).toHaveLength(1);
  });

  it("renders formatted release date when present", () => {
    renderRepoStatusTable(createRepos(1));

    expect(screen.getByText("Release date")).toBeTruthy();
    expect(screen.getByText(/Aug 6, 2026/)).toBeTruthy();
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

  it("renders outbound links when URLs are present", () => {
    renderRepoStatusTable([
      {
        ...createRepos(1)[0],
        openTicketExternalId: "TASK-42",
        openTicketTag: "v1.0.0",
        openTicketUrl: "https://phasical.example/task/task-42",
      },
    ]);

    const repoLink = screen.getByRole("link", {
      name: "Open repository org/repo-0",
    });
    expect(repoLink.getAttribute("href")).toBe("https://github.com/org/repo-0");
    expect(repoLink.getAttribute("target")).toBe("_blank");
    expect(repoLink.getAttribute("rel")).toBe("noopener noreferrer");

    const releaseLink = screen.getByRole("link", {
      name: "View release v1.0.0",
    });
    expect(releaseLink.getAttribute("href")).toBe(
      "https://github.com/org/repo-0/releases/tag/v1.0.0",
    );
    expect(releaseLink.getAttribute("target")).toBe("_blank");
    expect(releaseLink.getAttribute("rel")).toBe("noopener noreferrer");

    const ticketLink = screen.getByRole("link", {
      name: "Open ticket TASK-42",
    });
    expect(ticketLink.getAttribute("href")).toBe(
      "https://phasical.example/task/task-42",
    );
    expect(ticketLink.getAttribute("target")).toBe("_blank");
    expect(ticketLink.getAttribute("rel")).toBe("noopener noreferrer");
  });

  it("renders plain text when outbound URLs are absent", () => {
    renderRepoStatusTable([
      {
        ...createRepos(1)[0],
        lastKnownTag: null,
        openTicketExternalId: "TASK-42",
        openTicketTag: "v1.0.0",
        openTicketUrl: null,
        releaseUrl: null,
        repoUrl: null,
      },
    ]);

    expect(screen.getByText("org/repo-0")).toBeTruthy();
    expect(screen.getAllByText("—")).toHaveLength(1);
    expect(screen.getByText("TASK-42")).toBeTruthy();
    expect(screen.queryByRole("link")).toBeNull();
  });
});

describe("RepoStatusTable pagination options", () => {
  it("exposes the configured page size options", () => {
    expect(PAGE_SIZE_OPTIONS).toEqual([10, 25, 50]);
  });
});
