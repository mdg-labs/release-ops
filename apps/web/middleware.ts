import { NextRequest, NextResponse } from "next/server";

/** Browser-facing path via the Next.js proxy (matcher exclusions, docs). */
export const SESSION_API_PATH = "/api/go/api/v1/auth/session";

const DEFAULT_GO_API_URL = "http://127.0.0.1:8080";
const GO_SESSION_PATH = "/api/v1/auth/session";
const LOGIN_PATH = "/login";

export const PUBLIC_AUTH_PATHS = [
  "/login",
  "/accept-invitation",
  "/forgot-password",
  "/reset-password",
  "/confirm-email-change",
] as const;

function getSessionCheckUrl(): string {
  const base = (process.env.GO_API_URL ?? DEFAULT_GO_API_URL).replace(
    /\/$/,
    "",
  );
  return `${base}${GO_SESSION_PATH}`;
}

type SessionResponse = {
  user: { id: string; email: string } | null;
};

export function isPublicAuthPath(pathname: string): boolean {
  return PUBLIC_AUTH_PATHS.some(
    (path) => pathname === path || pathname.startsWith(`${path}/`),
  );
}

export function isLoginPath(pathname: string): boolean {
  return pathname === LOGIN_PATH || pathname.startsWith(`${LOGIN_PATH}/`);
}

export async function getSessionUser(
  request: NextRequest,
): Promise<SessionResponse["user"]> {
  try {
    const response = await fetch(getSessionCheckUrl(), {
      headers: {
        cookie: request.headers.get("cookie") ?? "",
        accept: "application/json",
      },
      cache: "no-store",
    });

    if (!response.ok) {
      return null;
    }

    const data = (await response.json()) as SessionResponse;
    return data.user ?? null;
  } catch {
    return null;
  }
}

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;
  const onPublicAuthPath = isPublicAuthPath(pathname);
  const onLoginPage = isLoginPath(pathname);
  const user = await getSessionUser(request);

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

export const MIDDLEWARE_MATCHER_PATTERN =
  "/((?!api|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)";

export const config = {
  matcher: [
    "/((?!api|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico)$).*)",
  ],
};
