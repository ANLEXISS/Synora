export function getApiBaseUrl() {
  if (!import.meta.env.DEV) return "";
  return import.meta.env.VITE_API_BASE_URL?.replace(/\/$/, "") ?? "";
}

export function buildApiUrl(path: string) {
  const base = getApiBaseUrl();
  const rawPath = path.startsWith("/") ? path : `/${path}`;
  const alreadyV1 = rawPath === "/api/v1" || rawPath.startsWith("/api/v1/");
  const normalizedPath = rawPath === "/api" || (rawPath.startsWith("/api/") && !alreadyV1)
    ? `/api/v1${rawPath.slice("/api".length)}`
    : rawPath;

  return `${base}${normalizedPath}`;
}

export function buildWsUrl(path = "/api/ws") {
  const base = getApiBaseUrl();

  const rawPath = path.startsWith("/") ? path : `/${path}`;
  const alreadyV1 = rawPath === "/api/v1" || rawPath.startsWith("/api/v1/");
  const normalizedPath = rawPath === "/api" || (rawPath.startsWith("/api/") && !alreadyV1)
    ? `/api/v1${rawPath.slice("/api".length)}`
    : rawPath;

  const url =
    base.length > 0
      ? new URL(normalizedPath, base)
      : new URL(normalizedPath, window.location.origin);

  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";

  return url.toString();
}
