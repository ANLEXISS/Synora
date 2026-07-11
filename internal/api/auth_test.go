package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newAuthTestService(t *testing.T) *AuthService {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth", "sessions.json")
	store, err := NewSessionStore(path, time.Hour, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	return NewAuthService(store, func(token string) bool { return token == "dev-token" })
}

func TestAuthLoginRejectsInvalidToken(t *testing.T) {
	service := newAuthTestService(t)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"bad-token"}`))
	rec := httptest.NewRecorder()

	service.LoginHandler(rec, req)

	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "{\"error\":\"unauthorized\"}\n" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Set-Cookie") != "" {
		t.Fatal("invalid login set a session cookie")
	}
}

func TestAuthSessionLifecycle(t *testing.T) {
	service := newAuthTestService(t)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"dev-token"}`))
	loginRec := httptest.NewRecorder()
	service.LoginHandler(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	loginCookie := mustCookie(t, loginRec)
	if loginCookie.Name != SessionCookieName || !loginCookie.HttpOnly || loginCookie.SameSite != http.SameSiteStrictMode || loginCookie.Secure {
		t.Fatalf("unexpected login cookie: %#v", loginCookie)
	}
	if !strings.Contains(loginRec.Body.String(), `"authenticated":true`) || !strings.Contains(loginRec.Body.String(), `"role":"admin"`) {
		t.Fatalf("unexpected login response: %s", loginRec.Body.String())
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meReq.AddCookie(loginCookie)
	meRec := httptest.NewRecorder()
	service.MeHandler(meRec, meReq)
	if meRec.Code != http.StatusOK || !strings.Contains(meRec.Body.String(), `"authenticated":true`) {
		t.Fatalf("me status=%d body=%s", meRec.Code, meRec.Body.String())
	}

	refreshReq := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshReq.AddCookie(loginCookie)
	refreshRec := httptest.NewRecorder()
	service.RefreshHandler(refreshRec, refreshReq)
	if refreshRec.Code != http.StatusOK {
		t.Fatalf("refresh status=%d body=%s", refreshRec.Code, refreshRec.Body.String())
	}
	refreshCookie := mustCookie(t, refreshRec)
	if refreshCookie.Value == loginCookie.Value {
		t.Fatal("refresh did not rotate the session identifier")
	}

	oldMeReq := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	oldMeReq.AddCookie(loginCookie)
	oldMeRec := httptest.NewRecorder()
	service.MeHandler(oldMeRec, oldMeReq)
	if oldMeRec.Code != http.StatusUnauthorized {
		t.Fatalf("old session status=%d body=%s", oldMeRec.Code, oldMeRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutReq.AddCookie(refreshCookie)
	logoutRec := httptest.NewRecorder()
	service.LogoutHandler(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusNoContent {
		t.Fatalf("logout status=%d body=%s", logoutRec.Code, logoutRec.Body.String())
	}
	logoutCookie := mustCookie(t, logoutRec)
	if logoutCookie.MaxAge != -1 {
		t.Fatalf("logout did not expire cookie: %#v", logoutCookie)
	}
}

func TestAuthLoginCookieSecureFollowsRequestTransport(t *testing.T) {
	service := newAuthTestService(t)
	req := httptest.NewRequest(http.MethodPost, "https://synora.local/api/auth/login", strings.NewReader(`{"token":"dev-token"}`))
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	service.LoginHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("https login status=%d body=%s", rec.Code, rec.Body.String())
	}
	cookie := mustCookie(t, rec)
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("https cookie is not secure: %#v", cookie)
	}
}

func TestSessionStorePersistsHashesAndInvalidatesOnTokenRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "sessions.json")
	store, err := NewSessionStore(path, time.Hour, "fingerprint-a")
	if err != nil {
		t.Fatal(err)
	}
	rawID, _, err := store.Create(AuthUser{Role: "admin", Source: "local"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), rawID) || !strings.Contains(string(data), SessionIDHash(rawID)) {
		t.Fatalf("session file contains unsafe identifier data: %s", data)
	}

	rotated, err := NewSessionStore(path, time.Hour, "fingerprint-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rotated.Lookup(rawID); ok {
		t.Fatal("token fingerprint rotation left an old session valid")
	}
}

func mustCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	response := http.Response{Header: rec.Header()}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d (%q)", len(cookies), rec.Header().Get("Set-Cookie"))
	}
	return cookies[0]
}
