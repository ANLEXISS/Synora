package network

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

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
	Enabled      bool        `yaml:"enabled"`
	Interface    string      `yaml:"interface"`
	SSID         string      `yaml:"ssid"`
	CountryCode  string      `yaml:"country_code"`
	SubnetCIDR   string      `yaml:"subnet_cidr"`
	GatewayIP    string      `yaml:"gateway_ip"`
	DHCPStart    string      `yaml:"dhcp_start"`
	DHCPEnd      string      `yaml:"dhcp_end"`
	DHCLeaseTime string      `yaml:"dhcp_lease_time"`
	DNS          DNSConfig   `yaml:"dns"`
	AP           APConfig    `yaml:"ap"`
	Services     ServiceURLs `yaml:"services"`
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
	HTTPSAPIURL   string `yaml:"https_api_url"`
	RTSPURL       string `yaml:"rtsp_url"`
	WebRTCBaseURL string `yaml:"webrtc_base_url"`
	HLSBaseURL    string `yaml:"hls_base_url"`
}

type NetworkConfig struct {
	SynoraNet SynoraNetConfig `yaml:"synoranet"`
}

func DefaultConfig() NetworkConfig {
	return NetworkConfig{SynoraNet: SynoraNetConfig{
		Enabled: false, Interface: "", SSID: DefaultSSID, CountryCode: DefaultCountry,
		SubnetCIDR: DefaultSubnet, GatewayIP: DefaultGateway, DHCPStart: DefaultDHCPStart,
		DHCPEnd: DefaultDHCPEnd, DHCLeaseTime: "12h",
		DNS:      DNSConfig{Enabled: true, Names: defaultDNSNames()},
		AP:       APConfig{PreferredBand: "5GHz", FallbackBand: "2.4GHz", Mode: "fallback", Channel5GHz: 36, Channel2GHz: 6, Width5GHz: 40, WPA: "wpa2", PassphraseFile: DefaultPSKPath},
		Services: ServiceURLs{HTTPSAPIURL: "https://10.77.0.1:8443", RTSPURL: "rtsp://10.77.0.1:8554"},
	}}
}

func defaultDNSNames() map[string]string {
	return map[string]string{
		"synora.local": "10.77.0.1", "hub.synora.local": "10.77.0.1",
		"api.synora.local": "10.77.0.1", "rtsp.synora.local": "10.77.0.1",
	}
}

func LoadConfig(path string) (NetworkConfig, error) {
	cfg := DefaultConfig()
	if strings.TrimSpace(path) == "" {
		path = DefaultConfigPath
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse SynoraNet config: %w", err)
	}
	cfg.SynoraNet = normalizeConfig(cfg.SynoraNet)
	return cfg, ValidateConfig(cfg.SynoraNet)
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
	if strings.TrimSpace(cfg.Services.RTSPURL) == "" {
		cfg.Services.RTSPURL = d.Services.RTSPURL
	}
	return cfg
}

func ValidateConfig(cfg SynoraNetConfig) error {
	if !cfg.Enabled {
		return nil
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

func EnsurePassphrase(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("passphrase file is required")
	}
	if data, err := os.ReadFile(path); err == nil {
		value := strings.TrimSpace(string(data))
		if len(value) >= 8 {
			if chmodErr := os.Chmod(path, 0600); chmodErr != nil {
				return "", chmodErr
			}
			return value, nil
		}
		return "", errors.New("passphrase file contains fewer than 8 characters")
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
