import { buildApiUrl } from "./config";

export class SynoraApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(status === 403 ? "Accès refusé" : `Synora API error ${status}: ${body}`);
    this.status = status;
    this.body = body;
  }
}

type ApiOptions = {
  signal?: AbortSignal;
};

export async function synoraFetch<T>(
  path: string,
  options: RequestInit & ApiOptions = {}
): Promise<T> {
  const headers = new Headers(options.headers);

  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json");
  }

  if (options.body && !(options.body instanceof FormData) && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(buildApiUrl(path), {
    ...options,
    headers,
    credentials: "include",
    cache: options.cache ?? "no-store",
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    if (response.status === 401) {
      window.dispatchEvent(new CustomEvent("synora:unauthorized"));
    }
    throw new SynoraApiError(response.status, body);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}
