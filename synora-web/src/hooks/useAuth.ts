import {
  createContext,
  createElement,
  useCallback,
  useContext,
  useEffect,
  useState,
  type PropsWithChildren,
} from "react";
import { SynoraApiError } from "../lib/api";
import {
  getCurrentUser,
  loginWithCredentials,
  loginWithToken,
  logout as logoutSession,
  refreshSession,
  type SynoraAuthResponse,
  type SynoraUser,
} from "../lib/auth";

export type UserRole = "admin" | "resident" | "guest";

export type AuthState = {
  loading: boolean;
  authenticated: boolean;
  user: SynoraUser | null;
  role: UserRole | null;
  permissions: string[];
  sessionExpiresAt: string | null;
  error: string | null;
  isAdmin: boolean;
  can: (permission: string) => boolean;
  login: (login: string, password: string, token?: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  me: () => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

function useAuthState(): AuthState {
  const [state, setState] = useState<{
    loading: boolean;
    authenticated: boolean;
    user: SynoraUser | null;
    sessionExpiresAt: string | null;
    error: string | null;
  }>({
    loading: true,
    authenticated: false,
    user: null,
    sessionExpiresAt: null,
    error: null,
  });

  const applyAuth = useCallback((response: SynoraAuthResponse) => {
    setState({
      loading: false,
      authenticated: response.authenticated,
      user: response.user ?? null,
      sessionExpiresAt: response.session_expires_at ?? null,
      error: null,
    });
  }, []);

  const markUnauthenticated = useCallback((error: string | null = null) => {
    setState({
      loading: false,
      authenticated: false,
      user: null,
      sessionExpiresAt: null,
      error,
    });
  }, []);

  const me = useCallback(async () => {
    try {
      applyAuth(await getCurrentUser());
    } catch (error: unknown) {
      markUnauthenticated(
        error instanceof SynoraApiError && error.status !== 401
          ? "API indisponible"
          : null
      );
    }
  }, [applyAuth, markUnauthenticated]);

  useEffect(() => {
    void me();
    const handleUnauthorized = () => markUnauthenticated();
    window.addEventListener("synora:unauthorized", handleUnauthorized);
    return () => window.removeEventListener("synora:unauthorized", handleUnauthorized);
  }, [me, markUnauthenticated]);

  const login = useCallback(
    async (loginName: string, password: string, token?: string) => {
      try {
        const response = token?.trim()
          ? await loginWithToken(token)
          : await loginWithCredentials(loginName, password);
        applyAuth(response);
      } catch (error) {
        markUnauthenticated(
          error instanceof SynoraApiError && error.status === 401
            ? "Identifiants invalides"
            : "Connexion impossible"
        );
        throw error;
      }
    },
    [applyAuth, markUnauthenticated]
  );

  const logout = useCallback(async () => {
    try {
      await logoutSession();
    } finally {
      markUnauthenticated();
    }
  }, [markUnauthenticated]);

  const refresh = useCallback(async () => {
    try {
      applyAuth(await refreshSession());
    } catch (error) {
      if (error instanceof SynoraApiError && error.status === 401) {
        markUnauthenticated();
      } else {
        throw error;
      }
    }
  }, [applyAuth, markUnauthenticated]);

  const permissions = state.user?.permissions ?? [];
  const role = state.user?.role ?? null;
  const can = useCallback(
    (permission: string) => state.user?.role === "admin" || permissions.includes(permission),
    [permissions, state.user?.role]
  );

  return {
    ...state,
    role,
    permissions,
    isAdmin: role === "admin",
    can,
    login,
    logout,
    refresh,
    me,
  };
}

export function AuthProvider({ children }: PropsWithChildren) {
  const auth = useAuthState();
  return createElement(AuthContext.Provider, { value: auth }, children);
}

export function useAuth(): AuthState {
  const auth = useContext(AuthContext);
  if (!auth) {
    throw new Error("useAuth must be used inside AuthProvider");
  }
  return auth;
}
