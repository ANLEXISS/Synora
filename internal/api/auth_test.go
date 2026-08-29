package api

import (
	"crypto/tls"
	"errors"
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

func TestAuthBootstrapIsOneShotAndPersistsDedicatedAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth", "auth.yaml")
	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(store, func(string) bool { return false })
	service.Users = directory
	request := httptest.NewRequest(http.MethodPost, "https://synora.local/api/auth/bootstrap", strings.NewReader(`{"login":"admin","password":"first-admin-password"}`))
	request.TLS = &tls.ConnectionState{}
	response := httptest.NewRecorder()
	service.BootstrapHandler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bootstrap status=%d body=%s", response.Code, response.Body.String())
	}
	cookie := mustCookie(t, response)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("insecure bootstrap cookie: %#v", cookie)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "first-admin-password") {
		t.Fatalf("dedicated auth file is invalid: %s (%v)", data, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("auth file mode=%o", info.Mode().Perm())
	}

	second := httptest.NewRecorder()
	service.BootstrapHandler(second, httptest.NewRequest(http.MethodPost, "/api/auth/bootstrap", strings.NewReader(`{"login":"other","password":"second-password"}`)))
	if second.Code != http.StatusConflict || !strings.Contains(second.Body.String(), `"error":"bootstrap_completed"`) {
		t.Fatalf("second bootstrap status=%d body=%s", second.Code, second.Body.String())
	}
	reloaded, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Authenticate("admin", "first-admin-password"); !ok || reloaded.AdminCount() != 1 {
		t.Fatal("persisted bootstrap admin cannot authenticate after reload")
	}
}

func TestAuthPasswordChangeRevokesSessionAndProtectsLastAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := directory.BootstrapAdmin("admin", "first-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(store, nil)
	service.Users = directory
	service.CookieOriginAllowed = func(*http.Request) bool { return true }

	login := httptest.NewRecorder()
	service.LoginHandler(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"admin","password":"first-admin-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", login.Code, login.Body.String())
	}
	cookie := mustCookie(t, login)
	change := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"current_password":"first-admin-password","new_password":"second-admin-password"}`))
	change.AddCookie(cookie)
	changed := httptest.NewRecorder()
	service.ChangePasswordHandler(changed, change)
	if changed.Code != http.StatusOK {
		t.Fatalf("password change status=%d body=%s", changed.Code, changed.Body.String())
	}
	me := httptest.NewRecorder()
	meRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meRequest.AddCookie(cookie)
	service.MeHandler(me, meRequest)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("old session survived password change: %d", me.Code)
	}
	if _, ok := directory.Authenticate("admin", "second-admin-password"); !ok {
		t.Fatal("new password does not authenticate")
	}

	if err := directory.DeleteUser(admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin deletion err=%v", err)
	}
	if _, err := directory.UpdateUser(admin.ID, nil, boolPtr(false), nil); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last admin disable err=%v", err)
	}
	if _, err := directory.CreateUser("second-admin", RoleAdmin, "second-admin-password", true); err != nil {
		t.Fatal(err)
	}
	if err := directory.DeleteUser(admin.ID); err != nil {
		t.Fatalf("delete with replacement admin: %v", err)
	}
}

func TestAuthUserManagementUsesRBACAndRejectsUnknownJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.yaml")
	directory, err := LoadUserDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := directory.BootstrapAdmin("admin", "first-admin-password"); err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(nil, func(token string) bool { return token == "admin-token" })
	service.Users = directory
	service.CookieOriginAllowed = func(*http.Request) bool { return true }

	unauthorized := httptest.NewRecorder()
	service.UsersHandler(unauthorized, httptest.NewRequest(http.MethodGet, "/api/auth/users", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized users status=%d", unauthorized.Code)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/auth/users", strings.NewReader(`{"login":"guest","role":"guest","password":"guest-password","unknown":true}`))
	create.Header.Set("Authorization", "Bearer admin-token")
	created := httptest.NewRecorder()
	service.UsersHandler(created, create)
	if created.Code != http.StatusBadRequest || !strings.Contains(created.Body.String(), `"error":"invalid_json"`) {
		t.Fatalf("unknown user field status=%d body=%s", created.Code, created.Body.String())
	}

	create = httptest.NewRequest(http.MethodPost, "/api/auth/users", strings.NewReader(`{"login":"guest","role":"guest","password":"guest-password"}`))
	create.Header.Set("Authorization", "Bearer admin-token")
	created = httptest.NewRecorder()
	service.UsersHandler(created, create)
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), "password") {
		t.Fatalf("user creation status=%d body=%s", created.Code, created.Body.String())
	}
	guest, found := directory.UserByID("user_guest")
	if !found || guest.Role != RoleGuest {
		t.Fatalf("created guest missing: %#v", guest)
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/auth/users/user_guest", strings.NewReader(`{"role":"resident"}`))
	patch.Header.Set("Authorization", "Bearer admin-token")
	patched := httptest.NewRecorder()
	service.UserHandler(patched, patch)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"role":"resident"`) {
		t.Fatalf("user patch status=%d body=%s", patched.Code, patched.Body.String())
	}
}

func boolPtr(value bool) *bool { return &value }

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

func TestRequestIsHTTPSDoesNotTrustUnconfiguredForwardedProto(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://synora.local/api/state", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if RequestIsHTTPS(request) {
		t.Fatal("untrusted forwarded proto changed transport security")
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
