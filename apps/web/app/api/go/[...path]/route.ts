import { NextRequest } from "next/server";

const DEFAULT_GO_API_URL = "http://127.0.0.1:8080";

const FORWARD_REQUEST_HEADERS = [
  "accept",
  "content-type",
  "cookie",
  "authorization",
];

function getGoApiUrl(): string {
  return process.env.GO_API_URL ?? DEFAULT_GO_API_URL;
}

function buildUpstreamUrl(pathSegments: string[], search: string): string {
  const base = getGoApiUrl().replace(/\/$/, "");
  const path = pathSegments.join("/");
  const url = `${base}/${path}`;
  return search ? `${url}?${search}` : url;
}

function buildUpstreamHeaders(request: NextRequest): Headers {
  const headers = new Headers();
  for (const name of FORWARD_REQUEST_HEADERS) {
    const value = request.headers.get(name);
    if (value) {
      headers.set(name, value);
    }
  }
  return headers;
}

function buildResponseHeaders(upstream: Response): Headers {
  const headers = new Headers();
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

async function proxyRequest(
  request: NextRequest,
  pathSegments: string[],
  method: string,
): Promise<Response> {
  const search = request.nextUrl.searchParams.toString();
  const upstreamUrl = buildUpstreamUrl(pathSegments, search);

  try {
    const hasBody = method !== "GET" && method !== "HEAD";
    const body = hasBody ? await request.arrayBuffer() : undefined;
    const hasPayload = body !== undefined && body.byteLength > 0;

    const upstream = await fetch(upstreamUrl, {
      method,
      headers: buildUpstreamHeaders(request),
      body: hasPayload ? body : undefined,
      duplex: hasPayload ? "half" : undefined,
    } as RequestInit);

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

export async function PATCH(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "PATCH");
}

export async function DELETE(request: NextRequest, context: RouteContext) {
  const { path } = await context.params;
  return proxyRequest(request, path, "DELETE");
}
