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

	handleSystemHealth(core).ServeHTTP(rec, req)

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

	for _, key := range []string{"services", "network", "mediamtx", "disk"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("system health missing key %q in %s", key, rec.Body.String())
		}
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
		name       string
		method     string
		path       string
		token      string
		wantStatus int
		wantBody   string
		wantCache  string
	}{
		{name: "index", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "spa", method: http.MethodGet, path: "/automations", wantStatus: http.StatusOK, wantBody: "<html>synora</html>"},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantBody: "console.log('ok')", wantCache: "public, max-age=31536000, immutable"},
		{name: "api-state", method: http.MethodGet, path: "/api/state", wantStatus: http.StatusUnauthorized},
		{name: "catalog-unauth", method: http.MethodGet, path: "/api/automations/catalog", wantStatus: http.StatusUnauthorized},
		{name: "catalog-auth", method: http.MethodGet, path: "/api/automations/catalog", token: "dev-token", wantStatus: http.StatusOK, wantBody: `"condition_kinds":[]`},
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
		})
	}
}

func contractTime() time.Time {
	return time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
}
