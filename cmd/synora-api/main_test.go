package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/pkg/contract"
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

func TestVersionEndpointReturnsNonSecretManifest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.json")
	if err := os.WriteFile(path, []byte(`{"image_version":"test","synora_version":"1.0.0","git_commit":"abc123","build_time":"2026-10-06T00:00:00Z","target_board":"rk3588","os_base":"debian","kernel_expected":"6.1","rknn_runtime_expected":"2.2","config_schema_version":1,"bundle_id":"local-abc123"}`), 0600); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handleVersion(recorder, httptest.NewRequest(http.MethodGet, "/api/system/version", nil), path)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"git_commit":"abc123"`) {
		t.Fatalf("unexpected version response: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if strings.Contains(strings.ToLower(recorder.Body.String()), "secret") {
		t.Fatal("version response contains an unexpected secret field")
	}
}

func TestVersionEndpointFailsClosedWhenManifestMissing(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleVersion(recorder, httptest.NewRequest(http.MethodGet, "/api/system/version", nil), filepath.Join(t.TempDir(), "missing.json"))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing version manifest status=%d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

type fakeRuntimeHealthRequester struct {
	message *contract.Message
	err     error
}

type fakeSystemDataResetRequester struct {
	message *contract.Message
	err     error
	msgType string
	source  string
	payload []byte
	target  string
	targets []string
}

func (f *fakeSystemDataResetRequester) RequestWithTimeout(msgType, source string, payload []byte, target string, _ time.Duration) (*contract.Message, error) {
	f.msgType, f.source, f.payload, f.target = msgType, source, append([]byte(nil), payload...), target
	f.targets = append(f.targets, target)
	if f.message != nil && target == "discovery" {
		var result contract.SystemStateResetResult
		if json.Unmarshal(f.message.Payload, &result) == nil {
			result.Scope = "discovery"
			body, _ := json.Marshal(result)
			return &contract.Message{Payload: body}, f.err
		}
	}
	return f.message, f.err
}

func (f fakeRuntimeHealthRequester) RequestWithTimeout(string, string, []byte, string, time.Duration) (*contract.Message, error) {
	return f.message, f.err
}

func TestSystemHealthReturnsRuntimeEvidence(t *testing.T) {
	body, err := json.Marshal(contract.RuntimeHealth{Status: "ok", Services: map[string]contract.RuntimeServiceHealth{
		"synora-core": {Name: "synora-core", Status: "ok", Active: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handleSystemHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/system/health", nil), fakeRuntimeHealthRequester{message: &contract.Message{Payload: body}})
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected runtime health: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSystemHealthFailsClosedWhenRuntimeUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	handleSystemHealth(recorder, httptest.NewRequest(http.MethodGet, "/api/system/health", nil), fakeRuntimeHealthRequester{err: os.ErrNotExist})
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"status":"unknown"`) {
		t.Fatalf("unexpected unavailable health: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSystemDataDeleteRequiresReasonAndReturnsErasureEvidence(t *testing.T) {
	body, err := json.Marshal(contract.SystemStateResetResult{Status: "erased", Scope: "core", TargetState: "empty", CreatedBy: "admin-1", Reason: "operator requested erasure", ErasedAt: time.Unix(100, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	requester := &fakeSystemDataResetRequester{message: &contract.Message{Payload: body}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{"reason":"operator requested erasure"}`))
	handleSystemDataDelete(recorder, request, requester, "admin-1")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"status":"erased"`) {
		t.Fatalf("unexpected data delete response: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var sent contract.SystemStateResetRequest
	if err := json.Unmarshal(requester.payload, &sent); err != nil || sent.TargetState != "empty" || sent.CreatedBy != "admin-1" {
		t.Fatalf("unexpected reset request: %#v err=%v", sent, err)
	}
	if requester.msgType != contract.RPCSystemResetState || requester.source != "api" || strings.Join(requester.targets, ",") != "core,discovery" {
		t.Fatalf("unexpected reset routing: %#v", requester)
	}
}

func TestSystemDataDeleteRejectsMissingReasonAndFailsClosed(t *testing.T) {
	requester := &fakeSystemDataResetRequester{}
	recorder := httptest.NewRecorder()
	handleSystemDataDelete(recorder, httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{}`)), requester, "admin-1")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status=%d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handleSystemDataDelete(recorder, httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{"reason":"operator requested erasure"}`)), &fakeSystemDataResetRequester{message: &contract.Message{Payload: []byte(`{"status":"error","error":"reset failed"}`)}}, "admin-1")
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"status":"unknown"`) {
		t.Fatalf("reset failure was not fail-closed: code=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSystemDataDeleteIsAdminOnlyAndCSRFProtected(t *testing.T) {
	cfg := &security.Config{APIToken: "admin-token", AllowedOrigins: []string{"https://synora.example"}}
	auth := newAPIAuth(cfg)
	responseBody, _ := json.Marshal(contract.SystemStateResetResult{Status: "erased", Scope: "core", TargetState: "empty", Reason: "operator requested erasure", ErasedAt: time.Now().UTC()})
	requester := &fakeSystemDataResetRequester{message: &contract.Message{Payload: responseBody}}
	handler := auth.require("admin", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, _ := auth.authenticate(r)
		handleSystemDataDelete(w, r, requester, claims.Subject)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{"reason":"operator requested erasure"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized delete status=%d", unauthorized.Code)
	}

	guestToken, err := security.SignSession(auth.secret, security.SessionClaims{Subject: "guest-1", Role: "guest", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.sessions.Register(guestToken, security.SessionClaims{Subject: "guest-1", Role: "guest", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	forbidden := httptest.NewRecorder()
	guestRequest := httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{"reason":"operator requested erasure"}`))
	guestRequest.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: guestToken})
	handler.ServeHTTP(forbidden, guestRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("guest delete status=%d", forbidden.Code)
	}

	adminToken, err := security.SignSession(auth.secret, security.SessionClaims{Subject: "admin-1", Role: "admin", CSRF: "admin-csrf", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.sessions.Register(adminToken, security.SessionClaims{Subject: "admin-1", Role: "admin", CSRF: "admin-csrf", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	adminRequest := httptest.NewRequest(http.MethodDelete, "/api/system/data", strings.NewReader(`{"reason":"operator requested erasure"}`))
	adminRequest.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: adminToken})
	adminRequest.Header.Set("Origin", "https://synora.example")
	adminRequest.Header.Set(csrfHeader, "admin-csrf")
	adminResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminResponse, adminRequest)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin delete status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
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

func TestSessionIsRejectedAfterAccountRoleOrEnabledStateChanges(t *testing.T) {
	root := t.TempDir()
	authPath := filepath.Join(root, "auth.yaml")
	if err := os.WriteFile(authPath, []byte("users:\n  - id: user_guest\n    login: guest\n    role: guest\n    enabled: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &security.Config{APIToken: "admin-token"}
	auth := newAPIAuth(cfg)
	auth.accountPath = authPath
	claims := security.SessionClaims{Subject: "user_guest", Role: "guest", CSRF: "csrf", ExpiresAt: time.Now().Add(time.Hour)}
	token, err := security.SignSession(auth.secret, claims)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.sessions.Register(token, claims); err != nil {
		t.Fatal(err)
	}
	handler := auth.require("guest", http.HandlerFunc(auth.me))
	request := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: security.SessionCookieName, Value: token})
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusOK {
		t.Fatalf("active account was rejected: %d", accepted.Code)
	}
	if err := os.WriteFile(authPath, []byte("users:\n  - id: user_guest\n    login: guest\n    role: resident\n    enabled: false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusUnauthorized {
		t.Fatalf("stale session remained accepted after account change: %d", rejected.Code)
	}
}
