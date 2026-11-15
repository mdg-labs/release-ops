import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ContentTemplateForm } from "@/components/ticket-projects/content-template-form";
import { toastManager } from "@/components/ui/toast";
import type { TicketIntegrationKind } from "@/lib/integrations/kinds";
import { integrationDefaultTemplates } from "@/lib/ticket-projects/content-templates";
import messages from "@/messages/en.json";

const EMPTY = { title: "", description: "", supersedeComment: "" };

function renderForm(kind: TicketIntegrationKind | undefined) {
  return render(
    <NextIntlClientProvider locale="en" messages={messages}>
      <ContentTemplateForm kind={kind} onChange={() => {}} values={EMPTY} />
    </NextIntlClientProvider>,
  );
}

describe("ContentTemplateForm", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("uses the markdown defaults as placeholders for Kaneo", () => {
    renderForm("kaneo");

    expect(screen.getByLabelText("Title template")).toHaveAttribute(
      "placeholder",
      "{{ .PullRequest.Title }} ({{ .Repository }})",
    );
    expect(
      screen.getByLabelText("Description template").getAttribute("placeholder"),
    ).toMatch(/^\*\*Repository:\*\* \{\{ \.Repository \}\}\n/);
    expect(
      screen
        .getByLabelText("Supersede comment template")
        .getAttribute("placeholder"),
    ).toMatch(/^Superseded by \{\{ \.Supersede\.NewPullRequest\.URL \}\}/);
  });

  it("uses the plain-text description default for Jira", () => {
    renderForm("jira");

    const placeholder = screen
      .getByLabelText("Description template")
      .getAttribute("placeholder");
    expect(placeholder).toBe(integrationDefaultTemplates("jira").description);
    expect(placeholder).toMatch(/^Repository: \{\{ \.Repository \}\}\n/);
    expect(placeholder).not.toContain("**");
  });

  it("shows the insertable syntax and where each variable works", () => {
    renderForm("kaneo");

    const urlRow = screen.getByText("{{ .PullRequest.URL }}").closest("tr");
    expect(urlRow).not.toBeNull();
    expect(
      within(urlRow as HTMLElement).getByText(
        "Title, Description, Update comment, Supersede comment",
      ),
    ).toBeInTheDocument();

    const notesRow = screen.getByText("{{ .ReleaseNotes }}").closest("tr");
    expect(
      within(notesRow as HTMLElement).getByText(
        "Title, Description, Update comment",
      ),
    ).toBeInTheDocument();

    const newPrRow = screen
      .getByText("{{ .Supersede.NewPullRequest.URL }}")
      .closest("tr");
    expect(
      within(newPrRow as HTMLElement).getByText("Supersede comment"),
    ).toBeInTheDocument();
    expect(screen.queryByText("{{ .Release.Tag }}")).not.toBeInTheDocument();
    expect(
      screen.queryByText("{{ .Repo.SourceKind }}"),
    ).not.toBeInTheDocument();
  });

  it("copies the full syntax with a success toast", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const add = vi.spyOn(toastManager, "add").mockReturnValue("toast-id");

    renderForm("kaneo");
    fireEvent.click(screen.getByRole("button", { name: "Copy .Repository" }));

    await vi.waitFor(() => {
      expect(add).toHaveBeenCalled();
    });
    expect(writeText).toHaveBeenCalledWith("{{ .Repository }}");
    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "success" }),
    );
  });

  it("shows an error toast when the clipboard write fails", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const add = vi.spyOn(toastManager, "add").mockReturnValue("toast-id");

    renderForm("kaneo");
    fireEvent.click(screen.getByRole("button", { name: "Copy .Repository" }));

    await vi.waitFor(() => {
      expect(add).toHaveBeenCalled();
    });
    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" }),
    );
  });
});
