export function getApiBaseUrl() {
  if (!import.meta.env.DEV) return "";
  return import.meta.env.VITE_API_BASE_URL?.replace(/\/$/, "") ?? "";
}

export function buildApiUrl(path: string) {
  const base = getApiBaseUrl();
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;

  return `${base}${normalizedPath}`;
}

export function buildWsUrl(path = "/api/ws") {
  const base = getApiBaseUrl();

  const url =
    base.length > 0
      ? new URL(path, base)
      : new URL(path, window.location.origin);

  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";

  return url.toString();
}
