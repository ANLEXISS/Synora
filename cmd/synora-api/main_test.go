package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webapi "synora/internal/api"
	"synora/internal/security"
	"synora/pkg/contract"
)

type fakeCore struct {
	snapshot    *contract.PublicSnapshot
	state       *contract.PublicSnapshot
	health      *contract.RuntimeHealth
	validations []contract.ValidationRequest
	resolved    *contract.ValidationRequest
	resolveID   string
	resolveBody string
	err         error
}

func (f fakeCore) Snapshot() (*contract.PublicSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

func (f fakeCore) State() (*contract.PublicSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func (f fakeCore) SystemHealth() (*contract.RuntimeHealth, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.health, nil
}

func (f fakeCore) Validations() ([]contract.ValidationRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.validations, nil
}

func (f *fakeCore) ResolveValidation(id string, body json.RawMessage) (*contract.ValidationRequest, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.resolveID = id
	f.resolveBody = string(body)
	return f.resolved, nil
}

func TestHandleSnapshotReturnsPublicSnapshot(t *testing.T) {
	core := fakeCore{
		snapshot: &contract.PublicSnapshot{
			System:      map[string]any{"last_state": "idle"},
			Devices:     []map[string]any{{"id": "camera-1", "last_seen": nil}},
			Residents:   []map[string]any{},
			Nodes:       []map[string]any{},
			Events:      []map[string]any{},
			Automations: []map[string]any{},
			Cameras:     []map[string]any{},
			Tracks:      []map[string]any{},
			Clusters:    []map[string]any{},
			Clips:       []map[string]any{},
			Presence:    []map[string]any{},
			Identities:  []map[string]any{},
			Metrics:     map[string]any{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	rec := httptest.NewRecorder()

	handleSnapshot(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	for _, key := range []string{"state_store", "device", "event", "automation"} {
		if _, ok := body[key]; ok {
			t.Fatalf("snapshot exposes legacy/internal key %q in %s", key, rec.Body.String())
		}
	}
	for _, key := range []string{"devices", "events", "automations"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("snapshot missing key %q in %s", key, rec.Body.String())
		}
	}
}

func TestHandleStateReturnsPublicSnapshot(t *testing.T) {
	core := fakeCore{
		state: &contract.PublicSnapshot{
			System:      map[string]any{"last_state": "idle"},
			Devices:     []map[string]any{},
			Residents:   []map[string]any{},
			Nodes:       []map[string]any{},
			Events:      []map[string]any{},
			Automations: []map[string]any{},
			Cameras:     []map[string]any{},
			Tracks:      []map[string]any{},
			Clusters:    []map[string]any{},
			Clips:       []map[string]any{},
			Presence:    []map[string]any{},
			Identities:  []map[string]any{},
			Metrics:     map[string]any{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	rec := httptest.NewRecorder()

	handleState(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := body["devices"]; !ok {
		t.Fatalf("state missing devices key in %s", rec.Body.String())
	}
	if _, ok := body["state_store"]; ok {
		t.Fatalf("state exposes state_store in %s", rec.Body.String())
	}
}

func TestHandleSnapshotError(t *testing.T) {
	core := fakeCore{err: errors.New("core unavailable")}
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	rec := httptest.NewRecorder()

	handleSnapshot(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSystemHealthReturnsCleanRuntimeHealth(t *testing.T) {
	core := fakeCore{
		health: &contract.RuntimeHealth{
			Services: map[string]contract.RuntimeServiceHealth{
				"synora-core": {
					Name:   "synora-core",
					Status: "active",
					Active: true,
				},
			},
			Network: contract.RuntimeNetworkHealth{
				Status: "ok",
			},
			MediaMTX: contract.RuntimeMediaMTXHealth{
				Status: "active",
			},
			Disk: contract.RuntimeDiskHealth{
				Status: "ok",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/health", nil)
	rec := httptest.NewRecorder()

	handleSystemHealth(core, &webapi.Server{WebEnabled: false, WebRoot: t.TempDir()}, webapi.ServerHealth{
		HTTPAddr:       ":8080",
		HTTPSEnabled:   true,
		HTTPSAddr:      ":8443",
		TLSCertPresent: true,
		TLSKeyPresent:  true,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	if _, ok := body["payload"]; ok {
		t.Fatalf("system health is double wrapped: %s", rec.Body.String())
	}

	for _, key := range []string{"services", "network", "mediamtx", "disk", "web"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("system health missing key %q in %s", key, rec.Body.String())
		}
	}
	server, ok := body["server"].(map[string]any)
	if !ok || server["https_enabled"] != true || server["tls_cert_present"] != true || server["tls_key_present"] != true {
		t.Fatalf("system health missing TLS server state: %#v", body["server"])
	}
}

func TestHandleValidationsReturnsList(t *testing.T) {
	core := &fakeCore{
		validations: []contract.ValidationRequest{{
			ID:        "validation-1",
			EventID:   "evt-1",
			Status:    contract.ValidationStatusPending,
			CreatedAt: contractTime(),
		}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/validations", nil)
	rec := httptest.NewRecorder()

	handleValidations(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body []contract.ValidationRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(body) != 1 || body[0].ID != "validation-1" {
		t.Fatalf("unexpected validations body: %#v", body)
	}
}

func TestHandleValidationResolve(t *testing.T) {
	core := &fakeCore{
		resolved: &contract.ValidationRequest{
			ID:     "validation-1",
			Status: contract.ValidationStatusRejected,
		},
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/validations/validation-1/resolve",
		strings.NewReader(`{"action":"reject"}`),
	)
	rec := httptest.NewRecorder()

	handleValidation(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if core.resolveID != "validation-1" || core.resolveBody != `{"action":"reject"}` {
		t.Fatalf("resolve call mismatch id=%q body=%q", core.resolveID, core.resolveBody)
	}
	var body contract.ValidationRequest
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Status != contract.ValidationStatusRejected {
		t.Fatalf("unexpected resolve body: %#v", body)
	}
}

func TestAPIAuthMiddlewareRequiresBearerToken(t *testing.T) {
	cfg := &security.Config{
		APITokenHash: security.HashSecret("dev-token"),
	}
	handler := apiAuthMiddleware(
		cfg,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ws status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/ws", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ws status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIAuthMiddlewareAllowsPublicSystemHealthWhenConfigured(t *testing.T) {
	cfg := &security.Config{
		APITokenHash:       security.HashSecret("dev-token"),
		PublicSystemHealth: true,
	}
	handler := apiAuthMiddleware(
		cfg,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/system/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerHandlerServesWebStaticWithoutAuthAndProtectsAPI(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>synora</html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("console.log('ok')"), 0o640); err != nil {
		t.Fatal(err)
	}

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	apiMux.HandleFunc("/api/automations/catalog", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"condition_kinds": []any{}})
	})

	handler := buildServerHandler(
		&security.Config{APITokenHash: security.HashSecret("dev-token")},
		apiMux,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		true,
		&webapi.Server{WebEnabled: true, WebRoot: root},
	)

	for _, tc := range []struct {
		name        string
		method      string
		path        string
		token       string
		wantStatus  int
		wantBody    string
		wantCache   string
		wantPragma  string
		wantExpires string
	}{
		{name: "index", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "spa", method: http.MethodGet, path: "/automations", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "console.log('ok')", wantCache: "public, max-age=31536000, immutable"},
		{name: "api-state", method: http.MethodGet, path: "/api/state", wantStatus: http.StatusUnauthorized, wantCache: "no-store", wantPragma: "no-cache", wantExpires: "0"},
		{name: "catalog-unauth", method: http.MethodGet, path: "/api/automations/catalog", wantStatus: http.StatusUnauthorized, wantCache: "no-store", wantPragma: "no-cache", wantExpires: "0"},
		{name: "catalog-auth", method: http.MethodGet, path: "/api/automations/catalog", token: "dev-token", wantStatus: http.StatusOK, wantBody: `"condition_kinds":[]`, wantCache: "no-store", wantPragma: "no-cache", wantExpires: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body=%s", rec.Body.String())
			}
			if tc.wantCache != "" && rec.Header().Get("Cache-Control") != tc.wantCache {
				t.Fatalf("cache-control=%q", rec.Header().Get("Cache-Control"))
			}
			if tc.wantPragma != "" && rec.Header().Get("Pragma") != tc.wantPragma {
				t.Fatalf("pragma=%q", rec.Header().Get("Pragma"))
			}
			if tc.wantExpires != "" && rec.Header().Get("Expires") != tc.wantExpires {
				t.Fatalf("expires=%q", rec.Header().Get("Expires"))
			}
		})
	}
}

func TestServerHandlerAcceptsCookieSessionForAPI(t *testing.T) {
	store, err := webapi.NewSessionStore(
		filepath.Join(t.TempDir(), "auth", "sessions.json"),
		time.Hour,
		security.HashSecret("fingerprint"),
	)
	if err != nil {
		t.Fatal(err)
	}
	auth := webapi.NewAuthService(store, func(token string) bool { return token == "dev-token" })
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/state", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	apiMux.HandleFunc("/api/mutate", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := buildServerHandlerWithAuth(
		&security.Config{APITokenHash: security.HashSecret("dev-token")},
		apiMux,
		nil,
		true,
		&webapi.Server{WebEnabled: false},
		auth,
		false,
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"token":"dev-token"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected session cookie, got %q", loginRec.Header().Get("Set-Cookie"))
	}

	stateReq := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	stateReq.AddCookie(cookies[0])
	stateRec := httptest.NewRecorder()
	handler.ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusNoContent {
		t.Fatalf("cookie state status=%d body=%s", stateRec.Code, stateRec.Body.String())
	}

	mutateReq := httptest.NewRequest(http.MethodPost, "/api/mutate", nil)
	mutateReq.AddCookie(cookies[0])
	mutateRec := httptest.NewRecorder()
	handler.ServeHTTP(mutateRec, mutateReq)
	if mutateRec.Code != http.StatusUnauthorized {
		t.Fatalf("cross-site mutation without origin status=%d body=%s", mutateRec.Code, mutateRec.Body.String())
	}

	mutateReq = httptest.NewRequest(http.MethodPost, "/api/mutate", nil)
	mutateReq.AddCookie(cookies[0])
	mutateReq.Header.Set("Origin", "http://example.com")
	mutateRec = httptest.NewRecorder()
	handler.ServeHTTP(mutateRec, mutateReq)
	if mutateRec.Code != http.StatusNoContent {
		t.Fatalf("same-origin mutation status=%d body=%s", mutateRec.Code, mutateRec.Body.String())
	}
}

func TestServerHandlerEnforcesResidentRBACAndKeepsBearerAdmin(t *testing.T) {
	hash, err := webapi.HashPassword("resident-password")
	if err != nil {
		t.Fatal(err)
	}
	guestHash, err := webapi.HashPassword("guest-password")
	if err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(t.TempDir(), "auth.yaml")
	if err := os.WriteFile(authPath, []byte("users:\n  - id: user-carole\n    login: carole\n    resident_id: carole\n    role: resident\n    enabled: true\n    password_hash: "+hash+"\n  - id: user-guest\n    login: guest\n    role: guest\n    enabled: true\n    password_hash: "+guestHash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	users, err := webapi.LoadUserDirectory(authPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := webapi.NewSessionStore(filepath.Join(t.TempDir(), "sessions.json"), time.Hour, security.HashSecret("fingerprint"))
	if err != nil {
		t.Fatal(err)
	}
	auth := webapi.NewAuthService(store, func(token string) bool { return token == "dev-token" })
	auth.Users = users

	apiMux := http.NewServeMux()
	for _, path := range []string{"/api/state", "/api/devices", "/api/residents/alexis/face", "/api/simulation/run"} {
		path := path
		apiMux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	handler := buildServerHandlerWithAuth(
		&security.Config{APITokenHash: security.HashSecret("dev-token")},
		apiMux,
		nil,
		true,
		&webapi.Server{WebEnabled: false},
		auth,
		false,
	)

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"carole","password":"resident-password"}`))
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}
	cookie := loginRec.Result().Cookies()[0]

	for _, path := range []string{"/api/state", "/api/devices"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("resident read %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}

	for _, path := range []string{"/api/devices", "/api/residents", "/api/residents/alexis/face", "/api/simulation/run"} {
		method := http.MethodPost
		if path == "/api/residents/alexis/face" {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, path, nil)
		req.Host = "example.com"
		req.Header.Set("Origin", "http://example.com")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || rec.Body.String() != "{\"error\":\"forbidden\"}\n" {
			t.Fatalf("resident write %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/devices/cam_01", nil)
	deleteReq.Host = "example.com"
	deleteReq.Header.Set("Origin", "http://example.com")
	deleteReq.AddCookie(cookie)
	deleteRec := httptest.NewRecorder()
	handler.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusForbidden || deleteRec.Body.String() != "{\"error\":\"forbidden\"}\n" {
		t.Fatalf("resident device delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/devices/cam_01", strings.NewReader(`{"name":"cam"}`))
	patchReq.Host = "example.com"
	patchReq.Header.Set("Origin", "http://example.com")
	patchReq.AddCookie(cookie)
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusForbidden || patchRec.Body.String() != "{\"error\":\"forbidden\"}\n" {
		t.Fatalf("resident device patch status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}

	guestLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"guest","password":"guest-password"}`))
	guestLoginRec := httptest.NewRecorder()
	handler.ServeHTTP(guestLoginRec, guestLogin)
	if guestLoginRec.Code != http.StatusOK {
		t.Fatalf("guest login status=%d body=%s", guestLoginRec.Code, guestLoginRec.Body.String())
	}
	guestCookie := guestLoginRec.Result().Cookies()[0]
	guestGet := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	guestGet.AddCookie(guestCookie)
	guestGetRec := httptest.NewRecorder()
	handler.ServeHTTP(guestGetRec, guestGet)
	if guestGetRec.Code != http.StatusNoContent {
		t.Fatalf("guest device read status=%d body=%s", guestGetRec.Code, guestGetRec.Body.String())
	}
	guestPatch := httptest.NewRequest(http.MethodPatch, "/api/devices/cam_01", strings.NewReader(`{"enabled":false}`))
	guestPatch.Host = "example.com"
	guestPatch.Header.Set("Origin", "http://example.com")
	guestPatch.AddCookie(guestCookie)
	guestPatchRec := httptest.NewRecorder()
	handler.ServeHTTP(guestPatchRec, guestPatch)
	if guestPatchRec.Code != http.StatusForbidden || guestPatchRec.Body.String() != "{\"error\":\"forbidden\"}\n" {
		t.Fatalf("guest device patch status=%d body=%s", guestPatchRec.Code, guestPatchRec.Body.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/api/devices", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("bearer admin write status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerHandlerNoStoreCoversEveryAPIRoute(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<html>synora</html>"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "test.js"), []byte("ok"), 0o640); err != nil {
		t.Fatal(err)
	}

	apiMux := http.NewServeMux()
	for _, path := range []string{"/api/state", "/api/system/health", "/api/topology", "/api/automations/catalog"} {
		path := path
		apiMux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	store, err := webapi.NewSessionStore(
		filepath.Join(t.TempDir(), "auth", "sessions.json"),
		time.Hour,
		security.HashSecret("fingerprint"),
	)
	if err != nil {
		t.Fatal(err)
	}
	auth := webapi.NewAuthService(store, func(token string) bool { return token == "dev-token" })
	handler := buildServerHandlerWithAuth(
		&security.Config{APITokenHash: security.HashSecret("dev-token")},
		apiMux,
		nil,
		true,
		&webapi.Server{WebEnabled: true, WebRoot: root},
		auth,
		false,
	)

	for _, tc := range []struct {
		name       string
		path       string
		token      string
		wantStatus int
		wantCache  string
	}{
		{name: "state", path: "/api/state", token: "dev-token", wantStatus: http.StatusNoContent, wantCache: "no-store"},
		{name: "health", path: "/api/system/health", token: "dev-token", wantStatus: http.StatusNoContent, wantCache: "no-store"},
		{name: "topology", path: "/api/topology", token: "dev-token", wantStatus: http.StatusNoContent, wantCache: "no-store"},
		{name: "catalog", path: "/api/automations/catalog", token: "dev-token", wantStatus: http.StatusNoContent, wantCache: "no-store"},
		{name: "auth-me", path: "/api/auth/me", wantStatus: http.StatusUnauthorized, wantCache: "no-store"},
		{name: "index", path: "/", wantStatus: http.StatusOK, wantCache: "no-cache"},
		{name: "asset", path: "/assets/test.js", wantStatus: http.StatusOK, wantCache: "public, max-age=31536000, immutable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
				t.Fatalf("cache-control=%q, want %q", got, tc.wantCache)
			}
			if strings.HasPrefix(tc.path, "/api/") {
				if got := rec.Header().Get("Pragma"); got != "no-cache" {
					t.Fatalf("pragma=%q", got)
				}
				if got := rec.Header().Get("Expires"); got != "0" {
					t.Fatalf("expires=%q", got)
				}
			}
		})
	}
}

func contractTime() time.Time {
	return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
}
