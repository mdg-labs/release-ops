const INTERNAL_BASE = "http://internal.invalid";

/**
 * Returns `value` when it is a same-origin path (e.g. `/repos?page=2`), otherwise `/`.
 * Rejects absolute URLs and protocol-relative forms such as `//evil.example` or
 * `/\evil.example` so a crafted `?redirect=` cannot send a user off-site after login.
 */
export function safeRedirectPath(value: string | null | undefined): string {
  if (!value || !value.startsWith("/") || value.startsWith("//")) {
    return "/";
  }
  if (value.startsWith("/\\")) {
    return "/";
  }
  try {
    const resolved = new URL(value, INTERNAL_BASE);
    if (resolved.origin !== INTERNAL_BASE) {
      return "/";
    }
    return `${resolved.pathname}${resolved.search}${resolved.hash}`;
  } catch {
    return "/";
  }
}
