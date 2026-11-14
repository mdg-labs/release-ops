import { cleanup, render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import messages from "@/messages/en.json";
import DashboardPage from "./page";

describe("DashboardPage", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("renders the empty state without calling the API", () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");

    const { container } = render(
      <NextIntlClientProvider locale="en" messages={messages} timeZone="UTC">
        <DashboardPage />
      </NextIntlClientProvider>,
    );

    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeTruthy();
    expect(screen.getByText("Nothing to show yet")).toBeTruthy();
    expect(container.querySelector(".min-w-0.w-full")).toBeTruthy();
    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
