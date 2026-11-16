import { render, screen } from "@testing-library/react";
import { NextIntlClientProvider } from "next-intl";
import { describe, expect, it } from "vitest";
import { Spinner } from "@/components/ui/spinner";
import messages from "@/messages/en.json";

function renderSpinner(ui: React.ReactElement) {
  return render(
    <NextIntlClientProvider locale="en" messages={messages}>
      {ui}
    </NextIntlClientProvider>,
  );
}

describe("Spinner", () => {
  it("takes its accessible name from the common.loading message", () => {
    renderSpinner(<Spinner />);

    expect(
      screen.getByRole("status", { name: messages.common.loading }),
    ).toBeInTheDocument();
  });

  it("lets a caller override the accessible name", () => {
    renderSpinner(<Spinner aria-label="Saving" />);

    expect(screen.getByRole("status", { name: "Saving" })).toBeInTheDocument();
  });
});
