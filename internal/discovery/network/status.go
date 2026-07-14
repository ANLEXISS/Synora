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
	Enabled    bool        `json:"enabled"`
	Status     string      `json:"status"`
	ActiveBand string      `json:"active_band,omitempty"`
	Message    string      `json:"message,omitempty"`
	SynoraNet  RuntimePart `json:"synoranet"`
	AP5GHz     RuntimePart `json:"ap_5ghz"`
	AP2GHz     RuntimePart `json:"ap_2ghz"`
	DHCP       RuntimePart `json:"dhcp"`
	DNS        RuntimePart `json:"dns"`
	HTTPSAPI   RuntimePart `json:"https_api"`
	MediaMTX   RuntimePart `json:"mediamtx_rtsp"`
	UpdatedAt  time.Time   `json:"updated_at"`
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
