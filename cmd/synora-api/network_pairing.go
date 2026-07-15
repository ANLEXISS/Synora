package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"synora/internal/discovery/network"
	"synora/pkg/contract"
)

var publishNetworkPairingEvent func(string, map[string]any)

func emitNetworkPairingEvent(event string, payload map[string]any) {
	if publishNetworkPairingEvent != nil {
		publishNetworkPairingEvent(event, payload)
	}
}

func synoraNetworkConfig() (network.SynoraNetConfig, error) {
	cfg, err := network.LoadConfig(strings.TrimSpace(os.Getenv("SYNORA_NETWORK_CONFIG")))
	return cfg.SynoraNet, err
}

func handleSynoraNetPairingStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		cfg, err := synoraNetworkConfig()
		if err != nil {
			writeError(w, err)
			return
		}
		state, err := network.LoadPairingState("")
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"active":          state.Active,
			"expires_at":      state.ExpiresAt,
			"ssid_visible":    state.Active && cfg.Visibility.VisibleDuringPairing,
			"pending_devices": state.PendingDevices,
			"known_devices":   network.KnownStationCount(os.Getenv("SYNORA_DEVICE")),
			"network_policy":  map[bool]string{true: "pairing", false: "runtime"}[state.Active],
		})
	}
}

func handleSynoraNetPairingWindowStart() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
			return
		}
		cfg, err := synoraNetworkConfig()
		if err != nil {
			writeError(w, err)
			return
		}
		state, err := network.StartPairingWindow(cfg, timeNow())
		if err != nil {
			writeError(w, err)
			return
		}
		emitNetworkPairingEvent("network.pairing.started", map[string]any{"expires_at": state.ExpiresAt, "ssid_visible": state.SSIDVisible})
		emitNetworkPairingEvent("network.policy.changed", map[string]any{"policy": "pairing"})
		writeJSON(w, http.StatusOK, map[string]any{"active": state.Active, "expires_at": state.ExpiresAt, "ssid_visible": state.SSIDVisible, "network_policy": "pairing"})
	}
}

func handleSynoraNetPairingWindowStop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		if !isAdminRequest(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden", "message": "admin access required"})
			return
		}
		state, err := network.StopPairingWindow(timeNow())
		if err != nil {
			writeError(w, err)
			return
		}
		emitNetworkPairingEvent("network.pairing.expired", map[string]any{"expires_at": state.ExpiresAt})
		emitNetworkPairingEvent("network.policy.changed", map[string]any{"policy": "runtime"})
		writeJSON(w, http.StatusOK, map[string]any{"active": state.Active, "expires_at": state.ExpiresAt, "ssid_visible": false, "network_policy": "runtime"})
	}
}

func sendNetworkPairingEvent(sender interface{ Send(contract.Message) error }, event string, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = sender.Send(contract.Message{Type: event, Kind: contract.KindEvent, Source: "api", SourceType: contract.SourceSystem, Payload: data})
}

var timeNow = func() time.Time { return time.Now().UTC() }
