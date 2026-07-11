package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

type AuthService struct {
	Sessions            *SessionStore
	VerifyToken         func(string) bool
	Users               *UserDirectory
	Now                 func() time.Time
	CookieOriginAllowed func(*http.Request) bool

	limiterMu sync.Mutex
	limiters  map[string]loginAttempt
}

type loginAttempt struct {
	StartedAt time.Time
	Count     int
}

type loginRequest struct {
	Token    string `json:"token"`
	Login    string `json:"login"`
	Password string `json:"password"`
}

type authResponse struct {
	Authenticated    bool       `json:"authenticated"`
	User             AuthUser   `json:"user,omitempty"`
	SessionExpiresAt *time.Time `json:"session_expires_at,omitempty"`
}

func NewAuthService(sessions *SessionStore, verifyToken func(string) bool) *AuthService {
	return &AuthService{
		Sessions:    sessions,
		VerifyToken: verifyToken,
		Now:         func() time.Time { return time.Now().UTC() },
		limiters:    make(map[string]loginAttempt),
	}
}

func (a *AuthService) LoginHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodPost) {
		return
	}
	if !a.allowLogin(ClientAddress(r)) {
		writeAuthJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests"})
		return
	}

	var request loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	if err := decoder.Decode(&request); err != nil {
		writeAuthUnauthorized(w)
		return
	}
	if a.Sessions == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_unavailable"})
		return
	}

	user, ok := a.authenticate(request)
	if !ok {
		writeAuthUnauthorized(w)
		return
	}

	a.resetLogin(ClientAddress(r))
	sessionID, session, err := a.Sessions.Create(user)
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
		return
	}
	http.SetCookie(w, SessionCookie(r, sessionID, session.ExpiresAt))
	writeAuthJSON(w, http.StatusOK, authResponse{
		Authenticated:    true,
		User:             session.User,
		SessionExpiresAt: &session.ExpiresAt,
	})
}

func (a *AuthService) MeHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodGet) {
		return
	}
	session, ok := a.sessionFromRequest(r)
	if !ok {
		writeAuthUnauthorized(w)
		return
	}
	writeAuthJSON(w, http.StatusOK, authResponse{
		Authenticated:    true,
		User:             session.User,
		SessionExpiresAt: &session.ExpiresAt,
	})
}

func (a *AuthService) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodPost) {
		return
	}
	_, sessionOK := a.sessionFromRequest(r)
	if sessionOK && !a.cookieMutationAllowed(r) {
		writeAuthUnauthorized(w)
		return
	}
	if !sessionOK && !a.BearerValid(bearerFromRequest(r)) {
		writeAuthUnauthorized(w)
		return
	}
	if a.Sessions != nil {
		if err := a.Sessions.Revoke(SessionIDFromRequest(r)); err != nil {
			writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
			return
		}
	}
	http.SetCookie(w, ExpiredSessionCookie(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a *AuthService) RefreshHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodPost) {
		return
	}
	if a.Sessions == nil {
		writeAuthUnauthorized(w)
		return
	}
	current, ok := a.sessionFromRequest(r)
	if !ok || !a.cookieMutationAllowed(r) {
		writeAuthUnauthorized(w)
		return
	}
	newID, session, ok, err := a.Sessions.Refresh(SessionIDFromRequest(r))
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
		return
	}
	if !ok {
		writeAuthUnauthorized(w)
		return
	}
	session.User = current.User
	http.SetCookie(w, SessionCookie(r, newID, session.ExpiresAt))
	writeAuthJSON(w, http.StatusOK, authResponse{
		Authenticated:    true,
		User:             session.User,
		SessionExpiresAt: &session.ExpiresAt,
	})
}

func (a *AuthService) SessionFromRequest(r *http.Request) (AuthSession, bool) {
	return a.sessionFromRequest(r)
}

func (a *AuthService) BearerValid(token string) bool {
	return a.VerifyToken != nil && a.VerifyToken(strings.TrimSpace(token))
}

func (a *AuthService) sessionFromRequest(r *http.Request) (AuthSession, bool) {
	if a == nil || a.Sessions == nil {
		return AuthSession{}, false
	}
	session, ok := a.Sessions.Lookup(SessionIDFromRequest(r))
	if !ok {
		return AuthSession{}, false
	}
	if a.Users != nil && session.User.ID != "" && session.User.Source == "auth.yaml" {
		current, currentOK := a.Users.UserByID(session.User.ID)
		if !currentOK {
			return AuthSession{}, false
		}
		session.User = current
	}
	return session, true
}

func (a *AuthService) authenticate(request loginRequest) (AuthUser, bool) {
	if strings.TrimSpace(request.Token) != "" && a.VerifyToken != nil && a.VerifyToken(request.Token) {
		return AdminAuthUser(), true
	}
	if a.Users == nil {
		return AuthUser{}, false
	}
	return a.Users.Authenticate(request.Login, request.Password)
}

func (a *AuthService) cookieMutationAllowed(r *http.Request) bool {
	if a.CookieOriginAllowed == nil {
		return true
	}
	return a.CookieOriginAllowed(r)
}

func (a *AuthService) allowLogin(client string) bool {
	now := a.currentTime()
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	attempt := a.limiters[client]
	if attempt.StartedAt.IsZero() || now.Sub(attempt.StartedAt) >= time.Minute {
		a.limiters[client] = loginAttempt{StartedAt: now, Count: 1}
		return true
	}
	if attempt.Count >= 8 {
		return false
	}
	attempt.Count++
	a.limiters[client] = attempt
	return true
}

func (a *AuthService) resetLogin(client string) {
	a.limiterMu.Lock()
	defer a.limiterMu.Unlock()
	delete(a.limiters, client)
}

func (a *AuthService) currentTime() time.Time {
	if a != nil && a.Now != nil {
		return a.Now().UTC()
	}
	return time.Now().UTC()
}

func bearerFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func requireAuthMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	return false
}

func writeAuthUnauthorized(w http.ResponseWriter) {
	writeAuthJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
}

func writeAuthJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
