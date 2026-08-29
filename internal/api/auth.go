package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
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

type passwordChangeRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type userCreateRequest struct {
	Login    string `json:"login"`
	Role     string `json:"role"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type userUpdateRequest struct {
	Role     *string `json:"role,omitempty"`
	Password *string `json:"password,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty"`
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
	if !decodeAuthJSON(w, r, &request, 4096) {
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

// BootstrapHandler provisions the first local administrator exactly once.
// It intentionally remains outside the authenticated API middleware because
// no account exists yet; subsequent calls fail closed after the first admin.
func (a *AuthService) BootstrapHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodPost) {
		return
	}
	if !a.allowLogin(ClientAddress(r)) {
		writeAuthJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too_many_requests"})
		return
	}
	var request loginRequest
	if !decodeAuthJSON(w, r, &request, 4096) {
		return
	}
	if a.Users == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_unavailable"})
		return
	}
	user, err := a.Users.BootstrapAdmin(request.Login, request.Password)
	if err != nil {
		writeAuthDomainError(w, err)
		return
	}
	a.resetLogin(ClientAddress(r))
	a.writeNewSession(w, r, user)
}

func (a *AuthService) ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	if !requireAuthMethod(w, r, http.MethodPost) {
		return
	}
	session, ok := a.sessionFromRequest(r)
	if !ok || !a.cookieMutationAllowed(r) {
		writeAuthUnauthorized(w)
		return
	}
	var request passwordChangeRequest
	if !decodeAuthJSON(w, r, &request, 4096) {
		return
	}
	if a.Users == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_unavailable"})
		return
	}
	if err := a.Users.ChangePassword(session.User.ID, request.CurrentPassword, request.NewPassword); err != nil {
		writeAuthDomainError(w, err)
		return
	}
	if a.Sessions != nil {
		if err := a.Sessions.RevokeUser(session.User.ID); err != nil {
			writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
			return
		}
	}
	http.SetCookie(w, ExpiredSessionCookie(r))
	writeAuthJSON(w, http.StatusOK, map[string]any{"changed": true})
}

func (a *AuthService) UsersHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.managementPrincipal(r)
	if !ok || principal.Role != RoleAdmin {
		writeAuthUnauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeAuthJSON(w, http.StatusOK, a.publicUsers())
	case http.MethodPost:
		if !a.cookieMutationAllowedForPrincipal(r, principal) {
			writeAuthUnauthorized(w)
			return
		}
		var request userCreateRequest
		if !decodeAuthJSON(w, r, &request, 4096) {
			return
		}
		enabled := true
		if request.Enabled != nil {
			enabled = *request.Enabled
		}
		user, err := a.Users.CreateUser(request.Login, request.Role, request.Password, enabled)
		if err != nil {
			writeAuthDomainError(w, err)
			return
		}
		writeAuthJSON(w, http.StatusCreated, user)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (a *AuthService) UserHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.managementPrincipal(r)
	if !ok || principal.Role != RoleAdmin {
		writeAuthUnauthorized(w)
		return
	}
	id, ok := authUserPathID(r.URL.Path)
	if !ok {
		writeAuthJSON(w, http.StatusNotFound, map[string]string{"error": "account_not_found"})
		return
	}
	if r.Method == http.MethodGet {
		user, found := a.Users.UserByID(id)
		if !found {
			writeAuthJSON(w, http.StatusNotFound, map[string]string{"error": "account_not_found"})
			return
		}
		writeAuthJSON(w, http.StatusOK, user)
		return
	}
	if !a.cookieMutationAllowedForPrincipal(r, principal) {
		writeAuthUnauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var request userUpdateRequest
		if !decodeAuthJSON(w, r, &request, 4096) {
			return
		}
		user, err := a.Users.UpdateUser(id, request.Role, request.Enabled, request.Password)
		if err != nil {
			writeAuthDomainError(w, err)
			return
		}
		if a.Sessions != nil && (request.Role != nil || request.Enabled != nil || request.Password != nil) {
			if err := a.Sessions.RevokeUser(id); err != nil {
				writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
				return
			}
		}
		writeAuthJSON(w, http.StatusOK, user)
	case http.MethodDelete:
		if err := a.Users.DeleteUser(id); err != nil {
			writeAuthDomainError(w, err)
			return
		}
		if a.Sessions != nil {
			if err := a.Sessions.RevokeUser(id); err != nil {
				writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		writeAuthJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (a *AuthService) managementPrincipal(r *http.Request) (AuthUser, bool) {
	if a == nil {
		return AuthUser{}, false
	}
	if a.BearerValid(bearerFromRequest(r)) {
		return AdminAuthUser(), true
	}
	session, ok := a.sessionFromRequest(r)
	if !ok {
		return AuthUser{}, false
	}
	return session.User, true
}

func (a *AuthService) cookieMutationAllowedForPrincipal(r *http.Request, principal AuthUser) bool {
	if principal.Source == "local" && bearerFromRequest(r) != "" {
		return true
	}
	return a.cookieMutationAllowed(r)
}

func (a *AuthService) publicUsers() []AuthUser {
	if a == nil || a.Users == nil {
		return []AuthUser{}
	}
	return a.Users.PublicUsers()
}

func (a *AuthService) writeNewSession(w http.ResponseWriter, r *http.Request, user AuthUser) {
	if a.Sessions == nil {
		writeAuthJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "auth_unavailable"})
		return
	}
	sessionID, session, err := a.Sessions.Create(user)
	if err != nil {
		writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "auth_unavailable"})
		return
	}
	http.SetCookie(w, SessionCookie(r, sessionID, session.ExpiresAt))
	writeAuthJSON(w, http.StatusOK, authResponse{Authenticated: true, User: user, SessionExpiresAt: &session.ExpiresAt})
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if r == nil || r.Body == nil {
		writeAuthJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeAuthJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeAuthJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return false
	}
	return true
}

func authUserPathID(path string) (string, bool) {
	raw := strings.TrimPrefix(path, "/api/auth/users/")
	if raw == "" || strings.Contains(raw, "/") {
		return "", false
	}
	id, err := url.PathUnescape(raw)
	return strings.TrimSpace(id), err == nil && strings.TrimSpace(id) != ""
}

func writeAuthDomainError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, ErrBootstrapCompleted), errors.Is(err, ErrAccountExists):
		status = http.StatusConflict
	case errors.Is(err, ErrAccountNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ErrLastAdmin):
		status = http.StatusConflict
	case errors.Is(err, ErrPasswordMismatch):
		status = http.StatusUnauthorized
	case errors.Is(err, ErrInvalidRole), errors.Is(err, ErrInvalidAccount), errors.Is(err, ErrInvalidPassword):
		status = http.StatusBadRequest
	default:
		status = http.StatusInternalServerError
	}
	code := err.Error()
	if status >= http.StatusInternalServerError {
		code = "auth_unavailable"
	}
	writeAuthJSON(w, status, map[string]string{"error": code})
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
