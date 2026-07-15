package network

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const PairingStatePath = DefaultRunDir + "/synoranet-pairing.json"

type PairingState struct {
	Active         bool      `json:"active"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	SSIDVisible    bool      `json:"ssid_visible"`
	PendingDevices int       `json:"pending_devices"`
	PendingMACs    []string  `json:"pending_macs,omitempty"`
	NetworkPolicy  string    `json:"network_policy"`
}

func pairingStatePath() string {
	if value := strings.TrimSpace(os.Getenv("SYNORA_NETWORK_PAIRING_STATE_FILE")); value != "" {
		return value
	}
	return PairingStatePath
}

func LoadPairingState(path string) (PairingState, error) {
	if strings.TrimSpace(path) == "" {
		path = pairingStatePath()
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PairingState{NetworkPolicy: "runtime"}, nil
	}
	if err != nil {
		return PairingState{}, err
	}
	var state PairingState
	if err := json.Unmarshal(data, &state); err != nil {
		return PairingState{}, err
	}
	if state.Active && !time.Now().UTC().Before(state.ExpiresAt) {
		state.Active = false
		state.SSIDVisible = false
		state.NetworkPolicy = "runtime"
		state.PendingDevices = 0
		state.PendingMACs = nil
		// Expiry is also cleanup. Best-effort persistence makes a subsequent
		// Discovery/API process observe the closed window without stale pending
		// stations, while a read-only health probe can still succeed if /run is
		// temporarily unavailable.
		_ = writePairingState(path, state)
	}
	if state.NetworkPolicy == "" {
		state.NetworkPolicy = map[bool]string{true: "pairing", false: "runtime"}[state.Active]
	}
	return state, nil
}

func writePairingState(path string, state PairingState) error {
	if strings.TrimSpace(path) == "" {
		path = pairingStatePath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	state.PendingMACs = normalizeMACList(state.PendingMACs)
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func StartPairingWindow(cfg SynoraNetConfig, now time.Time) (PairingState, error) {
	if !cfg.Pairing.Enabled {
		return PairingState{}, errors.New("SynoraNet pairing is disabled")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := PairingState{Active: true, StartedAt: now.UTC(), ExpiresAt: now.UTC().Add(time.Duration(cfg.Pairing.WindowSeconds) * time.Second), SSIDVisible: cfg.Visibility.VisibleDuringPairing, NetworkPolicy: "pairing"}
	if err := writePairingState(pairingStatePath(), state); err != nil {
		return PairingState{}, err
	}
	return state, nil
}

func StopPairingWindow(now time.Time) (PairingState, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := PairingState{Active: false, SSIDVisible: false, NetworkPolicy: "runtime", ExpiresAt: now.UTC()}
	if err := writePairingState(pairingStatePath(), state); err != nil {
		return PairingState{}, err
	}
	return state, nil
}

func PairingWindowActive() bool {
	state, err := LoadPairingState("")
	return err == nil && state.Active && time.Now().UTC().Before(state.ExpiresAt)
}

func AddPendingMAC(mac string) error {
	mac = normalizeMAC(mac)
	if mac == "" {
		return errors.New("invalid station MAC")
	}
	state, err := LoadPairingState("")
	if err != nil {
		return err
	}
	if !state.Active {
		return errors.New("pairing window is not active")
	}
	state.PendingMACs = append(state.PendingMACs, mac)
	state.PendingDevices = len(normalizeMACList(state.PendingMACs))
	return writePairingState("", state)
}

func ClearPendingMACs() error {
	state, err := LoadPairingState("")
	if err != nil {
		return err
	}
	state.PendingMACs = nil
	state.PendingDevices = 0
	return writePairingState("", state)
}

func normalizeMAC(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 17 {
		return ""
	}
	for i, r := range value {
		if i%3 == 2 {
			if r != ':' {
				return ""
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ""
		}
	}
	return value
}

// NormalizeMAC validates and canonicalizes a station address without exposing
// any pairing secret. It is used by the API boundary before persistence.
func NormalizeMAC(value string) string { return normalizeMAC(value) }

func normalizeMACList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if mac := normalizeMAC(value); mac != "" {
			if _, ok := seen[mac]; !ok {
				seen[mac] = struct{}{}
				out = append(out, mac)
			}
		}
	}
	return out
}
