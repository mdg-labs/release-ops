import { render, screen } from "@testing-library/react";
import { FolderIcon } from "lucide-react";
import { describe, expect, it } from "vitest";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";

describe("Empty", () => {
  it("renders all composition slots", () => {
    const title = "No items";
    const description = "Add your first item to get started.";
    const actionLabel = "Add item";

    render(
      <Empty data-testid="empty-root">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <FolderIcon aria-hidden="true" />
          </EmptyMedia>
          <EmptyTitle>{title}</EmptyTitle>
          <EmptyDescription>{description}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <button type="button">{actionLabel}</button>
        </EmptyContent>
      </Empty>,
    );

    expect(screen.getByTestId("empty-root")).toHaveAttribute(
      "data-slot",
      "empty",
    );
    expect(screen.getByText(title)).toHaveAttribute("data-slot", "empty-title");
    expect(screen.getByText(description)).toHaveAttribute(
      "data-slot",
      "empty-description",
    );
    expect(
      screen.getByRole("button", { name: actionLabel }),
    ).toBeInTheDocument();
    expect(
      screen.getByTestId("empty-root").querySelector("[data-slot=empty-media]"),
    ).toHaveAttribute("data-variant", "icon");
  });
});
