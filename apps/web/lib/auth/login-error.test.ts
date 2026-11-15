import { describe, expect, it } from "vitest";
import { githubLoginHref, loginErrorKey } from "./login-error";

describe("loginErrorKey", () => {
  it("maps each fixed code from the callback to its own message", () => {
    expect(loginErrorKey("denied")).toBe("loginErrorDenied");
    expect(loginErrorKey("no_role")).toBe("loginErrorNoRole");
    expect(loginErrorKey("github_unavailable")).toBe(
      "loginErrorGitHubUnavailable",
    );
    expect(loginErrorKey("exchange_failed")).toBe("loginErrorExchangeFailed");
    expect(loginErrorKey("invalid_state")).toBe("loginErrorInvalidState");
    expect(loginErrorKey("rate_limited")).toBe("rateLimited");
  });

  it("shows no error without a code and the generic one for any other value", () => {
    expect(loginErrorKey(null)).toBeNull();
    expect(loginErrorKey("")).toBeNull();
    expect(loginErrorKey("<script>alert(1)</script>")).toBe("loginFailed");
    expect(loginErrorKey("constructor")).toBe("loginFailed");
    expect(loginErrorKey("__proto__")).toBe("loginFailed");
  });
});

describe("githubLoginHref", () => {
  it("encodes the post-login path as one query value", () => {
    expect(githubLoginHref("/jobs?view=problems&page=2")).toBe(
      "/api/go/api/v1/auth/github/login?redirect=%2Fjobs%3Fview%3Dproblems%26page%3D2",
    );
  });
});
