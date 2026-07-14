package network

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

type Manager struct {
	mu     sync.RWMutex
	status Status
}

func NewManager() *Manager {
	return &Manager{status: CurrentStatus()}
}

func (m *Manager) Start() error {
	log.Println("network manager starting")
	path := os.Getenv("SYNORA_NETWORK_CONFIG")
	cfg, err := LoadConfig(path)
	if err != nil {
		m.setStatus(Status{Status: "unavailable", Message: "network config invalid: " + err.Error(), SynoraNet: RuntimePart{Status: "unavailable", Message: "network config invalid"}})
		return fmt.Errorf("load network config: %w", err)
	}
	if !cfg.SynoraNet.Enabled {
		status := Status{Enabled: false, Status: "disabled", Message: "SynoraNet disabled; Discovery continues", SynoraNet: RuntimePart{Status: "disabled", Active: false, Message: "SynoraNet disabled"}}
		m.setStatus(status)
		_ = WriteStatus(statusPath(), status)
		log.Println("SynoraNet disabled; network setup skipped")
		return nil
	}

	status := Status{Enabled: true, Status: "degraded", SynoraNet: RuntimePart{Status: "degraded", Active: false}}
	var failures []error
	if err := ensureBridge(cfg.SynoraNet); err != nil {
		failures = append(failures, fmt.Errorf("bridge init failed: %w", err))
		status.Message = err.Error()
		log.Printf("network component degraded component=bridge err=%v", err)
	} else {
		log.Println("network component ready component=bridge gateway=10.77.0.1")
	}
	ap, apErr := startAP(cfg.SynoraNet)
	status.AP5GHz, status.AP2GHz = ap.AP5GHz, ap.AP2GHz
	status.ActiveBand = ap.ActiveBand
	if apErr != nil {
		failures = append(failures, apErr)
		status.SynoraNet = RuntimePart{Status: "unavailable", Message: "hostapd failed"}
		log.Printf("network component degraded component=hostapd err=%v", apErr)
	} else {
		status.SynoraNet = RuntimePart{Status: "ok", Active: true, Message: ap.AP5GHz.Message}
		if ap.ActiveBand == "2.4GHz" {
			status.SynoraNet = RuntimePart{Status: "degraded", Active: true, Message: "5 GHz failed, running 2.4 GHz fallback"}
		}
		log.Printf("network component ready component=hostapd band=%s", ap.ActiveBand)
	}
	if err := ensureDnsmasq(cfg.SynoraNet); err != nil {
		failures = append(failures, err)
		status.DHCP = RuntimePart{Status: "unavailable", Message: "dnsmasq failed"}
		status.DNS = RuntimePart{Status: "unavailable", Message: "dnsmasq failed"}
		log.Printf("network component degraded component=dnsmasq err=%v", err)
	} else {
		status.DHCP = RuntimePart{Status: "ok", Active: true, Message: "dnsmasq active"}
		status.DNS = RuntimePart{Status: "ok", Active: true, Message: "local DNS active"}
		log.Println("network component ready component=dnsmasq")
	}
	if status.SynoraNet.Active {
		status.Status = "ok"
		if status.ActiveBand == "2.4GHz" {
			status.Status = "degraded"
		}
	}
	status.HTTPSAPI = localHTTPSStatus()
	status.MediaMTX = RuntimePart{Status: "ok", Active: true, Message: "RTSP expected on 8554"}
	m.setStatus(status)
	if writeErr := WriteStatus(statusPath(), status); writeErr != nil {
		failures = append(failures, writeErr)
	}
	log.Printf("SynoraNet status=%s band=%s", status.Status, status.ActiveBand)
	return errors.Join(failures...)
}

func (m *Manager) setStatus(status Status) {
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	SetStatus(status)
}
func (m *Manager) Status() Status { m.mu.RLock(); defer m.mu.RUnlock(); return m.status }
func statusPath() string {
	if value := os.Getenv("SYNORA_NETWORK_STATUS_FILE"); value != "" {
		return value
	}
	return DefaultStatusPath
}
