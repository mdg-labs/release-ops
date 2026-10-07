import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import { StatusCard } from "@/components/dashboard/status-card";
import type { StatusResponse } from "@/lib/query/types";
import messages from "@/messages/en.json";

function statusWithRun(runStatus: string): StatusResponse {
  return {
    pollIntervalMinutes: 360,
    lastRun: {
      id: "run-1",
      startedAt: "2026-08-07T10:00:00.000Z",
      finishedAt: "2026-08-07T10:05:00.000Z",
      status: runStatus,
      triggerSource: "manual",
      reposChecked: 2,
      ticketsCreated: 0,
      ticketsSuperseded: 0,
      errors: [],
    },
    repos: [],
    isPolling: false,
    version: "0.0.0-test",
  };
}

function renderCard(status: StatusResponse): void {
  render(
    <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
      <StatusCard
        isLoading={false}
        isPolling={false}
        isTriggerPending={false}
        onRunPoll={vi.fn()}
        status={status}
      />
    </NextIntlClientProvider>,
  );
}

describe("StatusCard run status badge", () => {
  afterEach(() => {
    cleanup();
  });

  it("labels a partial run as Partial, not the raw status", () => {
    renderCard(statusWithRun("partial"));

    expect(screen.getByText("Partial")).toBeTruthy();
    expect(screen.queryByText("partial")).toBeNull();
  });

  it("shows a fallback label for an unknown status", () => {
    renderCard(statusWithRun("cancelled"));

    expect(screen.getByText("Unknown")).toBeTruthy();
    expect(screen.queryByText("cancelled")).toBeNull();
  });
});
