package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"synora/internal/security"
)

const (
	apiSessionTTL = 12 * time.Hour
	csrfHeader    = "X-Synora-CSRF"
)

type apiAuth struct {
	config       *security.Config
	secret       []byte
	sessions     *security.SessionStore
	sessionError error
	now          func() time.Time
}

func newAPIAuth(config *security.Config) *apiAuth {
	auth := &apiAuth{config: config, now: func() time.Time { return time.Now().UTC() }}
	if config == nil {
		return auth
	}
	config.Normalize()
	path := strings.TrimSpace(config.SessionSecretFile)
	if body, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(body))) >= 16 {
		auth.secret = []byte(strings.TrimSpace(string(body)))
	}
	// A bootstrap may predate the session-secret file. The persisted token hash
	// is still secret material and gives the local pilot a stable fallback while
	// keeping sessions invalidated when the API token is rotated.
	if len(auth.secret) < 16 {
		auth.secret = []byte(strings.TrimSpace(config.APITokenHash))
	}
	if store, err := security.OpenSessionStore(strings.TrimSpace(config.SessionStoreFile)); err != nil {
		auth.sessionError = err
	} else {
		auth.sessions = store
	}
	return auth
}

func (a *apiAuth) bearerRole(r *http.Request) (security.SessionClaims, bool) {
	if a == nil || a.config == nil || r == nil {
		return security.SessionClaims{}, false
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
		return security.SessionClaims{}, false
	}
	token := strings.TrimSpace(value[len("Bearer "):])
	if !a.config.VerifyAPIToken(token) {
		return security.SessionClaims{}, false
	}
	return security.SessionClaims{Subject: "api-token", Role: "admin", CSRF: "bearer", ExpiresAt: a.now().Add(apiSessionTTL)}, true
}

func (a *apiAuth) authenticate(r *http.Request) (security.SessionClaims, bool) {
	if claims, ok := a.bearerRole(r); ok {
		return claims, true
	}
	if a == nil || len(a.secret) < 16 || r == nil {
		return security.SessionClaims{}, false
	}
	cookie, err := r.Cookie(security.SessionCookieName)
	if err != nil {
		return security.SessionClaims{}, false
	}
	claims, err := security.VerifySession(a.secret, cookie.Value, a.now())
	if err != nil || a.sessions == nil {
		return security.SessionClaims{}, false
	}
	active, ok := a.sessions.Active(cookie.Value, a.now())
	if !ok || active.Subject != claims.Subject || active.Role != claims.Role {
		return security.SessionClaims{}, false
	}
	return claims, true
}

func (a *apiAuth) require(role string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := a.authenticate(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !security.RoleAllows(claims.Role, role) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions && claims.CSRF != "bearer" {
			if r.Header.Get(csrfHeader) == "" || r.Header.Get(csrfHeader) != claims.CSRF || !a.config.AllowsOrigin(r.Header.Get("Origin")) {
				http.Error(w, "csrf validation failed", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *apiAuth) createSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, ok := a.bearerRole(r)
	if !ok || len(a.secret) < 16 || a.sessions == nil || a.sessionError != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	csrf, err := security.RandomHex(32)
	if err != nil {
		http.Error(w, "session unavailable", http.StatusServiceUnavailable)
		return
	}
	claims.CSRF = csrf
	claims.ExpiresAt = a.now().Add(apiSessionTTL)
	token, err := security.SignSession(a.secret, claims)
	if err != nil {
		http.Error(w, "session unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := a.sessions.Register(token, claims); err != nil {
		http.Error(w, "session unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: security.SessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, Expires: claims.ExpiresAt})
	w.Header().Set(csrfHeader, csrf)
	writeJSON(w, http.StatusCreated, map[string]any{"subject": claims.Subject, "role": claims.Role, "expires_at": claims.ExpiresAt})
}

func (a *apiAuth) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(security.SessionCookieName); err == nil && a.sessions != nil {
		if err := a.sessions.Revoke(cookie.Value); err != nil {
			http.Error(w, "session unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: security.SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

func (a *apiAuth) me(w http.ResponseWriter, r *http.Request) {
	claims, _ := a.authenticate(r)
	writeJSON(w, http.StatusOK, map[string]any{"id": claims.Subject, "role": claims.Role, "permissions": []string{"intelligence:read"}, "expires_at": claims.ExpiresAt})
}
