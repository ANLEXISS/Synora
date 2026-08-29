import { buildApiUrl } from "./config";

export class SynoraApiError extends Error {
  status: number;
  body: string;
  code?: string;

  constructor(status: number, body: string, code?: string) {
    super(status === 401 ? "Session expirée" : status === 403 ? "Accès refusé" : `Synora API error ${status}: ${body}`);
    this.status = status;
    this.body = body;
    this.code = code;
  }
}

type ApiOptions = {
  signal?: AbortSignal;
  onMeta?: (meta: SynoraApiMeta) => void;
  ifNoneMatch?: string;
};

export type SynoraApiMeta = {
  revision?: unknown;
  etag?: string;
  next_cursor?: string;
  limit?: number;
};

type ApiEnvelope<T> = {
  data: T;
  error?: { code?: string; message?: string } | null;
  meta?: SynoraApiMeta;
};

function unwrapEnvelope<T>(value: unknown, onMeta?: (meta: SynoraApiMeta) => void): T {
  if (
    value &&
    typeof value === "object" &&
    !Array.isArray(value) &&
    "data" in value &&
    "meta" in value
  ) {
    const envelope = value as ApiEnvelope<T>;
    onMeta?.(envelope.meta ?? {});
    return envelope.data;
  }
  return value as T;
}

export async function synoraFetch<T>(
  path: string,
  options: RequestInit & ApiOptions = {}
): Promise<T> {
  const { onMeta, ifNoneMatch, ...requestOptions } = options;
  const headers = new Headers(requestOptions.headers);

  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json");
  }

  if (requestOptions.body && !(requestOptions.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (ifNoneMatch && !headers.has("If-None-Match")) {
    headers.set("If-None-Match", ifNoneMatch);
  }

  const response = await fetch(buildApiUrl(path), {
    ...requestOptions,
    headers,
    credentials: "include",
    cache: requestOptions.cache ?? "no-store",
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    if (response.status === 401 && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("synora:unauthorized"));
    }
    let message = body;
    let code: string | undefined;
    try {
      const parsed = JSON.parse(body) as { error?: { code?: string; message?: string } };
      message = parsed.error?.message ?? body;
      code = parsed.error?.code;
    } catch {
      // Preserve non-JSON upstream failures without exposing a stack trace.
    }
    throw new SynoraApiError(response.status, message, code);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const payload: unknown = await response.json();
  return unwrapEnvelope<T>(payload, onMeta);
}
