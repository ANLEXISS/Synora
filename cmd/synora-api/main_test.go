package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/runtimeconfig"
	"synora/internal/security"
)

func TestConfiguredHTTPSUsesExisting8443Configuration(t *testing.T) {
	root := t.TempDir()
	securityPath := filepath.Join(root, "security.yaml")
	certPath := filepath.Join(root, "synora.crt")
	keyPath := filepath.Join(root, "synora.key")
	if err := os.WriteFile(securityPath, []byte("server:\n  http_addr: \":8080\"\n  https_enabled: true\n  https_addr: \":8443\"\n  tls_cert_file: \""+certPath+"\"\n  tls_key_file: \""+keyPath+"\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("test-certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("test-key"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"SYNORA_HTTP_ADDR", "SYNORA_HTTPS_ADDR", "SYNORA_HTTPS_ENABLED", "SYNORA_TLS_CERT_FILE", "SYNORA_TLS_KEY_FILE"} {
		t.Setenv(key, "")
	}
	runtime := runtimeconfig.Defaults()
	runtime.Paths.Security = securityPath
	runtime.Paths.TLSCert = certPath
	runtime.Paths.TLSKey = keyPath
	got, err := loadAPIServerConfig(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !got.HTTPSEnabled || got.HTTPAddr != ":8080" || got.HTTPSAddr != ":8443" || got.TLSCertFile != certPath || got.TLSKeyFile != keyPath {
		t.Fatalf("unexpected HTTPS configuration: %+v", got)
	}
}

func TestHealthIsLocalAndReadOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	handleHealth(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health response: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestIntelligenceRequiresAuthenticationAndRole(t *testing.T) {
	cfg := &security.Config{APIToken: "admin-token", AllowedOrigins: []string{"https://synora.example"}}
	auth := newAPIAuth(cfg)
	hub := newWebSocketHub(cfg, auth)
	handler := auth.require("guest", http.HandlerFunc(handleIntelligenceTopology(hub)))

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/intelligence/topology", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}

	forbidden := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/intelligence/topology", nil)
	request.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: "invalid"})
	handler.ServeHTTP(forbidden, request)
	if forbidden.Code != http.StatusUnauthorized {
		t.Fatalf("invalid session status = %d", forbidden.Code)
	}

	token, err := security.SignSession(auth.secret, security.SessionClaims{Subject: "guest-1", Role: "guest", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.sessions.Register(token, security.SessionClaims{Subject: "guest-1", Role: "guest", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	roleHandler := auth.require("admin", http.HandlerFunc(handleIntelligenceTopology(hub)))
	roleDenied := httptest.NewRecorder()
	roleRequest := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	roleRequest.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: token})
	roleHandler.ServeHTTP(roleDenied, roleRequest)
	if roleDenied.Code != http.StatusForbidden {
		t.Fatalf("insufficient role status = %d", roleDenied.Code)
	}
}

func TestSessionBootstrapReturnsCSRFProtectedCookie(t *testing.T) {
	cfg := &security.Config{APIToken: "admin-token", AllowedOrigins: []string{"https://synora.example"}}
	auth := newAPIAuth(cfg)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/session", nil)
	request.Header.Set("Authorization", "Bearer admin-token")
	auth.createSession(recorder, request)
	if recorder.Code != http.StatusCreated || recorder.Header().Get("Set-Cookie") == "" || recorder.Header().Get("X-Synora-CSRF") == "" {
		t.Fatalf("session bootstrap failed: code=%d headers=%v", recorder.Code, recorder.Header())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookie count = %d", len(cookies))
	}
	me := auth.require("guest", http.HandlerFunc(auth.me))
	meResponse := httptest.NewRecorder()
	meRequest := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	meRequest.AddCookie(cookies[0])
	me.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("session was not accepted after bootstrap: %d", meResponse.Code)
	}
	logout := auth.require("guest", http.HandlerFunc(auth.logout))
	logoutResponse := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	logoutRequest.Header.Set("Origin", "https://synora.example")
	logoutRequest.Header.Set(csrfHeader, recorder.Header().Get(csrfHeader))
	logout.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", logoutResponse.Code)
	}
	meAfterLogout := httptest.NewRecorder()
	me.ServeHTTP(meAfterLogout, meRequest)
	if meAfterLogout.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session remained accepted: %d", meAfterLogout.Code)
	}
}
