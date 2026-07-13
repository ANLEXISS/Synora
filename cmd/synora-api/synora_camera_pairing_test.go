package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	webapi "synora/internal/api"
	"synora/internal/security"
)

type fakeSynoraCameraPairingProvider struct {
	devices  []map[string]any
	topology map[string]any
	created  map[string]any
}

func (f *fakeSynoraCameraPairingProvider) Devices() ([]map[string]any, error) { return f.devices, nil }
func (f *fakeSynoraCameraPairingProvider) Topology() (map[string]any, error)  { return f.topology, nil }
func (f *fakeSynoraCameraPairingProvider) CreateDevice(body json.RawMessage) (map[string]any, error) {
	if err := json.Unmarshal(body, &f.created); err != nil {
		return nil, err
	}
	f.devices = append(f.devices, f.created)
	return f.created, nil
}

func validCameraQR() string {
	return `{"type":"synora.camera","version":1,"device_id":"cam_new","serial":"SYN-CAM-000010","model":"synora-cam-fe","setup_token":"one_time_secret"}`
}

func newPairingFake() (*fakeSynoraCameraPairingProvider, *synoraCameraPairingStore) {
	provider := &fakeSynoraCameraPairingProvider{
		topology: map[string]any{"nodes": []any{
			map[string]any{"id": "zoneA.L0.entree", "type": "room"},
		}},
	}
	return provider, newSynoraCameraPairingStore()
}

func callPairing(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestSynoraCameraPairingCapabilitiesAndValidation(t *testing.T) {
	provider, store := newPairingFake()
	capabilities := callPairing(handleSynoraCameraPairingCapabilities(), http.MethodGet, "/api/devices/pairing/capabilities", "")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"available":true`) {
		t.Fatalf("capabilities status=%d body=%s", capabilities.Code, capabilities.Body.String())
	}
	start := handleSynoraCameraPairingStart(provider, store)
	valid := callPairing(start, http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"qr_payload":`+validCameraQR()+`}`)
	if valid.Code != http.StatusOK || strings.Contains(valid.Body.String(), "one_time_secret") {
		t.Fatalf("valid start status=%d body=%s", valid.Code, valid.Body.String())
	}
	for name, body := range map[string]string{
		"invalid type":     `{"qr_payload":{"type":"other","version":1,"device_id":"cam_new","setup_token":"one_time_secret"}}`,
		"missing token":    `{"qr_payload":{"type":"synora.camera","version":1,"device_id":"cam_new"}}`,
		"unsafe device id": `{"raw_code":"{\"type\":\"synora.camera\",\"version\":1,\"device_id\":\"Cam.New\",\"setup_token\":\"one_time_secret\"}"}`,
	} {
		t.Run(name, func(t *testing.T) {
			response := callPairing(start, http.MethodPost, "/api/devices/pairing/synora-camera/start", body)
			if response.Code < http.StatusBadRequest || response.Code >= http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "one_time_secret") {
				t.Fatal("setup token was returned")
			}
		})
	}
	provider.devices = append(provider.devices, map[string]any{"id": "cam_existing"})
	duplicate := callPairing(start, http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+strconv.Quote(strings.Replace(validCameraQR(), "cam_new", "cam_existing", 1))+`}`)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}
}

func TestSynoraCameraPairingConfirmPersistsAndConsumesSession(t *testing.T) {
	provider, store := newPairingFake()
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+strconv.Quote(validCameraQR())+`}`)
	if start.Code != http.StatusOK {
		t.Fatal(start.Body.String())
	}
	var started synoraCameraPairingStartResponse
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	confirm := handleSynoraCameraPairingConfirm(provider, store)
	body := `{"session_id":"` + started.SessionID + `","name":"Caméra entrée","node_id":"zoneA.L0.entree","enabled":true}`
	invalidNode := callPairing(confirm, http.MethodPost, "/api/devices/pairing/synora-camera/confirm", strings.Replace(body, "zoneA.L0.entree", "zoneA.L0.missing", 1))
	if invalidNode.Code != http.StatusBadRequest {
		t.Fatalf("invalid node status=%d body=%s", invalidNode.Code, invalidNode.Body.String())
	}
	response := callPairing(confirm, http.MethodPost, "/api/devices/pairing/synora-camera/confirm", body)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"paired"`) {
		t.Fatalf("confirm status=%d body=%s", response.Code, response.Body.String())
	}
	if provider.created["vendor"] != "synora" || provider.created["pairing_method"] != "synora_qr" || provider.created["trusted"] != true {
		t.Fatalf("unexpected created device: %#v", provider.created)
	}
	if _, found := provider.created["setup_token"]; found {
		t.Fatal("setup token persisted")
	}
	consumed := callPairing(confirm, http.MethodPost, "/api/devices/pairing/synora-camera/confirm", body)
	if consumed.Code != http.StatusNotFound {
		t.Fatalf("consumed session status=%d body=%s", consumed.Code, consumed.Body.String())
	}
}

func TestSynoraCameraPairingExpiredSessionAndClaim(t *testing.T) {
	provider, store := newPairingFake()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+strconv.Quote(validCameraQR())+`}`)
	var started synoraCameraPairingStartResponse
	_ = json.Unmarshal(start.Body.Bytes(), &started)
	store.now = func() time.Time { return now.Add(synoraCameraPairingTTL + time.Second) }
	expired := callPairing(handleSynoraCameraPairingConfirm(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/confirm", `{"session_id":"`+started.SessionID+`","name":"Caméra","node_id":"zoneA.L0.entree"}`)
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired status=%d body=%s", expired.Code, expired.Body.String())
	}

	store.now = func() time.Time { return now }
	start = callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+strconv.Quote(validCameraQR())+`}`)
	_ = json.Unmarshal(start.Body.Bytes(), &started)
	claim := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"one_time_secret"}`)
	if claim.Code != http.StatusOK || !strings.Contains(claim.Body.String(), `"status":"accepted"`) {
		t.Fatalf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
}

func TestSynoraCameraPairingEndpointsAreAdminOnly(t *testing.T) {
	store, err := webapi.NewSessionStore(t.TempDir()+"/sessions.json", time.Hour, "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	auth := webapi.NewAuthService(store, func(string) bool { return false })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/devices/pairing/capabilities", handleSynoraCameraPairingCapabilities())
	handler := buildServerHandlerWithAuth(&security.Config{APITokenHash: security.HashSecret("admin-token")}, mux, nil, true, &webapi.Server{WebEnabled: false}, auth, false)
	residentID, _, err := store.Create(webapi.AuthUser{ID: "resident", Role: webapi.RoleResident, Permissions: webapi.PermissionsForRole(webapi.RoleResident)})
	if err != nil {
		t.Fatal(err)
	}
	resident := httptest.NewRequest(http.MethodGet, "/api/devices/pairing/capabilities", nil)
	resident.AddCookie(&http.Cookie{Name: webapi.SessionCookieName, Value: residentID})
	residentResponse := httptest.NewRecorder()
	handler.ServeHTTP(residentResponse, resident)
	if residentResponse.Code != http.StatusForbidden {
		t.Fatalf("resident status=%d body=%s", residentResponse.Code, residentResponse.Body.String())
	}
	admin := httptest.NewRequest(http.MethodGet, "/api/devices/pairing/capabilities", nil)
	admin.Header.Set("Authorization", "Bearer admin-token")
	adminResponse := httptest.NewRecorder()
	handler.ServeHTTP(adminResponse, admin)
	if adminResponse.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", adminResponse.Code, adminResponse.Body.String())
	}
}

func TestDeviceHTTPRedactsSecrets(t *testing.T) {
	provider := &fakeDeviceConfiguration{items: []map[string]any{{"id": "cam_01", "setup_token": "hidden", "config": map[string]any{"secret": "hidden"}}}}
	response := callPairing(handleDeviceCollection(provider), http.MethodGet, "/api/devices", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "hidden") {
		t.Fatalf("redaction status=%d body=%s", response.Code, response.Body.String())
	}
	response = callPairing(handleDeviceItem(provider), http.MethodDelete, "/api/devices/cam_01", "")
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "hidden") {
		t.Fatalf("delete redaction status=%d body=%s", response.Code, response.Body.String())
	}
}
