export function getApiBaseUrl() {
  return import.meta.env.VITE_API_BASE_URL?.replace(/\/$/, "") ?? "";
}

export function getApiToken() {
  const stored = localStorage.getItem("synora_api_token");

  if (stored) return stored;

  if (import.meta.env.DEV) {
    return import.meta.env.VITE_API_TOKEN ?? "";
  }

  return "";
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

  const token = getApiToken();

  if (token) {
    url.searchParams.set("token", token);
  }

  return url.toString();
}