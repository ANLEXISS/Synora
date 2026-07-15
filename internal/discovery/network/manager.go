package network

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
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
	devicePath := os.Getenv("SYNORA_DEVICE")
	if strings.TrimSpace(devicePath) == "" {
		devicePath = DefaultDeviceConfigPath
	}
	pairingState, _ := LoadPairingState("")
	stations, stationErr := WriteAllowedStations(cfg.SynoraNet, devicePath)
	if stationErr != nil && cfg.SynoraNet.AccessControl.Enabled {
		failures = append(failures, fmt.Errorf("station allowlist failed: %w", stationErr))
	}
	status.Visibility = VisibilityStatus{RuntimePart: RuntimePart{Status: "ok", Active: true, Message: "SSID hidden outside pairing window"}, Hidden: !pairingState.Active || !cfg.SynoraNet.Visibility.VisibleDuringPairing, PairingVisible: pairingState.Active && cfg.SynoraNet.Visibility.VisibleDuringPairing}
	accessStatus := "ok"
	accessMessage := fmt.Sprintf("%d authorized camera stations", len(stations))
	if stationErr != nil {
		accessStatus, accessMessage = "unavailable", "station allowlist unavailable"
	} else if HasStationSecurityWarning(stations) {
		accessStatus, accessMessage = "degraded", fmt.Sprintf("%d authorized camera stations; MAC security warning present", len(stations))
	}
	if len(stations) == 0 && !pairingState.Active {
		accessMessage = "locked: no authorized stations"
	}
	status.AccessControl = AccessControlStatus{RuntimePart: RuntimePart{Status: accessStatus, Active: stationErr == nil, Message: accessMessage}, Enabled: cfg.SynoraNet.AccessControl.Enabled, StationAllowlist: cfg.SynoraNet.AccessControl.StationAllowlist, KnownDevices: len(stations), PendingDevices: pairingState.PendingDevices, UnknownPolicy: cfg.SynoraNet.AccessControl.UnknownStationPolicy}
	policyStatus := "ok"
	policyMessage := "central-initiated camera connections"
	if cfg.SynoraNet.ConnectionPolicy.Mode == "camera_push_legacy" {
		policyStatus, policyMessage = "degraded", "legacy camera push enabled"
	}
	status.ConnectionPolicy = ConnectionPolicyStatus{RuntimePart: RuntimePart{Status: policyStatus, Active: true, Message: policyMessage}, Mode: cfg.SynoraNet.ConnectionPolicy.Mode, PairingWindowActive: pairingState.Active, CameraPushRuntimeAllowed: cfg.SynoraNet.ConnectionPolicy.AllowCameraPushRuntime}
	status.PairingSecurity = PairingSecurityStatus{RuntimePart: RuntimePart{Status: "ok", Active: pairingState.Active, Message: map[bool]string{true: "pairing window active", false: "pairing window closed"}[pairingState.Active]}, Active: pairingState.Active, ExpiresAt: pairingState.ExpiresAt, ClaimEndpointActive: pairingState.Active && cfg.SynoraNet.Pairing.ClaimEndpointEnabledOnlyDuringWindow, MaxPendingDevices: cfg.SynoraNet.Pairing.MaxPendingDevices}
	if err := ensureBridge(cfg.SynoraNet); err != nil {
		failures = append(failures, fmt.Errorf("bridge init failed: %w", err))
		status.Message = err.Error()
		log.Printf("network component degraded component=bridge err=%v", err)
	} else {
		log.Println("network component ready component=bridge gateway=10.77.0.1")
	}
	firewallErr := EnsureFirewallState(cfg.SynoraNet, pairingState.Active)
	if !cfg.SynoraNet.Firewall.Enabled {
		status.Firewall = RuntimePart{Status: "degraded", Message: "firewall disabled by configuration"}
		status.NetworkIsolation = RuntimePart{Status: "degraded", Message: "network isolation unavailable while firewall is disabled"}
	} else if firewallErr != nil {
		failures = append(failures, firewallErr)
		status.Firewall = RuntimePart{Status: "unavailable", Message: "unable to apply SynoraNet firewall"}
		status.NetworkIsolation = RuntimePart{Status: "unavailable", Message: "firewall application failed; AP start blocked"}
		log.Printf("network component degraded component=firewall err=%v", firewallErr)
	} else {
		status.Firewall = RuntimePart{Status: "ok", Active: true, Message: "SynoraNet nftables/iptables isolation active"}
		status.NetworkIsolation = RuntimePart{Status: "ok", Active: true, Message: "LAN, Tailscale, Internet and client forwarding blocked"}
	}
	if firewallErr != nil || stationErr != nil {
		status.AP5GHz = RuntimePart{Status: "unavailable", Message: "AP start blocked until firewall is active"}
		status.AP2GHz = RuntimePart{Status: "unavailable", Message: "AP start blocked until firewall is active"}
		status.DHCP = RuntimePart{Status: "unavailable", Message: "dnsmasq not started while firewall is unavailable"}
		status.DNS = status.DHCP
	} else {
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
		if err := ensureDnsmasqState(cfg.SynoraNet, pairingState.Active, stations); err != nil {
			failures = append(failures, err)
			status.DHCP = RuntimePart{Status: "unavailable", Message: "dnsmasq failed"}
			status.DNS = RuntimePart{Status: "unavailable", Message: "dnsmasq failed"}
			log.Printf("network component degraded component=dnsmasq err=%v", err)
		} else {
			status.DHCP = RuntimePart{Status: "ok", Active: true, Message: "dnsmasq active"}
			status.DNS = RuntimePart{Status: "ok", Active: true, Message: "local DNS active"}
			log.Println("network component ready component=dnsmasq")
		}
	}
	status.WifiSecurity = wifiSecurityStatus(cfg.SynoraNet, status.SynoraNet.Active)
	if status.SynoraNet.Active {
		status.Status = "ok"
		if status.ActiveBand == "2.4GHz" {
			status.Status = "degraded"
		}
	}
	if status.Firewall.Status != "ok" || status.NetworkIsolation.Status != "ok" || status.WifiSecurity.Status != "ok" {
		status.Status = "degraded"
	}
	status.HTTPSAPI = localHTTPSStatus()
	status.MediaMTX = RuntimePart{Status: "ok", Active: true, Message: "RTSP expected on 8554"}
	m.setStatus(status)
	if writeErr := WriteStatus(statusPath(), status); writeErr != nil {
		failures = append(failures, writeErr)
	}
	log.Printf("SynoraNet status=%s band=%s", status.Status, status.ActiveBand)
	go m.watchPairing(cfg.SynoraNet, devicePath)
	return errors.Join(failures...)
}

func (m *Manager) watchPairing(cfg SynoraNetConfig, devicePath string) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	lastKey := ""
	for range ticker.C {
		state, err := LoadPairingState("")
		if err != nil {
			continue
		}
		stations, stationErr := WriteAllowedStations(cfg, devicePath)
		if stationErr != nil && cfg.AccessControl.Enabled {
			continue
		}
		allowlistData, _ := os.ReadFile(cfg.AccessControl.MACAllowlistFile)
		key := fmt.Sprintf("%t|%s|%s|%d", state.Active, state.ExpiresAt.UTC().Format(time.RFC3339), string(allowlistData), len(stations))
		if key == lastKey {
			continue
		}
		lastKey = key
		passphrase, passErr := EnsurePassphrase(cfg.AP.PassphraseFile)
		if passErr != nil {
			continue
		}
		if err := writeHostapdConfigForCurrentBand(cfg, passphrase, state.Active); err != nil {
			continue
		}
		hostapdErr := reloadHostapd(cfg.Interface)
		firewallErr := EnsureFirewallState(cfg, state.Active)
		dnsmasqErr := reloadDnsmasqState(cfg, state.Active, stations)
		status := m.Status()
		status.Visibility = VisibilityStatus{RuntimePart: RuntimePart{Status: "ok", Active: true, Message: "SSID hidden outside pairing window"}, Hidden: !state.Active || !cfg.Visibility.VisibleDuringPairing, PairingVisible: state.Active && cfg.Visibility.VisibleDuringPairing}
		status.AccessControl.PendingDevices = state.PendingDevices
		status.AccessControl.KnownDevices = len(stations)
		if HasStationSecurityWarning(stations) {
			status.AccessControl.Status = "degraded"
			status.AccessControl.Message = "MAC security warning present"
		}
		status.ConnectionPolicy.PairingWindowActive = state.Active
		status.PairingSecurity = PairingSecurityStatus{RuntimePart: RuntimePart{Status: "ok", Active: state.Active, Message: map[bool]string{true: "pairing window active", false: "pairing window closed"}[state.Active]}, Active: state.Active, ExpiresAt: state.ExpiresAt, ClaimEndpointActive: state.Active && cfg.Pairing.ClaimEndpointEnabledOnlyDuringWindow, MaxPendingDevices: cfg.Pairing.MaxPendingDevices}
		if firewallErr != nil {
			status.Firewall = RuntimePart{Status: "unavailable", Message: "unable to apply SynoraNet firewall"}
			status.NetworkIsolation = RuntimePart{Status: "unavailable", Message: "firewall application failed"}
		}
		if hostapdErr != nil || dnsmasqErr != nil {
			status.Status = "degraded"
		}
		m.setStatus(status)
		_ = WriteStatus(statusPath(), status)
		log.Printf("SynoraNet pairing policy applied active=%t visibility=%t authorized_stations=%d", state.Active, state.SSIDVisible, len(stations))
	}
}

func writeHostapdConfigForCurrentBand(cfg SynoraNetConfig, passphrase string, pairing bool) error {
	path := Hostapd5GHzConfigPath
	band := "5GHz"
	if status := CurrentStatus(); status.ActiveBand == "2.4GHz" {
		path, band = Hostapd2GHzConfigPath, "2.4GHz"
	}
	return writeHostapdConfig(path, renderHostapdConfigState(cfg, band, passphrase, pairing, nil))
}

func reloadHostapd(iface string) error {
	return execCommand("hostapd_cli", "-p", DefaultRunDir+"/hostapd", "-i", iface, "reload")
}

func execCommand(name string, args ...string) error {
	return runCommand(name, args...)
}

var runCommand = func(name string, args ...string) error { return exec.Command(name, args...).Run() }

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
