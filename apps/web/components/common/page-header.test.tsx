import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageHeader } from "@/components/common/page-header";

describe("PageHeader", () => {
  it("renders title and action slot", () => {
    const title = "Repositories";
    const description = "Manage monitored repositories.";
    const actionLabel = "Add repo";

    render(
      <PageHeader
        title={title}
        description={description}
        action={<button type="button">{actionLabel}</button>}
      />,
    );

    expect(
      screen.getByRole("heading", { level: 1, name: title }),
    ).toHaveAttribute("data-slot", "page-header-title");
    expect(screen.getByText(description)).toHaveAttribute(
      "data-slot",
      "page-header-description",
    );
    expect(
      screen.getByRole("button", { name: actionLabel }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: actionLabel }).parentElement,
    ).toHaveAttribute("data-slot", "page-header-action");
  });

  it("omits description and action when not provided", () => {
    const title = "Dashboard";

    const { container } = render(<PageHeader title={title} />);

    expect(
      screen.getByRole("heading", { level: 1, name: title }),
    ).toBeInTheDocument();
    expect(
      container.querySelector("[data-slot=page-header-description]"),
    ).toBeNull();
    expect(
      container.querySelector("[data-slot=page-header-action]"),
    ).toBeNull();
  });
});
