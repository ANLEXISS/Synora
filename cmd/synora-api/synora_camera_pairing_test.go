package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func (f *fakeSynoraCameraPairingProvider) Device(id string) (map[string]any, error) {
	for _, item := range f.devices {
		if item["id"] == id {
			return item, nil
		}
	}
	return nil, nil
}

func (f *fakeSynoraCameraPairingProvider) UpdateDevice(id string, body json.RawMessage) (map[string]any, error) {
	var patch map[string]any
	if err := json.Unmarshal(body, &patch); err != nil {
		return nil, err
	}
	for _, item := range f.devices {
		if item["id"] != id {
			continue
		}
		for key, value := range patch {
			if key == "network" {
				if nested, ok := value.(map[string]any); ok {
					current, _ := item[key].(map[string]any)
					if current == nil {
						current = map[string]any{}
					}
					for nestedKey, nestedValue := range nested {
						current[nestedKey] = nestedValue
					}
					item[key] = current
					continue
				}
			}
			item[key] = value
		}
		return item, nil
	}
	return nil, nil
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
	claim := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"one_time_secret"}`)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
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

func TestSynoraCameraPairingSessionSurvivesStoreRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairing.json")
	provider, _ := newPairingFake()
	store := newSynoraCameraPairingStore(path)
	started := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+jsonString(validCameraQR())+`}`)
	if started.Code != http.StatusOK {
		t.Fatal(started.Body.String())
	}
	restarted := newSynoraCameraPairingStore(path)
	claim := callPairing(handleSynoraCameraPairingClaim(restarted), http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"one_time_secret"}`)
	if claim.Code != http.StatusOK {
		t.Fatalf("claim after restart status=%d body=%s", claim.Code, claim.Body.String())
	}
}

func TestSynoraCameraPairingClaimIsRateLimitedAndRecovers(t *testing.T) {
	provider, store := newPairingFake()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+jsonString(validCameraQR())+`}`)
	if start.Code != http.StatusOK {
		t.Fatal(start.Body.String())
	}
	claim := handleSynoraCameraPairingClaim(store)
	for index := 0; index < maxSynoraClaimFailures; index++ {
		response := callPairing(claim, http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"wrong-token"}`)
		if response.Code == http.StatusTooManyRequests {
			t.Fatalf("claim was limited too early at attempt %d", index+1)
		}
	}
	limited := callPairing(claim, http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"one_time_secret"}`)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited claim status=%d body=%s", limited.Code, limited.Body.String())
	}
	store.now = func() time.Time { return now.Add(synoraClaimFailureWindow) }
	recovered := callPairing(claim, http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam_new","setup_token":"one_time_secret"}`)
	if recovered.Code != http.StatusOK {
		t.Fatalf("claim after limiter window status=%d body=%s", recovered.Code, recovered.Body.String())
	}
}

func TestSynoraCameraPairingRevocationAndExplicitResetRotateIdentity(t *testing.T) {
	provider, store := newPairingFake()
	registry := security.NewIdentityRegistry(filepath.Join(t.TempDir(), "identities.json"))
	if err := registry.Load(); err != nil {
		t.Fatal(err)
	}
	store.identityRegistry = registry
	store.requirePublicKey = true
	store.requireObservedMAC = true
	public, private, err := security.GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := completeSecurePairing(provider, store, "cam_reset", "printed-one", public, private); err != nil {
		t.Fatal(err)
	}
	revoked := callPairing(handleSynoraCameraPairingRevoke(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/revoke", `{"device_id":"cam_reset"}`)
	if revoked.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revoked.Code, revoked.Body.String())
	}
	old, _ := registry.Lookup("cam_reset")
	if old.Status != security.IdentityRevoked || provider.devices[0]["trusted"] != false {
		t.Fatalf("revocation did not close access: identity=%#v device=%#v", old, provider.devices[0])
	}

	newPublic, newPrivate, err := security.GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := completeSecurePairing(provider, store, "cam_reset", "printed-two", newPublic, newPrivate); err == nil {
		t.Fatal("reset pairing unexpectedly accepted without reset flag")
	}
	if err := completeSecurePairingWithReset(provider, store, "cam_reset", "printed-two", newPublic, newPrivate); err != nil {
		t.Fatal(err)
	}
	current, _ := registry.Lookup("cam_reset")
	if current.Status != security.IdentityActive || current.Generation != old.Generation+1 || provider.devices[0]["trusted"] != true {
		t.Fatalf("reset did not rotate identity: identity=%#v device=%#v", current, provider.devices[0])
	}
}

func completeSecurePairing(provider *fakeSynoraCameraPairingProvider, store *synoraCameraPairingStore, deviceID, token string, public ed25519.PublicKey, private ed25519.PrivateKey) error {
	return completeSecurePairingMode(provider, store, deviceID, token, public, private, false)
}

func completeSecurePairingWithReset(provider *fakeSynoraCameraPairingProvider, store *synoraCameraPairingStore, deviceID, token string, public ed25519.PublicKey, private ed25519.PrivateKey) error {
	return completeSecurePairingMode(provider, store, deviceID, token, public, private, true)
}

func completeSecurePairingMode(provider *fakeSynoraCameraPairingProvider, store *synoraCameraPairingStore, deviceID, token string, public ed25519.PublicKey, private ed25519.PrivateKey, reset bool) error {
	encodedPublic := base64.StdEncoding.EncodeToString(public)
	qr := `{"type":"synora.camera","version":1,"device_id":"` + deviceID + `","setup_token":"` + token + `","public_key":"` + encodedPublic + `"}`
	startBody := `{"raw_code":` + jsonString(qr)
	if reset {
		startBody += `,"reset":true`
	}
	startBody += `}`
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", startBody)
	if start.Code != http.StatusOK {
		return fmt.Errorf("start status=%d body=%s", start.Code, start.Body.String())
	}
	mac := "02:00:00:00:00:31"
	now := store.currentTime()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	fingerprint := security.IdentityFingerprint(public)
	message := security.PairingProofMessage(deviceID, token, timestamp, mac, fingerprint)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, message))
	claimBody := `{"device_id":"` + deviceID + `","setup_token":"` + token + `","mac":"` + mac + `","public_key":"` + encodedPublic + `","timestamp":"` + timestamp + `","signature":"` + signature + `"}`
	claim := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", claimBody)
	if claim.Code != http.StatusOK {
		return fmt.Errorf("claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	var started synoraCameraPairingStartResponse
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		return err
	}
	confirm := callPairing(handleSynoraCameraPairingConfirm(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/confirm", `{"session_id":"`+started.SessionID+`","name":"Reset camera","node_id":"zoneA.L0.entree"}`)
	if confirm.Code != http.StatusOK {
		return fmt.Errorf("confirm status=%d body=%s", confirm.Code, confirm.Body.String())
	}
	return nil
}
