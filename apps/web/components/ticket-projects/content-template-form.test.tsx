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
      "Release: {{ .Repo.SourceKind }} {{ .Repo.ProjectPath }} {{ .Release.Tag }}",
    );
    expect(
      screen.getByLabelText("Description template").getAttribute("placeholder"),
    ).toMatch(/^\*\*Source:\*\* \{\{ \.Repo\.SourceKind \}\}\n/);
    expect(
      screen
        .getByLabelText("Supersede comment template")
        .getAttribute("placeholder"),
    ).toMatch(/^Superseded: \{\{ \.Supersede\.OldTag \}\}/);
  });

  it("uses the plain-text description default for Jira", () => {
    renderForm("jira");

    const placeholder = screen
      .getByLabelText("Description template")
      .getAttribute("placeholder");
    expect(placeholder).toBe(integrationDefaultTemplates("jira").description);
    expect(placeholder).toMatch(/^Source: \{\{ \.Repo\.SourceKind \}\}\n/);
    expect(placeholder).not.toContain("**");
  });

  it("shows the insertable syntax and where each variable works", () => {
    renderForm("kaneo");

    const tagRow = screen.getByText("{{ .Release.Tag }}").closest("tr");
    expect(tagRow).not.toBeNull();
    expect(
      within(tagRow as HTMLElement).getByText(
        "Title, Description, Supersede comment",
      ),
    ).toBeInTheDocument();

    const newTagRow = screen.getByText("{{ .Supersede.NewTag }}").closest("tr");
    expect(
      within(newTagRow as HTMLElement).getByText("Supersede comment"),
    ).toBeInTheDocument();
    expect(screen.queryByText(".Repo.SourceKind")).not.toBeInTheDocument();
  });

  it("copies the full syntax with a success toast", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const add = vi.spyOn(toastManager, "add").mockReturnValue("toast-id");

    renderForm("kaneo");
    fireEvent.click(screen.getByRole("button", { name: "Copy .Release.Tag" }));

    await vi.waitFor(() => {
      expect(add).toHaveBeenCalled();
    });
    expect(writeText).toHaveBeenCalledWith("{{ .Release.Tag }}");
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
    fireEvent.click(screen.getByRole("button", { name: "Copy .Release.Tag" }));

    await vi.waitFor(() => {
      expect(add).toHaveBeenCalled();
    });
    expect(add).toHaveBeenCalledWith(
      expect.objectContaining({ type: "error" }),
    );
  });
});
