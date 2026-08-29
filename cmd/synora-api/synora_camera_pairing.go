package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"synora/internal/configfile"
	"synora/internal/discovery/network"
	"synora/internal/security"
	"synora/pkg/contract"
)

const (
	synoraCameraPairingTTL   = 10 * time.Minute
	maxSynoraCameraPayload   = 64 * 1024
	maxSynoraSetupToken      = 512
	maxSynoraPairingSessions = 16
	maxSynoraClaimFailures   = 5
	synoraClaimFailureWindow = time.Minute
)

var synoraCameraDeviceIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

type synoraCameraPairingStore struct {
	mu                 sync.Mutex
	sessions           map[string]*synoraCameraPairingSession
	now                func() time.Time
	windowActive       func() bool
	identityRegistry   *security.IdentityRegistry
	securityPath       string
	persistencePath    string
	persistenceErr     error
	claimFailures      map[string]synoraClaimFailure
	requirePublicKey   bool
	requireObservedMAC bool
}

type synoraClaimFailure struct {
	WindowStart time.Time
	Failures    int
	BlockedTill time.Time
}

type synoraCameraPairingSession struct {
	ID                   string
	DeviceID             string
	Serial               string
	Model                string
	SetupHash            string
	CreatedAt            time.Time
	ExpiresAt            time.Time
	Status               string
	Confirming           bool
	ObservedMAC          string
	ObservedIP           string
	PublicKeyFingerprint string
	PublicKey            string
	TransportSecretHash  string
	RequestedEnabled     bool
	Reset                bool
	Claiming             bool `json:"-"`
}

func newSynoraCameraPairingStore(paths ...string) *synoraCameraPairingStore {
	store := &synoraCameraPairingStore{
		sessions:      make(map[string]*synoraCameraPairingSession),
		now:           func() time.Time { return time.Now().UTC() },
		claimFailures: make(map[string]synoraClaimFailure),
	}
	if len(paths) > 0 {
		store.persistencePath = strings.TrimSpace(paths[0])
		store.persistenceErr = store.loadPersisted()
	}
	return store
}

type synoraCameraPairingProvider interface {
	Devices() ([]map[string]any, error)
	Topology() (map[string]any, error)
	CreateDevice(json.RawMessage) (map[string]any, error)
}

type synoraCameraQRPayload struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	DeviceID   string `json:"device_id"`
	Serial     string `json:"serial"`
	Model      string `json:"model"`
	SetupToken string `json:"setup_token"`
	PublicKey  string `json:"public_key,omitempty"`
}

type synoraCameraPairingStartRequest struct {
	QRPayload json.RawMessage `json:"qr_payload"`
	RawCode   string          `json:"raw_code"`
	Reset     bool            `json:"reset,omitempty"`
}

type synoraCameraPairingStartResponse struct {
	SessionID string    `json:"session_id"`
	DeviceID  string    `json:"device_id"`
	Serial    string    `json:"serial,omitempty"`
	Model     string    `json:"model,omitempty"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

type synoraCameraPairingConfirmRequest struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	NodeID    string `json:"node_id"`
	Enabled   *bool  `json:"enabled"`
}

type synoraCameraPairingClaimRequest struct {
	DeviceID             string `json:"device_id"`
	SetupToken           string `json:"setup_token"`
	Serial               string `json:"serial,omitempty"`
	Model                string `json:"model,omitempty"`
	MAC                  string `json:"mac,omitempty"`
	PublicKeyFingerprint string `json:"public_key_fingerprint,omitempty"`
	PublicKey            string `json:"public_key,omitempty"`
	Timestamp            string `json:"timestamp,omitempty"`
	Signature            string `json:"signature,omitempty"`
}

func handleSynoraCameraPairingCapabilities() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"synora_camera": map[string]bool{
				"available":   true,
				"qr_scan":     true,
				"manual_code": true,
			},
		})
	}
}

func handleSynoraCameraPairingStart(core synoraCameraPairingProvider, store *synoraCameraPairingStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if store.persistenceErr != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "pairing state unavailable"))
			return
		}
		if !store.pairingWindowActive() {
			emitNetworkPairingEvent("network.pairing.failed", map[string]any{"reason": "window_closed", "operation": "start"})
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "SynoraNet pairing window is closed"))
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}

		var request synoraCameraPairingStartRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid Synora camera pairing request"))
			return
		}
		payload, err := parseSynoraCameraQRPayload(request)
		if err != nil {
			writeError(w, err)
			return
		}
		if store.requirePublicKey && strings.TrimSpace(payload.PublicKey) == "" {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "camera public key is required"))
			return
		}

		devices, err := core.Devices()
		if err != nil {
			writeError(w, err)
			return
		}
		for _, device := range devices {
			if id, _ := device["id"].(string); strings.TrimSpace(id) == payload.DeviceID {
				if !request.Reset || !store.identityRevoked(payload.DeviceID) || !deviceResettable(device) {
					writeError(w, contract.NewAPIError(contract.ErrorDuplicateID, "device %q already exists", payload.DeviceID))
					return
				}
			}
		}

		now := store.currentTime()
		sessionID, err := newPairingSessionID()
		if err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "could not create pairing session"))
			return
		}
		expiresAt := now.Add(synoraCameraPairingTTL)
		store.mu.Lock()
		store.cleanupLocked(now)
		for _, existing := range store.sessions {
			if existing != nil && existing.DeviceID == payload.DeviceID {
				store.mu.Unlock()
				writeError(w, contract.NewAPIError(contract.ErrorConflict, "a pairing session already exists for this camera"))
				return
			}
		}
		if len(store.sessions) >= maxSynoraPairingSessions {
			store.mu.Unlock()
			writeError(w, contract.NewAPIError(contract.ErrorConflict, "too many pairing sessions"))
			return
		}
		store.sessions[sessionID] = &synoraCameraPairingSession{
			ID:                   sessionID,
			DeviceID:             payload.DeviceID,
			Serial:               payload.Serial,
			Model:                payload.Model,
			SetupHash:            hashPairingSecret(payload.SetupToken),
			PublicKey:            strings.TrimSpace(payload.PublicKey),
			PublicKeyFingerprint: pairingPublicKeyFingerprint(payload.PublicKey),
			CreatedAt:            now,
			ExpiresAt:            expiresAt,
			Status:               "ready",
			Reset:                request.Reset,
		}
		if err := store.persistLocked(); err != nil {
			delete(store.sessions, sessionID)
			store.mu.Unlock()
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "pairing state unavailable"))
			return
		}
		store.mu.Unlock()

		writeJSON(w, http.StatusOK, synoraCameraPairingStartResponse{
			SessionID: sessionID,
			DeviceID:  payload.DeviceID,
			Serial:    payload.Serial,
			Model:     payload.Model,
			Status:    "ready_to_confirm",
			ExpiresAt: expiresAt,
		})
	}
}

func handleSynoraCameraPairingConfirm(core synoraCameraPairingProvider, store *synoraCameraPairingStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !store.pairingWindowActive() {
			emitNetworkPairingEvent("network.pairing.failed", map[string]any{"reason": "window_closed", "operation": "confirm"})
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "SynoraNet pairing window is closed"))
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		var request synoraCameraPairingConfirmRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid Synora camera confirmation"))
			return
		}
		request.SessionID = strings.TrimSpace(request.SessionID)
		request.Name = strings.TrimSpace(request.Name)
		request.NodeID = strings.TrimSpace(request.NodeID)
		if request.SessionID == "" {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "session_id is required"))
			return
		}
		if request.Name == "" || len(request.Name) > 128 {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "name is required and must be at most 128 characters"))
			return
		}
		if request.NodeID == "" || len(request.NodeID) > 256 {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "node_id is required"))
			return
		}

		session, ok := store.beginConfirm(request.SessionID)
		if !ok {
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "pairing session is missing, expired, or already consumed"))
			return
		}
		requestedEnabled := true
		if request.Enabled != nil {
			requestedEnabled = *request.Enabled
		}
		if session.Status != "device_seen" {
			store.resetConfirm(request.SessionID)
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "camera proof of possession is required before confirmation"))
			return
		}
		session.RequestedEnabled = requestedEnabled
		if !store.setRequestedEnabled(request.SessionID, requestedEnabled) {
			store.resetConfirm(request.SessionID)
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "pairing session is missing, expired, or already consumed"))
			return
		}
		if topology, err := core.Topology(); err == nil {
			if available, exists := topologyContainsNode(topology, request.NodeID); available && !exists {
				store.resetConfirm(request.SessionID)
				writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "node_id %q is not present in topology", request.NodeID))
				return
			}
		}

		createPayload := map[string]any{
			"id":             session.DeviceID,
			"name":           request.Name,
			"type":           contract.DeviceTypeCamera,
			"vendor":         "synora",
			"model":          session.Model,
			"pairing_method": "synora_qr",
			"status":         "pending",
			"trusted":        true,
			"enabled":        requestedEnabled,
			"node_id":        request.NodeID,
			"network": map[string]any{
				"allow_wifi":    false,
				"network_trust": "pending",
			},
		}
		if session.ObservedMAC != "" {
			createPayload["network"] = map[string]any{
				"mac":                    session.ObservedMAC,
				"last_seen_mac":          session.ObservedMAC,
				"last_seen_ip":           session.ObservedIP,
				"public_key_fingerprint": session.PublicKeyFingerprint,
				"allow_wifi":             true,
				"network_trust":          "paired",
				"paired_at":              store.currentTime(),
			}
		}
		if session.Serial != "" {
			createPayload["serial"] = session.Serial
		}
		if store.identityRegistry != nil && session.PublicKey != "" {
			publicKey, err := base64.StdEncoding.DecodeString(session.PublicKey)
			if err != nil || len(publicKey) != ed25519.PublicKeySize {
				store.resetConfirm(request.SessionID)
				writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "camera identity is invalid"))
				return
			}
			var identityErr error
			if session.Reset {
				_, identityErr = store.identityRegistry.Reset(session.DeviceID, ed25519.PublicKey(publicKey))
			} else {
				_, identityErr = store.identityRegistry.Register(session.DeviceID, security.IdentityCamera, ed25519.PublicKey(publicKey))
			}
			if identityErr != nil {
				store.resetConfirm(request.SessionID)
				writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "camera identity could not be registered"))
				return
			}
		}
		if err := store.persistTransportSecret(session); err != nil {
			if store.identityRegistry != nil && session.PublicKey != "" {
				_ = store.identityRegistry.Revoke(session.DeviceID, "pairing credential persistence failed")
			}
			store.resetConfirm(request.SessionID)
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "pairing credential unavailable"))
			return
		}
		devicePayload := createPayload
		if session.Reset {
			devicePayload = map[string]any{
				"name":    request.Name,
				"node_id": request.NodeID,
				"enabled": requestedEnabled,
				"trusted": true,
				"network": createPayload["network"],
			}
		}
		encoded, _ := json.Marshal(devicePayload)
		var device map[string]any
		var err error
		if session.Reset {
			if updater, ok := core.(synoraCameraDeviceUpdater); ok {
				device, err = updater.UpdateDevice(session.DeviceID, encoded)
			} else {
				err = errors.New("camera reset requires device update support")
			}
		} else {
			device, err = core.CreateDevice(encoded)
		}
		if err != nil {
			if store.identityRegistry != nil && session.PublicKey != "" {
				_ = store.identityRegistry.Revoke(session.DeviceID, "pairing device creation failed")
			}
			store.resetConfirm(request.SessionID)
			writeError(w, err)
			return
		}
		store.consume(request.SessionID)
		writeJSON(w, http.StatusOK, map[string]any{"device": device, "status": "paired"})
	}
}

func handleSynoraCameraPairingClaim(store *synoraCameraPairingStore) http.HandlerFunc {
	return handleSynoraCameraPairingClaimWithProvider(nil, store)
}

type synoraCameraDeviceUpdater interface {
	UpdateDevice(string, json.RawMessage) (map[string]any, error)
}

type synoraCameraDeviceReader interface {
	Device(string) (map[string]any, error)
}

func handleSynoraCameraPairingClaimWithProvider(core synoraCameraDeviceUpdater, store *synoraCameraPairingStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !store.pairingWindowActive() {
			emitNetworkPairingEvent("network.pairing.failed", map[string]any{"reason": "window_closed", "operation": "claim"})
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "SynoraNet pairing window is closed"))
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		var request synoraCameraPairingClaimRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid Synora camera claim"))
			return
		}
		request.DeviceID = strings.TrimSpace(request.DeviceID)
		request.Serial = strings.TrimSpace(request.Serial)
		request.Model = strings.TrimSpace(request.Model)
		request.MAC = network.NormalizeMAC(strings.TrimSpace(pairingFirstNonEmptyString(request.MAC, r.Header.Get("X-Synora-Station-MAC"))))
		request.PublicKeyFingerprint = strings.TrimSpace(request.PublicKeyFingerprint)
		if request.DeviceID == "" || len(request.SetupToken) == 0 || len(request.SetupToken) > maxSynoraSetupToken {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "device_id and setup_token are required"))
			return
		}
		if store.requireObservedMAC && request.MAC == "" {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "camera MAC observation is required"))
			return
		}
		attemptKey := pairingAttemptKey(request.DeviceID, requestIP(r))
		if !store.allowClaimAttempt(attemptKey) {
			writeError(w, contract.NewAPIError(contract.ErrorRateLimited, "pairing attempts are temporarily limited"))
			return
		}
		if !store.validateClaimProof(request.DeviceID, request.SetupToken, request.MAC, request.PublicKey, request.Timestamp, request.Signature) {
			store.recordClaimFailure(attemptKey)
			emitNetworkPairingEvent("network.pairing.failed", map[string]any{"reason": "invalid_pairing_proof", "device_id": request.DeviceID})
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "pairing session is missing, expired, or proof is invalid"))
			return
		}
		if request.PublicKey != "" {
			request.PublicKeyFingerprint = pairingPublicKeyFingerprint(request.PublicKey)
		}
		observedIP := requestIP(r)
		session, ok := store.beginDeviceClaimWithMetadata(request.DeviceID, request.SetupToken, request.MAC, observedIP, request.PublicKeyFingerprint)
		if !ok {
			store.recordClaimFailure(attemptKey)
			emitNetworkPairingEvent("network.pairing.failed", map[string]any{"reason": "invalid_or_expired_claim", "device_id": request.DeviceID})
			writeError(w, contract.NewAPIError(contract.ErrorNotFound, "pairing session is missing, expired, or token is invalid"))
			return
		}
		if request.MAC != "" {
			_ = network.AddPendingMAC(request.MAC)
		}
		if core != nil {
			networkTrust := "paired"
			allowWiFi := true
			currentMAC := ""
			devicePresent := true
			if reader, ok := core.(synoraCameraDeviceReader); ok {
				current, err := reader.Device(request.DeviceID)
				if err == nil && current == nil {
					devicePresent = false
				} else if err != nil && contract.APIErrorCode(err) == contract.ErrorNotFound {
					devicePresent = false
				} else if err != nil {
					store.resetDeviceClaim(request.DeviceID)
					writeError(w, err)
					return
				} else {
					currentMAC = network.NormalizeMAC(networkMACFromDevice(current))
				}
			}
			if currentMAC != "" && session.ObservedMAC != "" && currentMAC != session.ObservedMAC {
				networkTrust = "security_warning"
				allowWiFi = false
			}
			networkData := map[string]any{
				"allow_wifi":    allowWiFi,
				"network_trust": networkTrust,
				"paired_at":     store.currentTime(),
			}
			if session.ObservedMAC != "" && (currentMAC == "" || currentMAC == session.ObservedMAC) {
				networkData["mac"] = session.ObservedMAC
				networkData["last_seen_mac"] = session.ObservedMAC
			} else if session.ObservedMAC != "" {
				networkData["last_seen_mac"] = session.ObservedMAC
			}
			if session.ObservedIP != "" {
				networkData["last_seen_ip"] = session.ObservedIP
			}
			if session.PublicKeyFingerprint != "" {
				networkData["public_key_fingerprint"] = session.PublicKeyFingerprint
			}
			if devicePresent {
				encoded, _ := json.Marshal(map[string]any{
					"enabled": requestEnabledForClaim(session),
					"trusted": true,
					"network": networkData,
				})
				if _, err := core.UpdateDevice(request.DeviceID, encoded); err != nil {
					store.resetDeviceClaim(request.DeviceID)
					writeError(w, err)
					return
				}
			}
		}
		if err := store.persistTransportSecret(session); err != nil {
			store.resetDeviceClaim(request.DeviceID)
			writeError(w, contract.NewAPIError(contract.ErrorInternal, "pairing credential unavailable"))
			return
		}
		if !store.completeDeviceClaim(request.DeviceID, request.MAC, observedIP, request.PublicKeyFingerprint) {
			writeError(w, contract.NewAPIError(contract.ErrorConflict, "pairing claim could not be committed"))
			return
		}
		store.clearClaimFailures(attemptKey)
		emitNetworkPairingEvent("network.pairing.claimed", map[string]any{"device_id": request.DeviceID, "mac_observed": request.MAC != ""})
		emitNetworkPairingEvent("network.station.allowed", map[string]any{"device_id": request.DeviceID, "mac_observed": request.MAC != ""})
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "device_id": request.DeviceID})
	}
}

type synoraCameraIdentityActionRequest struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason,omitempty"`
}

func handleSynoraCameraPairingRevoke(core synoraCameraDeviceUpdater, store *synoraCameraPairingStore) http.HandlerFunc {
	return handleSynoraCameraIdentityAction(core, store, "revoked")
}

func handleSynoraCameraPairingReset(core synoraCameraDeviceUpdater, store *synoraCameraPairingStore) http.HandlerFunc {
	return handleSynoraCameraIdentityAction(core, store, "reset")
}

func handleSynoraCameraIdentityAction(core synoraCameraDeviceUpdater, store *synoraCameraPairingStore, operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		body, ok := readJSONObject(w, r, true)
		if !ok {
			return
		}
		var request synoraCameraIdentityActionRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeError(w, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid camera identity action"))
			return
		}
		request.DeviceID = strings.TrimSpace(request.DeviceID)
		if !synoraCameraDeviceIDPattern.MatchString(request.DeviceID) {
			writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "device_id is invalid"))
			return
		}
		if operation == "reset" {
			request.Reason = "explicit camera reset"
		} else if strings.TrimSpace(request.Reason) == "" {
			request.Reason = "camera access revoked"
		}
		if store.identityRegistry != nil {
			if err := store.identityRegistry.Revoke(request.DeviceID, request.Reason); err != nil && !errors.Is(err, security.ErrIdentityNotFound) {
				writeError(w, contract.NewAPIError(contract.ErrorNotFound, "camera identity not found"))
				return
			}
		}
		store.removeSessionsForDevice(request.DeviceID)
		if core != nil {
			payload, _ := json.Marshal(map[string]any{
				"enabled": false,
				"trusted": false,
				"network": map[string]any{
					"allow_wifi":    false,
					"network_trust": "revoked",
				},
			})
			if _, err := core.UpdateDevice(request.DeviceID, payload); err != nil {
				writeError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "device_id": request.DeviceID})
	}
}

func networkMACFromDevice(value map[string]any) string {
	if value == nil {
		return ""
	}
	if networkValue, ok := value["network"].(map[string]any); ok {
		if mac, ok := networkValue["mac"].(string); ok {
			return mac
		}
	}
	return ""
}

func (s *synoraCameraPairingStore) pairingWindowActive() bool {
	if s == nil || s.windowActive == nil {
		return true
	}
	return s.windowActive()
}

func (s *synoraCameraPairingStore) validateClaimProof(deviceID, setupToken, mac, publicKeyEncoded, timestamp, signature string) bool {
	now := s.currentTime()
	s.mu.Lock()
	s.cleanupLocked(now)
	var expected *synoraCameraPairingSession
	for _, session := range s.sessions {
		if session != nil && session.DeviceID == deviceID && session.Status != "consumed" && !session.Confirming && subtle.ConstantTimeCompare([]byte(session.SetupHash), []byte(hashPairingSecret(setupToken))) == 1 {
			copy := *session
			expected = &copy
			break
		}
	}
	s.mu.Unlock()
	if expected == nil {
		return false
	}
	if strings.TrimSpace(expected.PublicKey) == "" {
		return !s.requirePublicKey
	}
	if strings.TrimSpace(publicKeyEncoded) != expected.PublicKey {
		return false
	}
	publicKey, err := base64.StdEncoding.DecodeString(publicKeyEncoded)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	fingerprint := security.IdentityFingerprint(ed25519.PublicKey(publicKey))
	if expected.PublicKeyFingerprint != "" && expected.PublicKeyFingerprint != fingerprint {
		return false
	}
	return security.VerifyPairingProof(ed25519.PublicKey(publicKey), deviceID, setupToken, timestamp, mac, fingerprint, signature, now, security.DefaultTimestampSkew) == nil
}

func pairingFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func requestEnabledForClaim(session synoraCameraPairingSession) bool {
	return session.RequestedEnabled
}

func requestIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return ""
	}
	if index := strings.LastIndex(host, ":"); index > -1 && !strings.Contains(host[index+1:], "]") {
		return strings.Trim(host[:index], "[]")
	}
	return strings.Trim(host, "[]")
}

func parseSynoraCameraQRPayload(request synoraCameraPairingStartRequest) (synoraCameraQRPayload, error) {
	raw := request.QRPayload
	if len(raw) == 0 {
		code := strings.TrimSpace(request.RawCode)
		if code == "" {
			return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "qr_payload or raw_code is required")
		}
		if len(code) > maxSynoraCameraPayload {
			return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "QR payload is too large")
		}
		raw = json.RawMessage(code)
	}
	if len(raw) > maxSynoraCameraPayload || !json.Valid(raw) {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "invalid QR payload")
	}
	var payload synoraCameraQRPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "invalid QR payload")
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.DeviceID = strings.TrimSpace(payload.DeviceID)
	payload.Serial = strings.TrimSpace(payload.Serial)
	payload.Model = strings.TrimSpace(payload.Model)
	payload.PublicKey = strings.TrimSpace(payload.PublicKey)
	if payload.Type != "synora.camera" {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "unsupported QR device type")
	}
	if payload.Version < 1 {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "unsupported QR payload version")
	}
	if !synoraCameraDeviceIDPattern.MatchString(payload.DeviceID) {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "device_id must contain only lowercase letters, numbers, underscores, or hyphens")
	}
	if len(payload.DeviceID) > 128 || len(payload.Serial) > 128 || len(payload.Model) > 128 {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "QR payload field is too long")
	}
	if len(payload.SetupToken) < 8 || len(payload.SetupToken) > maxSynoraSetupToken {
		return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "setup_token has an invalid length")
	}
	if payload.PublicKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(payload.PublicKey)
		if err != nil || len(decoded) != ed25519.PublicKeySize {
			return synoraCameraQRPayload{}, contract.NewAPIError(contract.ErrorValidationFailed, "public_key has an invalid format")
		}
	}
	return payload, nil
}

func pairingPublicKeyFingerprint(encoded string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return ""
	}
	return security.IdentityFingerprint(ed25519.PublicKey(decoded))
}

func (s *synoraCameraPairingStore) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

const synoraPairingDiskVersion = 1

type synoraPairingDisk struct {
	Version  int                          `json:"version"`
	Sessions []synoraCameraPairingSession `json:"sessions"`
}

func (s *synoraCameraPairingStore) loadPersisted() error {
	if strings.TrimSpace(s.persistencePath) == "" {
		return nil
	}
	info, err := os.Lstat(s.persistencePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("pairing state file is unsafe")
	}
	data, err := os.ReadFile(s.persistencePath)
	if err != nil {
		return err
	}
	var disk synoraPairingDisk
	if err := json.Unmarshal(data, &disk); err != nil || disk.Version != synoraPairingDiskVersion {
		return errors.New("invalid pairing state format")
	}
	for _, session := range disk.Sessions {
		if strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.DeviceID) == "" ||
			session.Status != "ready" && session.Status != "device_seen" {
			return errors.New("invalid pairing session")
		}
		copy := session
		s.sessions[session.ID] = &copy
	}
	return nil
}

func (s *synoraCameraPairingStore) persistLocked() error {
	if strings.TrimSpace(s.persistencePath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.persistencePath), 0700); err != nil {
		return err
	}
	sessions := make([]synoraCameraPairingSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session == nil || session.Status == "consumed" {
			continue
		}
		copy := *session
		copy.Confirming = false
		copy.Claiming = false
		sessions = append(sessions, copy)
	}
	data, err := json.MarshalIndent(synoraPairingDisk{Version: synoraPairingDiskVersion, Sessions: sessions}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return configfile.WriteAtomicWithBackup(s.persistencePath, data, 0600)
}

func (s *synoraCameraPairingStore) identityRevoked(deviceID string) bool {
	if s == nil || s.identityRegistry == nil {
		return false
	}
	record, ok := s.identityRegistry.Lookup(strings.TrimSpace(deviceID))
	return ok && record.Kind == security.IdentityCamera && record.Status == security.IdentityRevoked
}

func deviceResettable(device map[string]any) bool {
	if device == nil {
		return false
	}
	if deleted, ok := device["deleted_at"]; ok && deleted != nil {
		return true
	}
	if trusted, ok := device["trusted"].(bool); ok && !trusted {
		return true
	}
	if networkData, ok := device["network"].(map[string]any); ok {
		trust, _ := networkData["network_trust"].(string)
		return strings.TrimSpace(trust) == "revoked"
	}
	return false
}

func (s *synoraCameraPairingStore) persistTransportSecret(session synoraCameraPairingSession) error {
	if strings.TrimSpace(s.securityPath) == "" || strings.TrimSpace(session.TransportSecretHash) == "" {
		return nil
	}
	cfg, err := security.Load(s.securityPath)
	if err != nil {
		return err
	}
	if cfg.DeviceSecrets == nil {
		cfg.DeviceSecrets = make(map[string]string)
	}
	cfg.DeviceSecrets[session.DeviceID] = session.TransportSecretHash
	return security.Save(s.securityPath, cfg)
}

func (s *synoraCameraPairingStore) setRequestedEnabled(id string, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[id]
	if session == nil || session.Status != "device_seen" {
		return false
	}
	session.RequestedEnabled = enabled
	return s.persistLocked() == nil
}

func pairingAttemptKey(deviceID, remote string) string {
	deviceID = strings.TrimSpace(deviceID)
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return deviceID
	}
	return deviceID + "|" + remote
}

func (s *synoraCameraPairingStore) allowClaimAttempt(key string) bool {
	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimFailures == nil {
		s.claimFailures = make(map[string]synoraClaimFailure)
	}
	state, ok := s.claimFailures[key]
	if ok && now.Before(state.BlockedTill) {
		return false
	}
	if ok && !now.Before(state.WindowStart.Add(synoraClaimFailureWindow)) {
		delete(s.claimFailures, key)
	}
	return true
}

func (s *synoraCameraPairingStore) recordClaimFailure(key string) {
	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimFailures == nil {
		s.claimFailures = make(map[string]synoraClaimFailure)
	}
	state, known := s.claimFailures[key]
	if !known && len(s.claimFailures) >= 1024 {
		for candidate, value := range s.claimFailures {
			if !now.Before(value.BlockedTill) && !now.Before(value.WindowStart.Add(synoraClaimFailureWindow)) {
				delete(s.claimFailures, candidate)
				break
			}
		}
		if len(s.claimFailures) >= 1024 {
			return
		}
	}
	if state.WindowStart.IsZero() || !now.Before(state.WindowStart.Add(synoraClaimFailureWindow)) {
		state = synoraClaimFailure{WindowStart: now}
	}
	state.Failures++
	if state.Failures >= maxSynoraClaimFailures {
		state.BlockedTill = now.Add(synoraClaimFailureWindow)
	}
	s.claimFailures[key] = state
}

func (s *synoraCameraPairingStore) clearClaimFailures(key string) {
	s.mu.Lock()
	delete(s.claimFailures, key)
	s.mu.Unlock()
}

func (s *synoraCameraPairingStore) removeSessionsForDevice(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for id, session := range s.sessions {
		if session != nil && session.DeviceID == deviceID {
			delete(s.sessions, id)
			changed = true
		}
	}
	if changed {
		_ = s.persistLocked()
	}
}

func (s *synoraCameraPairingStore) cleanupLocked(now time.Time) {
	for id, session := range s.sessions {
		if session == nil || !now.Before(session.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *synoraCameraPairingStore) beginConfirm(id string) (synoraCameraPairingSession, bool) {
	now := s.currentTime()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	session, ok := s.sessions[id]
	if !ok || session == nil || session.Confirming || (session.Status != "ready" && session.Status != "device_seen") {
		return synoraCameraPairingSession{}, false
	}
	session.Confirming = true
	return *session, true
}

func (s *synoraCameraPairingStore) resetConfirm(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[id]; session != nil {
		session.Confirming = false
		_ = s.persistLocked()
	}
}

func (s *synoraCameraPairingStore) consume(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	_ = s.persistLocked()
}

func (s *synoraCameraPairingStore) markDeviceSeen(deviceID, token string) bool {
	_, ok := s.markDeviceSeenWithMetadata(deviceID, token, "", "", "")
	return ok
}

func (s *synoraCameraPairingStore) markDeviceSeenWithMetadata(deviceID, token, mac, ip, fingerprint string) (synoraCameraPairingSession, bool) {
	session, ok := s.beginDeviceClaimWithMetadata(deviceID, token, mac, ip, fingerprint)
	if !ok {
		return synoraCameraPairingSession{}, false
	}
	if !s.completeDeviceClaim(deviceID, mac, ip, fingerprint) {
		return synoraCameraPairingSession{}, false
	}
	return session, true
}

func (s *synoraCameraPairingStore) beginDeviceClaimWithMetadata(deviceID, token, mac, ip, fingerprint string) (synoraCameraPairingSession, bool) {
	now := s.currentTime()
	hash := hashPairingSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	for _, session := range s.sessions {
		if session == nil || session.DeviceID != deviceID || session.Confirming ||
			session.Claiming || session.Status != "ready" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(session.SetupHash), []byte(hash)) != 1 {
			continue
		}
		session.Claiming = true
		session.TransportSecretHash = security.DeriveDeviceTransportSecret(deviceID, token, fingerprint)
		return *session, true
	}
	return synoraCameraPairingSession{}, false
}

func (s *synoraCameraPairingStore) resetDeviceClaim(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		if session != nil && session.DeviceID == deviceID && session.Claiming {
			session.Claiming = false
			_ = s.persistLocked()
			return
		}
	}
}

func (s *synoraCameraPairingStore) completeDeviceClaim(deviceID, mac, ip, fingerprint string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.sessions {
		if session == nil || session.DeviceID != deviceID || !session.Claiming {
			continue
		}
		previous := *session
		session.Status = "device_seen"
		session.ObservedMAC = mac
		session.ObservedIP = ip
		session.PublicKeyFingerprint = fingerprint
		session.Claiming = false
		if err := s.persistLocked(); err != nil {
			*session = previous
			return false
		}
		return true
	}
	return false
}

func newPairingSessionID() (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func hashPairingSecret(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func topologyContainsNode(topology map[string]any, wanted string) (available bool, exists bool) {
	if topology == nil {
		return false, false
	}
	if nested, ok := topology["topology"].(map[string]any); ok {
		return topologyContainsNode(nested, wanted)
	}
	nodes, ok := topology["nodes"].([]any)
	if !ok || len(nodes) == 0 {
		return false, false
	}
	return true, topologyNodeListContains(nodes, wanted)
}

func topologyNodeListContains(nodes []any, wanted string) bool {
	for _, value := range nodes {
		node, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := node["id"].(string); id == wanted {
			return true
		}
		if children, ok := node["children"].([]any); ok && topologyNodeListContains(children, wanted) {
			return true
		}
	}
	return false
}
