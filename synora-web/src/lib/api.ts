import { buildApiUrl } from "./config";

export class SynoraApiError extends Error {
  readonly status: number;
  readonly body: string;

  constructor(status: number, body: string) {
    super(`Synora API error ${status}: ${body}`);
    this.status = status;
    this.body = body;
  }
}

export async function synoraFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");
  const response = await fetch(buildApiUrl(path), { ...options, headers, cache: "no-store", credentials: "same-origin" });
  if (!response.ok) throw new SynoraApiError(response.status, await response.text().catch(() => ""));
  return response.json() as Promise<T>;
}
