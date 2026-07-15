package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"synora/internal/discovery/network"
	"synora/pkg/contract"
)

const (
	synoraCameraPairingTTL = 10 * time.Minute
	maxSynoraCameraPayload = 64 * 1024
	maxSynoraSetupToken    = 512
)

var synoraCameraDeviceIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

type synoraCameraPairingStore struct {
	mu           sync.Mutex
	sessions     map[string]*synoraCameraPairingSession
	now          func() time.Time
	windowActive func() bool
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
}

func newSynoraCameraPairingStore() *synoraCameraPairingStore {
	return &synoraCameraPairingStore{
		sessions: make(map[string]*synoraCameraPairingSession),
		now:      func() time.Time { return time.Now().UTC() },
	}
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
}

type synoraCameraPairingStartRequest struct {
	QRPayload json.RawMessage `json:"qr_payload"`
	RawCode   string          `json:"raw_code"`
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

		devices, err := core.Devices()
		if err != nil {
			writeError(w, err)
			return
		}
		for _, device := range devices {
			if id, _ := device["id"].(string); strings.TrimSpace(id) == payload.DeviceID {
				writeError(w, contract.NewAPIError(contract.ErrorDuplicateID, "device %q already exists", payload.DeviceID))
				return
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
		store.sessions[sessionID] = &synoraCameraPairingSession{
			ID:        sessionID,
			DeviceID:  payload.DeviceID,
			Serial:    payload.Serial,
			Model:     payload.Model,
			SetupHash: hashPairingSecret(payload.SetupToken),
			CreatedAt: now,
			ExpiresAt: expiresAt,
			Status:    "ready",
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
		if topology, err := core.Topology(); err == nil {
			if available, exists := topologyContainsNode(topology, request.NodeID); available && !exists {
				store.resetConfirm(request.SessionID)
				writeError(w, contract.NewAPIError(contract.ErrorValidationFailed, "node_id %q is not present in topology", request.NodeID))
				return
			}
		}

		enabled := true
		if request.Enabled != nil {
			enabled = *request.Enabled
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
			"enabled":        enabled,
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
		encoded, _ := json.Marshal(createPayload)
		device, err := core.CreateDevice(encoded)
		if err != nil {
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
		observedIP := requestIP(r)
		session, ok := store.markDeviceSeenWithMetadata(request.DeviceID, request.SetupToken, request.MAC, observedIP, request.PublicKeyFingerprint)
		if !ok {
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
			if reader, ok := core.(synoraCameraDeviceReader); ok {
				if current, err := reader.Device(request.DeviceID); err == nil {
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
			encoded, _ := json.Marshal(map[string]any{"network": networkData})
			if _, err := core.UpdateDevice(request.DeviceID, encoded); err != nil {
				writeError(w, err)
				return
			}
		}
		emitNetworkPairingEvent("network.pairing.claimed", map[string]any{"device_id": request.DeviceID, "mac_observed": request.MAC != ""})
		emitNetworkPairingEvent("network.station.allowed", map[string]any{"device_id": request.DeviceID, "mac_observed": request.MAC != ""})
		writeJSON(w, http.StatusOK, map[string]string{"status": "accepted", "device_id": request.DeviceID})
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

func pairingFirstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
	return payload, nil
}

func (s *synoraCameraPairingStore) currentTime() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
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
	}
}

func (s *synoraCameraPairingStore) consume(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *synoraCameraPairingStore) markDeviceSeen(deviceID, token string) bool {
	_, ok := s.markDeviceSeenWithMetadata(deviceID, token, "", "", "")
	return ok
}

func (s *synoraCameraPairingStore) markDeviceSeenWithMetadata(deviceID, token, mac, ip, fingerprint string) (synoraCameraPairingSession, bool) {
	now := s.currentTime()
	hash := hashPairingSecret(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	for _, session := range s.sessions {
		if session == nil || session.DeviceID != deviceID || session.Confirming ||
			(session.Status != "ready" && session.Status != "device_seen") {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(session.SetupHash), []byte(hash)) != 1 {
			continue
		}
		session.Status = "device_seen"
		session.ObservedMAC = mac
		session.ObservedIP = ip
		session.PublicKeyFingerprint = fingerprint
		return *session, true
	}
	return synoraCameraPairingSession{}, false
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
