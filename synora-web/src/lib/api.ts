import { buildApiUrl, getApiToken } from "./config";

export class SynoraApiError extends Error {
  status: number;
  body: string;

  constructor(status: number, body: string) {
    super(`Synora API error ${status}: ${body}`);
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
  const token = getApiToken();

  const headers = new Headers(options.headers);

  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json");
  }

  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const response = await fetch(buildApiUrl(path), {
    ...options,
    headers,
  });

  if (!response.ok) {
    const body = await response.text().catch(() => "");
    throw new SynoraApiError(response.status, body);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  return response.json() as Promise<T>;
}