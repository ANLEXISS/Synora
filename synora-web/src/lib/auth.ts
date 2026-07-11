import { synoraFetch } from "./api";

export type SynoraUser = {
  id: string;
  login: string;
  role: "admin" | "resident" | "guest";
  resident_id?: string;
  source: string;
  permissions: string[];
};

export type SynoraAuthResponse = {
  authenticated: boolean;
  user?: SynoraUser;
  session_expires_at?: string;
};

export function loginWithToken(token: string) {
  return synoraFetch<SynoraAuthResponse>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ token }),
  });
}

export function loginWithCredentials(login: string, password: string) {
  return synoraFetch<SynoraAuthResponse>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ login, password }),
  });
}

export function getCurrentUser() {
  return synoraFetch<SynoraAuthResponse>("/api/auth/me");
}

export function logout() {
  return synoraFetch<void>("/api/auth/logout", { method: "POST" });
}

export function refreshSession() {
  return synoraFetch<SynoraAuthResponse>("/api/auth/refresh", {
    method: "POST",
  });
}
