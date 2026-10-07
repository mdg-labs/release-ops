const BASE = "/api/go/api/v1";

export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export class NetworkError extends Error {
  constructor(message = "Network request failed") {
    super(message);
    this.name = "NetworkError";
  }
}

interface ErrorEnvelope {
  error?: {
    code?: string;
    message?: string;
  };
}

type RequestOptions = {
  body?: unknown;
  signal?: AbortSignal;
};

async function parseApiError(response: Response): Promise<ApiError> {
  try {
    const data = (await response.json()) as ErrorEnvelope;
    const code = data.error?.code ?? "UNKNOWN_ERROR";
    const message = data.error?.message ?? response.statusText;
    return new ApiError(code, message, response.status);
  } catch {
    return new ApiError("UNKNOWN_ERROR", response.statusText, response.status);
  }
}

function buildUrl(path: string): string {
  return `${BASE}${path.startsWith("/") ? path : `/${path}`}`;
}

async function request<T>(
  method: string,
  path: string,
  options?: RequestOptions,
): Promise<T> {
  const headers: HeadersInit = {
    Accept: "application/json",
  };

  let body: string | undefined;
  if (options?.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body);
  }

  try {
    const response = await fetch(buildUrl(path), {
      method,
      credentials: "include",
      headers,
      body,
      signal: options?.signal,
    });

    if (!response.ok) {
      throw await parseApiError(response);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    return (await response.json()) as T;
  } catch (error) {
    if (error instanceof ApiError) {
      throw error;
    }
    if (error instanceof DOMException && error.name === "AbortError") {
      throw error;
    }
    throw new NetworkError();
  }
}

export const apiClient = {
  get<T>(path: string, options?: Omit<RequestOptions, "body">): Promise<T> {
    return request<T>("GET", path, options);
  },

  post<T>(
    path: string,
    body?: unknown,
    options?: Omit<RequestOptions, "body">,
  ): Promise<T> {
    return request<T>("POST", path, { ...options, body });
  },

  patch<T>(
    path: string,
    body?: unknown,
    options?: Omit<RequestOptions, "body">,
  ): Promise<T> {
    return request<T>("PATCH", path, { ...options, body });
  },

  delete<T>(path: string, options?: Omit<RequestOptions, "body">): Promise<T> {
    return request<T>("DELETE", path, options);
  },
};
