import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { MockAgent, setGlobalDispatcher } from "undici";
import { ForgotPasswordForm } from "./forgot-password-form";

const ORIGIN = "http://localhost:3000";

vi.mock("next-intl", () => ({
  useTranslations: (namespace: string) => (key: string) =>
    `${namespace}.${key}`,
}));

describe("ForgotPasswordForm", () => {
  let mockAgent: MockAgent;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    mockAgent = new MockAgent();
    setGlobalDispatcher(mockAgent);
    mockAgent.disableNetConnect();

    globalThis.fetch = ((input: RequestInfo | URL, init?: RequestInit) => {
      const url =
        typeof input === "string" && input.startsWith("/")
          ? `${ORIGIN}${input}`
          : input instanceof URL
            ? input
            : input instanceof Request
              ? input.url.startsWith("/")
                ? `${ORIGIN}${input.url}`
                : input.url
              : input;
      return originalFetch(url, init);
    }) as typeof fetch;
  });

  afterEach(() => {
    cleanup();
    globalThis.fetch = originalFetch;
    void mockAgent.close();
  });

  it("shows generic success state after submit", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/forgot-password",
        method: "POST",
        body: JSON.stringify({ email: "user@example.com" }),
      })
      .reply(200, {
        message:
          "If an account exists for that email, a reset link has been sent.",
      });

    render(<ForgotPasswordForm />);

    fireEvent.change(screen.getByLabelText(/auth\.email/), {
      target: { value: "user@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "auth.sendResetLink" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        "auth.forgotPasswordSuccess",
      ),
    );
    expect(screen.queryByLabelText(/auth\.email/)).not.toBeInTheDocument();
  });

  it("shows rate limit error on 429", async () => {
    const pool = mockAgent.get(ORIGIN);
    pool
      .intercept({
        path: "/api/go/api/v1/auth/forgot-password",
        method: "POST",
      })
      .reply(429, {
        error: {
          code: "RATE_LIMITED",
          message: "Too many requests. Try again later.",
        },
      });

    render(<ForgotPasswordForm />);

    fireEvent.change(screen.getByLabelText(/auth\.email/), {
      target: { value: "user@example.com" },
    });
    fireEvent.click(screen.getByRole("button", { name: "auth.sendResetLink" }));

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("auth.rateLimited"),
    );
  });
});
