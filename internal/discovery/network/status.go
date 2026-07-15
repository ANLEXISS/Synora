package network

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Enabled          bool                   `json:"enabled"`
	Status           string                 `json:"status"`
	ActiveBand       string                 `json:"active_band,omitempty"`
	Message          string                 `json:"message,omitempty"`
	SynoraNet        RuntimePart            `json:"synoranet"`
	AP5GHz           RuntimePart            `json:"ap_5ghz"`
	AP2GHz           RuntimePart            `json:"ap_2ghz"`
	DHCP             RuntimePart            `json:"dhcp"`
	DNS              RuntimePart            `json:"dns"`
	WifiSecurity     WifiSecurityStatus     `json:"wifi_security"`
	Visibility       VisibilityStatus       `json:"synoranet_visibility"`
	AccessControl    AccessControlStatus    `json:"synoranet_access_control"`
	ConnectionPolicy ConnectionPolicyStatus `json:"synoranet_connection_policy"`
	PairingSecurity  PairingSecurityStatus  `json:"pairing_security"`
	NetworkIsolation RuntimePart            `json:"network_isolation"`
	Firewall         RuntimePart            `json:"firewall"`
	HTTPSAPI         RuntimePart            `json:"https_api"`
	MediaMTX         RuntimePart            `json:"mediamtx_rtsp"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

type VisibilityStatus struct {
	RuntimePart
	Hidden         bool `json:"hidden"`
	PairingVisible bool `json:"pairing_visible"`
}

type AccessControlStatus struct {
	RuntimePart
	Enabled          bool   `json:"enabled"`
	StationAllowlist bool   `json:"station_allowlist"`
	KnownDevices     int    `json:"known_devices"`
	PendingDevices   int    `json:"pending_devices"`
	UnknownPolicy    string `json:"unknown_policy"`
}

type ConnectionPolicyStatus struct {
	RuntimePart
	Mode                     string `json:"mode"`
	PairingWindowActive      bool   `json:"pairing_window_active"`
	CameraPushRuntimeAllowed bool   `json:"camera_push_runtime_allowed"`
}

type PairingSecurityStatus struct {
	RuntimePart
	Active              bool      `json:"active"`
	ExpiresAt           time.Time `json:"expires_at,omitempty"`
	ClaimEndpointActive bool      `json:"claim_endpoint_active"`
	MaxPendingDevices   int       `json:"max_pending_devices"`
}

func wifiSecurityStatus(cfg SynoraNetConfig, active bool) WifiSecurityStatus {
	status := "degraded"
	message := "legacy/weak WPA2 mode; use WPA3-SAE with required PMF"
	if cfg.Security.Mode == "wpa3" && cfg.Security.PMF == "required" && cfg.Security.APIsolate {
		status, message = "ok", "WPA3-SAE only, PMF required, client isolation enabled"
	} else if cfg.Security.Mode == "wpa2-wpa3-transition" {
		message = "transition mode explicitly enabled; WPA2 clients remain accepted"
	}
	if !active && cfg.Enabled {
		status = "unavailable"
		message = "secure AP is not active"
	}
	return WifiSecurityStatus{RuntimePart: RuntimePart{Status: status, Active: active, Message: message}, Mode: cfg.Security.Mode, PMF: cfg.Security.PMF, APIsolate: cfg.Security.APIsolate}
}

var statusMu sync.RWMutex
var currentStatus = Status{Status: "disabled", SynoraNet: RuntimePart{Status: "disabled"}}

func localHTTPSStatus() RuntimePart {
	cert := os.Getenv("SYNORA_TLS_CERT_FILE")
	key := os.Getenv("SYNORA_TLS_KEY_FILE")
	if cert == "" {
		cert = "/etc/synora/tls/synora.crt"
	}
	if key == "" {
		key = "/etc/synora/tls/synora.key"
	}
	if _, err := os.Stat(cert); err != nil {
		return RuntimePart{Status: "degraded", Message: "HTTPS configured but local certificate is missing"}
	}
	if _, err := os.Stat(key); err != nil {
		return RuntimePart{Status: "degraded", Message: "HTTPS configured but local key is missing"}
	}
	return RuntimePart{Status: "ok", Active: true, Message: "HTTPS API available on 8443"}
}

func SetStatus(status Status) {
	status.UpdatedAt = time.Now().UTC()
	statusMu.Lock()
	currentStatus = status
	statusMu.Unlock()
}

func CurrentStatus() Status {
	statusMu.RLock()
	defer statusMu.RUnlock()
	return currentStatus
}

func WriteStatus(path string, status Status) error {
	if strings.TrimSpace(path) == "" {
		path = DefaultStatusPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(status)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func LoadStatus(path string) (Status, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultStatusPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Status{}, os.ErrNotExist
	}
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, err
	}
	return status, nil
}
