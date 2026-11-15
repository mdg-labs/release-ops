import { NextRequest, NextResponse } from "next/server";

/** Browser-facing path via the Next.js proxy (matcher exclusions, docs). */
export const SESSION_API_PATH = "/api/go/api/v1/auth/session";

const DEFAULT_GO_API_URL = "http://127.0.0.1:8080";
const GO_SESSION_PATH = "/api/v1/auth/session";
const LOGIN_PATH = "/login";

export const PUBLIC_AUTH_PATHS = ["/login", "/login/link", "/setup"] as const;

function getSessionCheckUrl(): string {
  const base = (process.env.GO_API_URL ?? DEFAULT_GO_API_URL).replace(
    /\/$/,
    "",
  );
  return `${base}${GO_SESSION_PATH}`;
}

// A GitHub sign-in answers { id (numeric GitHub id), login, role }; a local account, which
// goes away with the local accounts, answers { id, email }.
type SessionResponse = {
  user: { id: string | number; login?: string; email?: string } | null;
};

export function isPublicAuthPath(pathname: string): boolean {
  return PUBLIC_AUTH_PATHS.some(
    (path) => pathname === path || pathname.startsWith(`${path}/`),
  );
}

export function isLoginPath(pathname: string): boolean {
  return pathname === LOGIN_PATH || pathname.startsWith(`${LOGIN_PATH}/`);
}

/**
 * What the session endpoint says about the request: a signed-in user, no session, or that it
 * cannot tell right now. GitHub being unreachable for the role re-check answers 503 and the
 * session stays valid, so that is not "no session".
 */
export type SessionState =
  | { kind: "user"; user: NonNullable<SessionResponse["user"]> }
  | { kind: "none" }
  | { kind: "unavailable" };

export async function getSessionState(
  request: NextRequest,
): Promise<SessionState> {
  try {
    const response = await fetch(getSessionCheckUrl(), {
      headers: {
        cookie: request.headers.get("cookie") ?? "",
        accept: "application/json",
      },
      cache: "no-store",
    });

    if (response.status === 503) {
      return { kind: "unavailable" };
    }
    if (!response.ok) {
      return { kind: "none" };
    }

    const data = (await response.json()) as SessionResponse;
    return data.user ? { kind: "user", user: data.user } : { kind: "none" };
  } catch {
    return { kind: "none" };
  }
}

export async function getSessionUser(
  request: NextRequest,
): Promise<SessionResponse["user"]> {
  const session = await getSessionState(request);
  return session.kind === "user" ? session.user : null;
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const onPublicAuthPath = isPublicAuthPath(pathname);
  const onLoginPage = isLoginPath(pathname);
  const session = await getSessionState(request);

  // The access check is unavailable, not the session gone: the page renders and says so
  // itself, and the next request checks again.
  if (session.kind === "unavailable") {
    return NextResponse.next();
  }

  const user = session.kind === "user" ? session.user : null;

  if (!user && !onPublicAuthPath) {
    const loginUrl = request.nextUrl.clone();
    loginUrl.pathname = LOGIN_PATH;
    loginUrl.search = "";
    if (pathname !== "/") {
      loginUrl.searchParams.set("redirect", pathname);
    }
    return NextResponse.redirect(loginUrl);
  }

  if (user && onLoginPage) {
    const homeUrl = request.nextUrl.clone();
    homeUrl.pathname = "/";
    homeUrl.search = "";
    return NextResponse.redirect(homeUrl);
  }

  return NextResponse.next();
}

// /hooks/github is GitHub's webhook delivery, authenticated in Go by its HMAC signature
// rather than a session; no other path under /hooks is exempt.
export const MIDDLEWARE_MATCHER_PATTERN =
  "/((?!api|hooks/github$|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)";

export const config = {
  matcher: [
    "/((?!api|hooks/github$|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
  ],
};
