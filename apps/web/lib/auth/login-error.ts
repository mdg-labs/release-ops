/**
 * Fixed error codes Go puts in `/login?error=`: the callback's (`denied`, `no_role`,
 * `github_unavailable`, `exchange_failed`, `invalid_state`, `server_error`) and the rate
 * limiter's `rate_limited` for the sign-in start and callback (see the `LoginError*` constants in
 * internal/api/auth/github_signin.go).
 * Each maps to a message key of the `auth` namespace; GitHub's own text never reaches the page,
 * and any other value shows the generic failure.
 */
const LOGIN_ERROR_KEYS = {
  denied: "loginErrorDenied",
  no_role: "loginErrorNoRole",
  github_unavailable: "loginErrorGitHubUnavailable",
  exchange_failed: "loginErrorExchangeFailed",
  invalid_state: "loginErrorInvalidState",
  rate_limited: "rateLimited",
  server_error: "loginFailed",
} as const;

export type LoginErrorKey =
  (typeof LOGIN_ERROR_KEYS)[keyof typeof LOGIN_ERROR_KEYS] | "loginFailed";

/** Message key for an `error` query value, or null when there is none. */
export function loginErrorKey(code: string | null): LoginErrorKey | null {
  if (!code) {
    return null;
  }
  return Object.hasOwn(LOGIN_ERROR_KEYS, code)
    ? LOGIN_ERROR_KEYS[code as keyof typeof LOGIN_ERROR_KEYS]
    : "loginFailed";
}

/** The Go route that starts the GitHub sign-in, through the same-origin proxy. */
export const GITHUB_LOGIN_PATH = "/api/go/api/v1/auth/github/login";

/** Sign-in start URL carrying the already-sanitised post-login path. */
export function githubLoginHref(redirectPath: string): string {
  return `${GITHUB_LOGIN_PATH}?redirect=${encodeURIComponent(redirectPath)}`;
}
