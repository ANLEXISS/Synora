package network

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultConfigPath = "/etc/synora/network.yaml"
	DefaultRunDir     = "/run/synora"
	DefaultStatusPath = DefaultRunDir + "/network-status.json"
	DefaultPSKPath    = "/etc/synora/secrets/synoranet_psk"
	DefaultSSID       = "SynoraNet"
	DefaultSubnet     = "10.77.0.0/24"
	DefaultGateway    = "10.77.0.1"
	DefaultDHCPStart  = "10.77.0.50"
	DefaultDHCPEnd    = "10.77.0.200"
	DefaultCountry    = "FR"
	DefaultInterface  = "wlan0"

	Hostapd5GHzConfigPath = DefaultRunDir + "/hostapd-5ghz.conf"
	Hostapd2GHzConfigPath = DefaultRunDir + "/hostapd-2ghz.conf"
	HostapdConfigPath     = DefaultRunDir + "/hostapd.conf"
	DnsmasqConfigPath     = DefaultRunDir + "/dnsmasq-synoranet.conf"
)

// SynoraNetConfig is deliberately independent from the security and device
// configs. It can therefore be disabled without affecting Core or the API.
type SynoraNetConfig struct {
	Enabled          bool                   `yaml:"enabled"`
	Interface        string                 `yaml:"interface"`
	SSID             string                 `yaml:"ssid"`
	CountryCode      string                 `yaml:"country_code"`
	SubnetCIDR       string                 `yaml:"subnet_cidr"`
	GatewayIP        string                 `yaml:"gateway_ip"`
	DHCPStart        string                 `yaml:"dhcp_start"`
	DHCPEnd          string                 `yaml:"dhcp_end"`
	DHCLeaseTime     string                 `yaml:"dhcp_lease_time"`
	DNS              DNSConfig              `yaml:"dns"`
	Security         SecurityConfig         `yaml:"security"`
	Visibility       VisibilityConfig       `yaml:"visibility"`
	AccessControl    AccessControlConfig    `yaml:"access_control"`
	ConnectionPolicy ConnectionPolicyConfig `yaml:"connection_policy"`
	Pairing          PairingConfig          `yaml:"pairing"`
	AP               APConfig               `yaml:"ap"`
	Firewall         FirewallConfig         `yaml:"firewall"`
	Services         ServiceURLs            `yaml:"services"`
}

type SecurityConfig struct {
	Mode                string `yaml:"mode"`
	PMF                 string `yaml:"pmf"`
	APIsolate           bool   `yaml:"ap_isolate"`
	MinPassphraseLength int    `yaml:"min_passphrase_length"`
	AllowLegacyWPA2     bool   `yaml:"allow_legacy_wpa2"`
	AllowTransitionMode bool   `yaml:"allow_transition_mode"`
}

type VisibilityConfig struct {
	HiddenByDefault                 bool `yaml:"hidden_by_default"`
	VisibleDuringPairing            bool `yaml:"visible_during_pairing"`
	PairingVisibilityTimeoutSeconds int  `yaml:"pairing_visibility_timeout_seconds"`
}

type AccessControlConfig struct {
	Enabled              bool   `yaml:"enabled"`
	StationAllowlist     bool   `yaml:"station_allowlist"`
	UnknownStationPolicy string `yaml:"unknown_station_policy"`
	MACAllowlistFile     string `yaml:"mac_allowlist_file"`
	KnownDevicesOnly     bool   `yaml:"known_devices_only"`
	BindDHCPToKnown      bool   `yaml:"bind_dhcp_to_known_devices"`
}

type ConnectionPolicyConfig struct {
	Mode                    string `yaml:"mode"`
	AllowCameraPushPairing  bool   `yaml:"allow_camera_push_during_pairing"`
	AllowCameraPushRuntime  bool   `yaml:"allow_camera_push_runtime"`
	AllowEstablishedRelated bool   `yaml:"allow_established_related"`
}

type PairingConfig struct {
	Enabled                              bool `yaml:"enabled"`
	WindowSeconds                        int  `yaml:"window_seconds"`
	RequireAdminSession                  bool `yaml:"require_admin_session"`
	RequireSetupToken                    bool `yaml:"require_setup_token"`
	RequireDevicePublicKey               bool `yaml:"require_device_public_key"`
	RequireObservedMACMatch              bool `yaml:"require_observed_mac_match"`
	MaxPendingDevices                    int  `yaml:"max_pending_devices"`
	ClaimEndpointEnabledOnlyDuringWindow bool `yaml:"claim_endpoint_enabled_only_during_window"`
}

type FirewallConfig struct {
	Enabled                       bool   `yaml:"enabled"`
	DefaultPolicy                 string `yaml:"default_policy"`
	BlockClientToClient           bool   `yaml:"block_client_to_client"`
	BlockForwardToLAN             bool   `yaml:"block_forward_to_lan"`
	BlockForwardToTailscale       bool   `yaml:"block_forward_to_tailscale"`
	BlockForwardToInternet        bool   `yaml:"block_forward_to_internet"`
	AllowDNS                      bool   `yaml:"allow_dns"`
	AllowDHCP                     bool   `yaml:"allow_dhcp"`
	AllowNTPLocal                 bool   `yaml:"allow_ntp_local"`
	AllowAPIHTTPFromClients       bool   `yaml:"allow_api_http_from_clients"`
	AllowAPIHTTPSFromClients      bool   `yaml:"allow_api_https_from_clients"`
	AllowVisionIngressFromClients bool   `yaml:"allow_vision_ingress_from_clients"`
	AllowMediaRTSPFromClients     bool   `yaml:"allow_media_rtsp_from_clients"`
	AllowMediaWebRTCFromClients   bool   `yaml:"allow_media_webrtc_from_clients"`
	AllowMediaHLSFromClients      bool   `yaml:"allow_media_hls_from_clients"`
	PairingAllowedPorts           []int  `yaml:"pairing_allowed_ports"`
	CentralToCameraAllowedPorts   []int  `yaml:"central_to_camera_allowed_ports"`
	// Deprecated aliases retained for old configurations.
	AllowAPIHTTP       bool `yaml:"allow_api_http"`
	AllowAPIHTTPS      bool `yaml:"allow_api_https"`
	AllowVisionIngress bool `yaml:"allow_vision_ingress"`
	AllowMediaRTSP     bool `yaml:"allow_media_rtsp"`
	AllowMediaWebRTC   bool `yaml:"allow_media_webrtc"`
	AllowMediaHLS      bool `yaml:"allow_media_hls"`
	AllowMediaMTXAPI   bool `yaml:"allow_mediamtx_api"`
}

type DNSConfig struct {
	Enabled bool              `yaml:"enabled"`
	Names   map[string]string `yaml:"names"`
}

type APConfig struct {
	PreferredBand  string `yaml:"preferred_band"`
	FallbackBand   string `yaml:"fallback_band"`
	Mode           string `yaml:"mode"`
	Channel5GHz    int    `yaml:"channel_5ghz"`
	Channel2GHz    int    `yaml:"channel_2ghz"`
	Width5GHz      int    `yaml:"width_5ghz"`
	Hidden         bool   `yaml:"hidden"`
	WPA            string `yaml:"wpa"`
	PassphraseFile string `yaml:"passphrase_file"`
}

type ServiceURLs struct {
	HTTPAPIURL       string `yaml:"http_api_url"`
	HTTPSAPIURL      string `yaml:"https_api_url"`
	VisionIngressURL string `yaml:"vision_ingress_url"`
	RTSPURL          string `yaml:"rtsp_url"`
	WebRTCBaseURL    string `yaml:"webrtc_base_url"`
	HLSBaseURL       string `yaml:"hls_base_url"`
}

type NetworkConfig struct {
	SynoraNet SynoraNetConfig `yaml:"synoranet"`
}

func DefaultConfig() NetworkConfig {
	return NetworkConfig{SynoraNet: SynoraNetConfig{
		Enabled: false, Interface: "", SSID: DefaultSSID, CountryCode: DefaultCountry,
		SubnetCIDR: DefaultSubnet, GatewayIP: DefaultGateway, DHCPStart: DefaultDHCPStart,
		DHCPEnd: DefaultDHCPEnd, DHCLeaseTime: "12h",
		DNS:              DNSConfig{Enabled: true, Names: defaultDNSNames()},
		Security:         SecurityConfig{Mode: "wpa3", PMF: "required", APIsolate: true, MinPassphraseLength: 24},
		Visibility:       VisibilityConfig{HiddenByDefault: true, VisibleDuringPairing: true, PairingVisibilityTimeoutSeconds: 600},
		AccessControl:    AccessControlConfig{Enabled: true, StationAllowlist: true, UnknownStationPolicy: "reject", MACAllowlistFile: DefaultRunDir + "/hostapd-allowed-stations", KnownDevicesOnly: true, BindDHCPToKnown: true},
		ConnectionPolicy: ConnectionPolicyConfig{Mode: "central_initiated", AllowCameraPushPairing: true, AllowCameraPushRuntime: false, AllowEstablishedRelated: true},
		Pairing:          PairingConfig{Enabled: true, WindowSeconds: 600, RequireAdminSession: true, RequireSetupToken: true, RequireObservedMACMatch: true, MaxPendingDevices: 5, ClaimEndpointEnabledOnlyDuringWindow: true},
		AP:               APConfig{PreferredBand: "5GHz", FallbackBand: "2.4GHz", Mode: "fallback", Channel5GHz: 36, Channel2GHz: 6, Width5GHz: 20, PassphraseFile: DefaultPSKPath},
		Firewall:         FirewallConfig{Enabled: true, DefaultPolicy: "deny", BlockClientToClient: true, BlockForwardToLAN: true, BlockForwardToTailscale: true, BlockForwardToInternet: true, AllowDNS: true, AllowDHCP: true, PairingAllowedPorts: []int{53, 67, 68, 8080, 8443, 7070}, CentralToCameraAllowedPorts: []int{443, 7443, 8554, 8889}},
		Services:         ServiceURLs{HTTPAPIURL: "http://10.77.0.1:8080", HTTPSAPIURL: "https://10.77.0.1:8443", VisionIngressURL: "http://10.77.0.1:7070", RTSPURL: "rtsp://10.77.0.1:8554", WebRTCBaseURL: "http://10.77.0.1:8889", HLSBaseURL: "http://10.77.0.1:8888"},
	}}
}

func defaultDNSNames() map[string]string {
	return map[string]string{
		"synora.local": "10.77.0.1", "hub.synora.local": "10.77.0.1",
		"api.synora.local": "10.77.0.1", "rtsp.synora.local": "10.77.0.1",
	}
}

func LoadConfig(path string) (NetworkConfig, error) {
	cfg := NetworkConfig{}
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse SynoraNet config: %w", err)
	}
	cfg.SynoraNet = normalizeConfig(cfg.SynoraNet)
	// A missing section is different from an explicit development override.
	// Keep explicit false values intact while making old configs secure when
	// they have not yet gained the new sections.
	var raw struct {
		SynoraNet struct {
			Security         *yaml.Node `yaml:"security"`
			Visibility       *yaml.Node `yaml:"visibility"`
			AccessControl    *yaml.Node `yaml:"access_control"`
			ConnectionPolicy *yaml.Node `yaml:"connection_policy"`
			Pairing          *yaml.Node `yaml:"pairing"`
			Firewall         *yaml.Node `yaml:"firewall"`
			AP               *yaml.Node `yaml:"ap"`
		} `yaml:"synoranet"`
	}
	if err := yaml.Unmarshal(data, &raw); err == nil {
		defaults := DefaultConfig().SynoraNet
		if raw.SynoraNet.Security == nil || !yamlNodeHasKey(raw.SynoraNet.Security, "ap_isolate") {
			cfg.SynoraNet.Security.APIsolate = defaults.Security.APIsolate
		}
		if raw.SynoraNet.Visibility == nil {
			cfg.SynoraNet.Visibility = defaults.Visibility
			if raw.SynoraNet.AP != nil && yamlNodeHasKey(raw.SynoraNet.AP, "hidden") {
				cfg.SynoraNet.Visibility.HiddenByDefault = cfg.SynoraNet.AP.Hidden
			}
		} else {
			hidden := defaults.Visibility.HiddenByDefault
			if raw.SynoraNet.AP != nil && yamlNodeHasKey(raw.SynoraNet.AP, "hidden") {
				hidden = cfg.SynoraNet.AP.Hidden
			}
			setVisibilityDefaults(&cfg.SynoraNet.Visibility, defaults.Visibility, raw.SynoraNet.Visibility, hidden)
		}
		if raw.SynoraNet.AccessControl == nil {
			cfg.SynoraNet.AccessControl = defaults.AccessControl
		} else {
			setAccessControlDefaults(&cfg.SynoraNet.AccessControl, defaults.AccessControl, raw.SynoraNet.AccessControl)
		}
		if raw.SynoraNet.ConnectionPolicy == nil {
			cfg.SynoraNet.ConnectionPolicy = defaults.ConnectionPolicy
			// Old configs explicitly allowed camera push through the legacy
			// firewall fields. Keep them working, but surface degraded health.
			if cfg.SynoraNet.Firewall.AllowVisionIngress || cfg.SynoraNet.Firewall.AllowAPIHTTP || cfg.SynoraNet.Firewall.AllowAPIHTTPS {
				cfg.SynoraNet.ConnectionPolicy.Mode = "camera_push_legacy"
				cfg.SynoraNet.ConnectionPolicy.AllowCameraPushRuntime = true
			}
		} else {
			setConnectionPolicyDefaults(&cfg.SynoraNet.ConnectionPolicy, defaults.ConnectionPolicy, raw.SynoraNet.ConnectionPolicy)
		}
		if raw.SynoraNet.Pairing == nil {
			cfg.SynoraNet.Pairing = defaults.Pairing
		} else {
			setPairingDefaults(&cfg.SynoraNet.Pairing, defaults.Pairing, raw.SynoraNet.Pairing)
		}
		if raw.SynoraNet.Firewall == nil {
			cfg.SynoraNet.Firewall = defaults.Firewall
		} else {
			setFirewallDefaults(&cfg.SynoraNet.Firewall, defaults.Firewall, raw.SynoraNet.Firewall)
		}
	}
	return cfg, ValidateConfig(cfg.SynoraNet)
}

func yamlNodeHasKey(node *yaml.Node, key string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return true
		}
	}
	return false
}

func setFirewallDefaults(cfg *FirewallConfig, defaults FirewallConfig, node *yaml.Node) {
	if !yamlNodeHasKey(node, "enabled") {
		cfg.Enabled = defaults.Enabled
	}
	if !yamlNodeHasKey(node, "block_client_to_client") {
		cfg.BlockClientToClient = defaults.BlockClientToClient
	}
	if !yamlNodeHasKey(node, "block_forward_to_lan") {
		cfg.BlockForwardToLAN = defaults.BlockForwardToLAN
	}
	if !yamlNodeHasKey(node, "block_forward_to_tailscale") {
		cfg.BlockForwardToTailscale = defaults.BlockForwardToTailscale
	}
	if !yamlNodeHasKey(node, "block_forward_to_internet") {
		cfg.BlockForwardToInternet = defaults.BlockForwardToInternet
	}
	if !yamlNodeHasKey(node, "allow_dns") {
		cfg.AllowDNS = defaults.AllowDNS
	}
	if !yamlNodeHasKey(node, "allow_dhcp") {
		cfg.AllowDHCP = defaults.AllowDHCP
	}
	if !yamlNodeHasKey(node, "allow_ntp_local") {
		cfg.AllowNTPLocal = defaults.AllowNTPLocal
	}
	if !yamlNodeHasKey(node, "allow_api_http_from_clients") {
		cfg.AllowAPIHTTPFromClients = defaults.AllowAPIHTTPFromClients
	}
	if !yamlNodeHasKey(node, "allow_api_https_from_clients") {
		cfg.AllowAPIHTTPSFromClients = defaults.AllowAPIHTTPSFromClients
	}
	if !yamlNodeHasKey(node, "allow_vision_ingress_from_clients") {
		cfg.AllowVisionIngressFromClients = defaults.AllowVisionIngressFromClients
	}
	if !yamlNodeHasKey(node, "allow_media_rtsp_from_clients") {
		cfg.AllowMediaRTSPFromClients = defaults.AllowMediaRTSPFromClients
	}
	if !yamlNodeHasKey(node, "allow_media_webrtc_from_clients") {
		cfg.AllowMediaWebRTCFromClients = defaults.AllowMediaWebRTCFromClients
	}
	if !yamlNodeHasKey(node, "allow_media_hls_from_clients") {
		cfg.AllowMediaHLSFromClients = defaults.AllowMediaHLSFromClients
	}
	if !yamlNodeHasKey(node, "allow_api_http") {
		cfg.AllowAPIHTTP = defaults.AllowAPIHTTP
	}
	if !yamlNodeHasKey(node, "allow_api_https") {
		cfg.AllowAPIHTTPS = defaults.AllowAPIHTTPS
	}
	if !yamlNodeHasKey(node, "allow_vision_ingress") {
		cfg.AllowVisionIngress = defaults.AllowVisionIngress
	}
	if !yamlNodeHasKey(node, "allow_media_rtsp") {
		cfg.AllowMediaRTSP = defaults.AllowMediaRTSP
	}
	if !yamlNodeHasKey(node, "allow_media_webrtc") {
		cfg.AllowMediaWebRTC = defaults.AllowMediaWebRTC
	}
	if !yamlNodeHasKey(node, "allow_media_hls") {
		cfg.AllowMediaHLS = defaults.AllowMediaHLS
	}
	if !yamlNodeHasKey(node, "allow_mediamtx_api") {
		cfg.AllowMediaMTXAPI = defaults.AllowMediaMTXAPI
	}
	// Preserve explicit legacy client-push permissions while the connection
	// policy reports the configuration as camera_push_legacy/degraded.
	if cfg.AllowAPIHTTP {
		cfg.AllowAPIHTTPFromClients = true
	}
	if cfg.AllowAPIHTTPS {
		cfg.AllowAPIHTTPSFromClients = true
	}
	if cfg.AllowVisionIngress {
		cfg.AllowVisionIngressFromClients = true
	}
	if cfg.AllowMediaRTSP {
		cfg.AllowMediaRTSPFromClients = true
	}
	if cfg.AllowMediaWebRTC {
		cfg.AllowMediaWebRTCFromClients = true
	}
	if cfg.AllowMediaHLS {
		cfg.AllowMediaHLSFromClients = true
	}
}

func setVisibilityDefaults(cfg *VisibilityConfig, defaults VisibilityConfig, node *yaml.Node, legacyHidden bool) {
	if !yamlNodeHasKey(node, "hidden_by_default") {
		cfg.HiddenByDefault = legacyHidden
	}
	if !yamlNodeHasKey(node, "visible_during_pairing") {
		cfg.VisibleDuringPairing = defaults.VisibleDuringPairing
	}
	if !yamlNodeHasKey(node, "pairing_visibility_timeout_seconds") {
		cfg.PairingVisibilityTimeoutSeconds = defaults.PairingVisibilityTimeoutSeconds
	}
}

func setAccessControlDefaults(cfg *AccessControlConfig, defaults AccessControlConfig, node *yaml.Node) {
	if !yamlNodeHasKey(node, "enabled") {
		cfg.Enabled = defaults.Enabled
	}
	if !yamlNodeHasKey(node, "station_allowlist") {
		cfg.StationAllowlist = defaults.StationAllowlist
	}
	if !yamlNodeHasKey(node, "unknown_station_policy") {
		cfg.UnknownStationPolicy = defaults.UnknownStationPolicy
	}
	if !yamlNodeHasKey(node, "mac_allowlist_file") {
		cfg.MACAllowlistFile = defaults.MACAllowlistFile
	}
	if !yamlNodeHasKey(node, "known_devices_only") {
		cfg.KnownDevicesOnly = defaults.KnownDevicesOnly
	}
	if !yamlNodeHasKey(node, "bind_dhcp_to_known_devices") {
		cfg.BindDHCPToKnown = defaults.BindDHCPToKnown
	}
}

func setConnectionPolicyDefaults(cfg *ConnectionPolicyConfig, defaults ConnectionPolicyConfig, node *yaml.Node) {
	if !yamlNodeHasKey(node, "mode") {
		cfg.Mode = defaults.Mode
	}
	if !yamlNodeHasKey(node, "allow_camera_push_during_pairing") {
		cfg.AllowCameraPushPairing = defaults.AllowCameraPushPairing
	}
	if !yamlNodeHasKey(node, "allow_camera_push_runtime") {
		cfg.AllowCameraPushRuntime = defaults.AllowCameraPushRuntime
	}
	if !yamlNodeHasKey(node, "allow_established_related") {
		cfg.AllowEstablishedRelated = defaults.AllowEstablishedRelated
	}
}

func setPairingDefaults(cfg *PairingConfig, defaults PairingConfig, node *yaml.Node) {
	if !yamlNodeHasKey(node, "enabled") {
		cfg.Enabled = defaults.Enabled
	}
	if !yamlNodeHasKey(node, "window_seconds") {
		cfg.WindowSeconds = defaults.WindowSeconds
	}
	if !yamlNodeHasKey(node, "require_admin_session") {
		cfg.RequireAdminSession = defaults.RequireAdminSession
	}
	if !yamlNodeHasKey(node, "require_setup_token") {
		cfg.RequireSetupToken = defaults.RequireSetupToken
	}
	if !yamlNodeHasKey(node, "require_device_public_key") {
		cfg.RequireDevicePublicKey = defaults.RequireDevicePublicKey
	}
	if !yamlNodeHasKey(node, "require_observed_mac_match") {
		cfg.RequireObservedMACMatch = defaults.RequireObservedMACMatch
	}
	if !yamlNodeHasKey(node, "max_pending_devices") {
		cfg.MaxPendingDevices = defaults.MaxPendingDevices
	}
	if !yamlNodeHasKey(node, "claim_endpoint_enabled_only_during_window") {
		cfg.ClaimEndpointEnabledOnlyDuringWindow = defaults.ClaimEndpointEnabledOnlyDuringWindow
	}
}

func normalizeConfig(cfg SynoraNetConfig) SynoraNetConfig {
	d := DefaultConfig().SynoraNet
	if strings.TrimSpace(cfg.SSID) == "" {
		cfg.SSID = d.SSID
	}
	if strings.TrimSpace(cfg.CountryCode) == "" {
		cfg.CountryCode = d.CountryCode
	}
	if strings.TrimSpace(cfg.SubnetCIDR) == "" {
		cfg.SubnetCIDR = d.SubnetCIDR
	}
	if strings.TrimSpace(cfg.GatewayIP) == "" {
		cfg.GatewayIP = d.GatewayIP
	}
	if strings.TrimSpace(cfg.DHCPStart) == "" {
		cfg.DHCPStart = d.DHCPStart
	}
	if strings.TrimSpace(cfg.DHCPEnd) == "" {
		cfg.DHCPEnd = d.DHCPEnd
	}
	if strings.TrimSpace(cfg.DHCLeaseTime) == "" {
		cfg.DHCLeaseTime = d.DHCLeaseTime
	}
	if cfg.DNS.Names == nil {
		cfg.DNS.Names = defaultDNSNames()
	}
	if strings.TrimSpace(cfg.Security.Mode) == "" {
		switch strings.TrimSpace(strings.ToLower(cfg.AP.WPA)) {
		case "wpa2":
			cfg.Security.Mode = "wpa2"
		case "wpa3":
			cfg.Security.Mode = "wpa3"
		case "wpa2-wpa3-transition", "transition":
			cfg.Security.Mode = "wpa2-wpa3-transition"
		default:
			cfg.Security.Mode = d.Security.Mode
		}
	}
	if strings.TrimSpace(cfg.Security.PMF) == "" {
		cfg.Security.PMF = map[string]string{"wpa3": "required", "wpa2-wpa3-transition": "optional", "wpa2": "disabled"}[cfg.Security.Mode]
		if cfg.Security.PMF == "" {
			cfg.Security.PMF = d.Security.PMF
		}
	}
	if cfg.Security.MinPassphraseLength == 0 {
		cfg.Security.MinPassphraseLength = d.Security.MinPassphraseLength
	}
	if strings.TrimSpace(cfg.Firewall.DefaultPolicy) == "" {
		cfg.Firewall.DefaultPolicy = d.Firewall.DefaultPolicy
	}
	if cfg.Visibility.PairingVisibilityTimeoutSeconds == 0 {
		cfg.Visibility.PairingVisibilityTimeoutSeconds = d.Visibility.PairingVisibilityTimeoutSeconds
	}
	if strings.TrimSpace(cfg.AccessControl.UnknownStationPolicy) == "" {
		cfg.AccessControl.UnknownStationPolicy = d.AccessControl.UnknownStationPolicy
	}
	if strings.TrimSpace(cfg.AccessControl.MACAllowlistFile) == "" {
		cfg.AccessControl.MACAllowlistFile = d.AccessControl.MACAllowlistFile
	}
	if strings.TrimSpace(cfg.ConnectionPolicy.Mode) == "" {
		cfg.ConnectionPolicy.Mode = d.ConnectionPolicy.Mode
	}
	if cfg.Pairing.WindowSeconds == 0 {
		cfg.Pairing.WindowSeconds = d.Pairing.WindowSeconds
	}
	if cfg.Pairing.MaxPendingDevices == 0 {
		cfg.Pairing.MaxPendingDevices = d.Pairing.MaxPendingDevices
	}
	if cfg.Firewall.PairingAllowedPorts == nil {
		cfg.Firewall.PairingAllowedPorts = append([]int(nil), d.Firewall.PairingAllowedPorts...)
	}
	if cfg.Firewall.CentralToCameraAllowedPorts == nil {
		cfg.Firewall.CentralToCameraAllowedPorts = append([]int(nil), d.Firewall.CentralToCameraAllowedPorts...)
	}
	if strings.TrimSpace(cfg.AP.PreferredBand) == "" {
		cfg.AP.PreferredBand = d.AP.PreferredBand
	}
	if strings.TrimSpace(cfg.AP.FallbackBand) == "" {
		cfg.AP.FallbackBand = d.AP.FallbackBand
	}
	if strings.TrimSpace(cfg.AP.Mode) == "" {
		cfg.AP.Mode = d.AP.Mode
	}
	if cfg.AP.Channel5GHz == 0 {
		cfg.AP.Channel5GHz = d.AP.Channel5GHz
	}
	if cfg.AP.Channel2GHz == 0 {
		cfg.AP.Channel2GHz = d.AP.Channel2GHz
	}
	if cfg.AP.Width5GHz == 0 {
		cfg.AP.Width5GHz = d.AP.Width5GHz
	}
	if strings.TrimSpace(cfg.AP.WPA) == "" {
		cfg.AP.WPA = d.AP.WPA
	}
	if strings.TrimSpace(cfg.AP.PassphraseFile) == "" {
		cfg.AP.PassphraseFile = d.AP.PassphraseFile
	}
	if strings.TrimSpace(cfg.Services.HTTPSAPIURL) == "" {
		cfg.Services.HTTPSAPIURL = d.Services.HTTPSAPIURL
	}
	if strings.TrimSpace(cfg.Services.HTTPAPIURL) == "" {
		cfg.Services.HTTPAPIURL = d.Services.HTTPAPIURL
	}
	if strings.TrimSpace(cfg.Services.VisionIngressURL) == "" {
		cfg.Services.VisionIngressURL = d.Services.VisionIngressURL
	}
	if strings.TrimSpace(cfg.Services.RTSPURL) == "" {
		cfg.Services.RTSPURL = d.Services.RTSPURL
	}
	return cfg
}

func ValidateConfig(cfg SynoraNetConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if !validSecurityModes[cfg.Security.Mode] {
		return fmt.Errorf("unknown security.mode %q", cfg.Security.Mode)
	}
	if !validPMF[cfg.Security.PMF] {
		return fmt.Errorf("unknown security.pmf %q", cfg.Security.PMF)
	}
	if cfg.Security.Mode == "wpa3" && cfg.Security.PMF != "required" {
		return errors.New("wpa3 requires security.pmf=required")
	}
	if cfg.Security.Mode == "wpa2-wpa3-transition" && cfg.Security.PMF == "disabled" {
		return errors.New("transition mode requires security.pmf=optional or required")
	}
	if cfg.Security.MinPassphraseLength < 16 {
		return errors.New("security.min_passphrase_length must be at least 16")
	}
	if cfg.Visibility.PairingVisibilityTimeoutSeconds < 60 || cfg.Visibility.PairingVisibilityTimeoutSeconds > 3600 {
		return errors.New("visibility.pairing_visibility_timeout_seconds must be between 60 and 3600")
	}
	if cfg.AccessControl.UnknownStationPolicy != "reject" && cfg.AccessControl.UnknownStationPolicy != "allow_pairing" {
		return fmt.Errorf("unsupported access_control.unknown_station_policy %q", cfg.AccessControl.UnknownStationPolicy)
	}
	if cfg.ConnectionPolicy.Mode != "central_initiated" && cfg.ConnectionPolicy.Mode != "camera_push_legacy" {
		return fmt.Errorf("unsupported connection_policy.mode %q", cfg.ConnectionPolicy.Mode)
	}
	if cfg.Pairing.WindowSeconds < 60 || cfg.Pairing.WindowSeconds > 3600 || cfg.Pairing.MaxPendingDevices < 1 {
		return errors.New("invalid pairing window or max_pending_devices")
	}
	if cfg.Firewall.DefaultPolicy != "deny" && cfg.Firewall.DefaultPolicy != "accept" {
		return fmt.Errorf("unsupported firewall.default_policy %q", cfg.Firewall.DefaultPolicy)
	}
	if strings.TrimSpace(cfg.Interface) == "" {
		return errors.New("synoranet interface is required when enabled")
	}
	if net.ParseIP(cfg.GatewayIP) == nil {
		return fmt.Errorf("invalid gateway_ip %q", cfg.GatewayIP)
	}
	_, subnet, err := net.ParseCIDR(cfg.SubnetCIDR)
	if err != nil || subnet == nil {
		return fmt.Errorf("invalid subnet_cidr %q", cfg.SubnetCIDR)
	}
	if !subnet.Contains(net.ParseIP(cfg.GatewayIP)) {
		return errors.New("gateway_ip is outside subnet_cidr")
	}
	for _, value := range []string{cfg.DHCPStart, cfg.DHCPEnd} {
		if !subnet.Contains(net.ParseIP(value)) {
			return fmt.Errorf("DHCP address %q is outside subnet_cidr", value)
		}
	}
	return nil
}

var validSecurityModes = map[string]bool{"wpa3": true, "wpa2-wpa3-transition": true, "wpa2": true}
var validPMF = map[string]bool{"required": true, "optional": true, "disabled": true}

func ValidatePassphrase(value string, minimum int) error {
	// 16 is the hard WPA2/SAE floor. security.min_passphrase_length is the
	// recommended length (24 by default), so values below it are warned about
	// but are not rejected until they cross the hard floor.
	if len([]byte(strings.TrimSpace(value))) < 16 {
		return errors.New("passphrase is shorter than the hard minimum (16 characters)")
	}
	return nil
}

func PassphraseNeedsWarning(value string, recommended int) bool {
	if recommended <= 0 {
		recommended = 24
	}
	return len([]byte(strings.TrimSpace(value))) < recommended
}

// MigrateConfig adds the explicit security/firewall model to an installed
// config. It backs up the exact original first and only writes config data;
// the passphrase file is neither read nor regenerated.
func MigrateConfig(path string, now time.Time) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		return "", err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cfg.SynoraNet.AP.WPA = ""
	return writeConfigWithBackup(path, cfg, original, now)
}

// WriteConfigWithBackup persists a validated configuration after making a
// caller-requested, non-runtime edit. The original bytes are backed up before
// the replacement is made; secret files are not read or rewritten.
func WriteConfigWithBackup(path string, cfg NetworkConfig, now time.Time) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return writeConfigWithBackup(path, cfg, original, now)
}

func writeConfigWithBackup(path string, cfg NetworkConfig, original []byte, now time.Time) (string, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	backup := path + ".backup-" + now.Format("20060102-150405")
	if _, err := os.Stat(backup); err == nil {
		backup += "-" + now.Format(".000000000")
	}
	mode := os.FileMode(0640)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(backup, original, mode); err != nil {
		return "", fmt.Errorf("backup network config: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("render migrated network config: %w", err)
	}
	tmp := path + ".migration.tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return backup, nil
}

func EnsurePassphrase(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("passphrase file is required")
	}
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if err := ValidatePassphrase(value, 16); err == nil {
			if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
				return "", chmodErr
			}
			if PassphraseNeedsWarning(value, 24) {
				log.Printf("SynoraNet passphrase is below the recommended length")
			}
			return value, nil
		}
		return "", errors.New("passphrase file contains fewer than 16 characters")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	value := "Synora-" + hex.EncodeToString(buf)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", err
	}
	return value, nil
}
