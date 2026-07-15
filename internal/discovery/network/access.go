package network

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"synora/internal/device"
	"synora/pkg/contract"
)

const DefaultDeviceConfigPath = "/etc/synora/devices.yaml"

type KnownStation struct {
	DeviceID  string
	MAC       string
	StaticIP  string
	AllowWiFi bool
	Trust     string
}

func knownStations(path string) ([]KnownStation, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultDeviceConfigPath
	}
	items, err := device.Load(path)
	if err != nil {
		return nil, err
	}
	out := make([]KnownStation, 0, len(items))
	for _, item := range items {
		if item.Type != contract.DeviceTypeCamera || !item.Enabled || item.DeletedAt != nil {
			continue
		}
		mac := normalizeMAC(stringValue(item.Network, "mac"))
		if mac == "" {
			continue
		}
		allow := boolValue(item.Network, "allow_wifi", true)
		trust := stringValue(item.Network, "network_trust")
		if trust == "" {
			trust = "paired"
		}
		if trust != "paired" {
			allow = false
		}
		out = append(out, KnownStation{DeviceID: item.ID, MAC: mac, StaticIP: firstNonEmpty(stringValue(item.Network, "static_ip"), stringValue(item.Network, "ip")), AllowWiFi: allow, Trust: trust})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MAC < out[j].MAC })
	return out, nil
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
func boolValue(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	if value, ok := values[key].(bool); ok {
		return value
	}
	if value, ok := values[key].(string); ok {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func RenderAllowedStations(stations []KnownStation) string {
	macs := make([]string, 0, len(stations))
	for _, station := range stations {
		if station.AllowWiFi && station.Trust == "paired" && station.MAC != "" {
			macs = append(macs, station.MAC)
		}
	}
	sort.Strings(macs)
	return strings.Join(macs, "\n") + map[bool]string{true: "\n", false: ""}[len(macs) > 0]
}

func WriteAllowedStations(cfg SynoraNetConfig, devicePath string) ([]KnownStation, error) {
	stations, err := knownStations(devicePath)
	if err != nil {
		return nil, err
	}
	if !cfg.AccessControl.Enabled || !cfg.AccessControl.StationAllowlist {
		stations = nil
	}
	path := cfg.AccessControl.MACAllowlistFile
	if strings.TrimSpace(path) == "" {
		path = DefaultRunDir + "/hostapd-allowed-stations"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(RenderAllowedStations(stations)), 0600); err != nil {
		return nil, fmt.Errorf("write station allowlist: %w", err)
	}
	_ = os.Chmod(path, 0600)
	return stations, nil
}

func KnownStationCount(devicePath string) int {
	stations, err := knownStations(devicePath)
	if err != nil {
		return 0
	}
	count := 0
	for _, station := range stations {
		if station.AllowWiFi && station.Trust == "paired" {
			count++
		}
	}
	return count
}

func HasStationSecurityWarning(stations []KnownStation) bool {
	for _, station := range stations {
		if station.Trust != "paired" {
			return true
		}
	}
	return false
}
