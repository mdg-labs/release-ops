import { NextRequest } from "next/server";

const DEFAULT_GO_API_URL = "http://127.0.0.1:8080";

// x-forwarded-for is forwarded as the server preload (preload/preload.cjs, loaded by
// docker/entrypoint.sh) set it: the connection's address, or the right hop behind a trusted
// proxy. A value the client sent never reaches this handler, and Go keys its per-IP rate
// limits on it.
const FORWARD_REQUEST_HEADERS = [
  "accept",
  "content-type",
  "cookie",
  "authorization",
  "x-forwarded-for",
];

// GitHub's webhook delivery (specs.html § 2.10) is the one request that carries no session:
// Go authenticates it by the HMAC of the raw body. Its path gets its own header list, which
// leaves out the cookie and authorization headers, and a cap on the body the proxy buffers.
const WEBHOOK_PATH = "hooks/github";
const WEBHOOK_REQUEST_HEADERS = [
  "content-type",
  "x-forwarded-for",
  "x-hub-signature-256",
  "x-github-event",
  "x-github-delivery",
];
const WEBHOOK_MAX_BODY_BYTES = 1024 * 1024;

const REDIRECT_STATUSES = new Set([301, 302, 303, 307, 308]);
const GITHUB_ORIGIN = "https://github.com";

function getGoApiUrl(): string {
  return process.env.GO_API_URL ?? DEFAULT_GO_API_URL;
}

function buildUpstreamUrl(pathSegments: string[], search: string): string {
  const base = getGoApiUrl().replace(/\/$/, "");
  const path = pathSegments.join("/");
  const url = `${base}/${path}`;
  return search ? `${url}?${search}` : url;
}

function isWebhookRequest(pathSegments: string[], method: string): boolean {
  return method === "POST" && pathSegments.join("/") === WEBHOOK_PATH;
}

function buildUpstreamHeaders(
  request: NextRequest,
  names: string[] = FORWARD_REQUEST_HEADERS,
): Headers {
  const headers = new Headers();
  for (const name of names) {
    const value = request.headers.get(name);
    if (value) {
      headers.set(name, value);
    }
  }
  return headers;
}

function allowedRedirectOrigins(): Set<string> {
  const origins = new Set([GITHUB_ORIGIN]);
  const configured = process.env.GITHUB_BASE_URL;
  if (configured) {
    try {
      origins.add(new URL(configured).origin);
    } catch {
      // An unparsable GITHUB_BASE_URL adds no allowed origin.
    }
  }
  return origins;
}

// A same-origin path (one slash, not protocol-relative) or an absolute URL on an allowed
// GitHub origin; anything else is not forwarded, so the proxy is not an open redirect.
function isAllowedRedirectLocation(location: string): boolean {
  if (location.startsWith("/")) {
    return !location.startsWith("//") && !location.includes("\\");
  }
  try {
    return allowedRedirectOrigins().has(new URL(location).origin);
  } catch {
    return false;
  }
}

function buildResponseHeaders(upstream: Response): Headers {
  const headers = new Headers();
  if (REDIRECT_STATUSES.has(upstream.status)) {
    const location = upstream.headers.get("location");
    if (location) {
      headers.set("location", location);
    }
  }
  const contentType = upstream.headers.get("content-type");
  if (contentType) {
    headers.set("content-type", contentType);
  }

  const setCookies = upstream.headers.getSetCookie();
  if (setCookies.length > 0) {
    for (const cookie of setCookies) {
      headers.append("set-cookie", cookie);
    }
  } else {
    const single = upstream.headers.get("set-cookie");
    if (single) {
      headers.append("set-cookie", single);
    }
  }

  return headers;
}

function upstreamErrorResponse(): Response {
  return Response.json(
    {
      error: {
        code: "UPSTREAM_ERROR",
        message: "Failed to connect to Go API",
      },
    },
    { status: 502 },
  );
}

function payloadTooLargeResponse(): Response {
  return new Response(null, { status: 413 });
}

// Reads the body of a webhook delivery, giving up as soon as it passes the cap, so a request
// without a Content-Length cannot make the proxy buffer more than the cap. Returns null when
// the body is over the cap.
async function readCappedBody(
  request: NextRequest,
): Promise<Uint8Array<ArrayBuffer> | null> {
  const declared = request.headers.get("content-length");
  if (declared !== null && Number(declared) > WEBHOOK_MAX_BODY_BYTES) {
    return null;
  }
  if (!request.body) {
    return new Uint8Array(0);
  }
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) {
      break;
    }
    total += value.byteLength;
    if (total > WEBHOOK_MAX_BODY_BYTES) {
      await reader.cancel();
      return null;
    }
    chunks.push(value);
  }
  const body = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return body;
}

async function proxyRequest(
  request: NextRequest,
  pathSegments: string[],
  method: string,
): Promise<Response> {
  const search = request.nextUrl.searchParams.toString();
  const upstreamUrl = buildUpstreamUrl(pathSegments, search);

  try {
    const webhook = isWebhookRequest(pathSegments, method);
    const hasBody = method !== "GET" && method !== "HEAD";
    let body: ArrayBuffer | Uint8Array<ArrayBuffer> | undefined;
    if (webhook) {
      const capped = await readCappedBody(request);
      if (capped === null) {
        return payloadTooLargeResponse();
      }
      body = capped;
    } else if (hasBody) {
      body = await request.arrayBuffer();
    }
    const hasPayload = body !== undefined && body.byteLength > 0;

    const upstream = await fetch(upstreamUrl, {
      method,
      headers: buildUpstreamHeaders(
        request,
        webhook ? WEBHOOK_REQUEST_HEADERS : FORWARD_REQUEST_HEADERS,
      ),
      body: hasPayload ? body : undefined,
      duplex: hasPayload ? "half" : undefined,
      redirect: "manual",
    } as RequestInit);

    if (REDIRECT_STATUSES.has(upstream.status)) {
      const location = upstream.headers.get("location");
      if (location && !isAllowedRedirectLocation(location)) {
        await upstream.body?.cancel();
        return upstreamErrorResponse();
      }
    }

    return new Response(upstream.body, {
      status: upstream.status,
      headers: buildResponseHeaders(upstream),
    });
  } catch {
    return upstreamErrorResponse();
  }
}

type RouteContext = { params: Promise<{ path: string[] }> };

export async function GET(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "GET");
}

export async function POST(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "POST");
}

export async function PUT(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "PUT");
}

export async function PATCH(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "PATCH");
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "DELETE");
}
