import { buildApiUrl } from "./config";

export type RuntimeHealthService = { name?: string; status?: string; message?: string; error?: string };
export type RuntimeHealth = {
  status: "ok" | "degraded" | "unknown" | string;
  services?: Record<string, RuntimeHealthService>;
  components?: Record<string, RuntimeHealthService>;
  disk?: { status?: string; used_percent?: number };
};

export async function getRuntimeHealth(signal?: AbortSignal): Promise<RuntimeHealth> {
  const response = await fetch(buildApiUrl("/api/system/health"), { signal, cache: "no-store", credentials: "same-origin", headers: { Accept: "application/json" } });
  const body = await response.json().catch(() => ({ status: "unknown" }));
  if (!response.ok) return { status: body?.status ?? "unknown", ...body };
  return body as RuntimeHealth;
}

