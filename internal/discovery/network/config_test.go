package network

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig(t *testing.T) SynoraNetConfig {
	t.Helper()
	cfg := DefaultConfig().SynoraNet
	cfg.Enabled = true
	cfg.Interface = "wlan-test"
	cfg.AP.PassphraseFile = filepath.Join(t.TempDir(), "synoranet_psk")
	return cfg
}

func TestDefaultConfigUsesCameraSubnetAndSafeDisabledState(t *testing.T) {
	cfg := DefaultConfig().SynoraNet
	if cfg.Enabled || cfg.SubnetCIDR != "10.77.0.0/24" || cfg.GatewayIP != "10.77.0.1" {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
	if cfg.AP.Channel5GHz != 36 || cfg.AP.Channel2GHz != 6 || cfg.DHCPStart != "10.77.0.50" || cfg.DHCPEnd != "10.77.0.200" {
		t.Fatalf("unexpected radio or DHCP defaults: %#v", cfg)
	}
}

func TestEnsurePassphraseGenerates0600SecretWithoutLoggingValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret", "psk")
	value, err := EnsurePassphrase(path)
	if err != nil || len(value) < 8 {
		t.Fatalf("value=%q err=%v", value, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("secret stat=%v info=%v", err, info)
	}
	reloaded, err := EnsurePassphrase(path)
	if err != nil || reloaded != value {
		t.Fatalf("reload=%q/%v", reloaded, err)
	}
}

func TestHostapdConfigsPrefer5GHzAndProvide24GHzFallback(t *testing.T) {
	cfg := testConfig(t)
	five := renderHostapdConfig(cfg, "5GHz", "test-passphrase")
	two := renderHostapdConfig(cfg, "2.4GHz", "test-passphrase")
	for _, want := range []string{"driver=nl80211", "country_code=FR", "ssid=SynoraNet", "hw_mode=a", "channel=36", "wpa=2", "wpa_key_mgmt=SAE", "ieee80211w=2", "sae_password=test-passphrase", "ap_isolate=1"} {
		if !strings.Contains(five, want) {
			t.Fatalf("5GHz config missing %q: %s", want, five)
		}
	}
	for _, want := range []string{"hw_mode=g", "channel=6", "wpa_key_mgmt=SAE", "sae_password=test-passphrase"} {
		if !strings.Contains(two, want) {
			t.Fatalf("2.4GHz config missing %q: %s", want, two)
		}
	}
	if strings.Contains(five, "wpa_passphrase=") {
		t.Fatal("WPA3 config must not contain wpa_passphrase")
	}
}

func TestHostapdWPA2LegacyAndTransitionModes(t *testing.T) {
	cfg := testConfig(t)
	cfg.Security = SecurityConfig{Mode: "wpa2", PMF: "disabled", APIsolate: true, MinPassphraseLength: 24}
	wpa2 := renderHostapdConfig(cfg, "5GHz", "test-passphrase")
	if !strings.Contains(wpa2, "wpa_key_mgmt=WPA-PSK") || !strings.Contains(wpa2, "wpa_passphrase=test-passphrase") || strings.Contains(wpa2, "sae_password=") {
		t.Fatalf("unexpected WPA2 config: %s", wpa2)
	}
	cfg.Security = SecurityConfig{Mode: "wpa2-wpa3-transition", PMF: "optional", APIsolate: true, MinPassphraseLength: 24}
	transition := renderHostapdConfig(cfg, "5GHz", "test-passphrase")
	for _, want := range []string{"wpa_key_mgmt=WPA-PSK SAE", "wpa_passphrase=test-passphrase", "sae_password=test-passphrase", "ieee80211w=1"} {
		if !strings.Contains(transition, want) {
			t.Fatalf("transition config missing %q: %s", want, transition)
		}
	}
}

func TestHostapdHiddenNormalAndVisiblePairingModes(t *testing.T) {
	cfg := testConfig(t)
	cfg.Visibility.HiddenByDefault = true
	cfg.Visibility.VisibleDuringPairing = true
	cfg.AccessControl.Enabled = true
	cfg.AccessControl.StationAllowlist = true
	normal := renderHostapdConfigState(cfg, "5GHz", "test-passphrase", false, nil)
	for _, want := range []string{"ignore_broadcast_ssid=1", "ap_isolate=1", "macaddr_acl=1", "accept_mac_file=/run/synora/hostapd-allowed-stations", "wpa_key_mgmt=SAE", "ieee80211w=2"} {
		if !strings.Contains(normal, want) {
			t.Fatalf("normal hostapd config missing %q: %s", want, normal)
		}
	}
	pairing := renderHostapdConfigState(cfg, "5GHz", "test-passphrase", true, nil)
	for _, want := range []string{"ignore_broadcast_ssid=0", "macaddr_acl=0", "wpa_key_mgmt=SAE", "ieee80211w=2"} {
		if !strings.Contains(pairing, want) {
			t.Fatalf("pairing hostapd config missing %q: %s", want, pairing)
		}
	}
	if strings.Contains(pairing, "wpa_key_mgmt=WPA-PSK") {
		t.Fatalf("pairing mode must not silently fall back to WPA2: %s", pairing)
	}
}

func TestSecurityValidationAndLegacyMapping(t *testing.T) {
	cfg := testConfig(t)
	cfg.Security.PMF = "disabled"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("WPA3 with disabled PMF should be rejected")
	}
	cfg = testConfig(t)
	cfg.Security.Mode = "unknown"
	if err := ValidateConfig(cfg); err == nil {
		t.Fatal("unknown security mode should be rejected")
	}
	if err := ValidatePassphrase("short", 24); err == nil {
		t.Fatal("short passphrase should be rejected")
	}
	path := filepath.Join(t.TempDir(), "network.yaml")
	content := "synoranet:\n  enabled: true\n  interface: wlan-test\n  ap:\n    wpa: wpa2\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SynoraNet.Security.Mode != "wpa2" || loaded.SynoraNet.Security.PMF != "disabled" {
		t.Fatalf("legacy mapping=%#v", loaded.SynoraNet.Security)
	}
	if loaded.SynoraNet.Visibility.HiddenByDefault != true || loaded.SynoraNet.AccessControl.StationAllowlist != true || loaded.SynoraNet.ConnectionPolicy.Mode != "central_initiated" {
		t.Fatalf("secure defaults were not applied to partial legacy config: %#v", loaded.SynoraNet)
	}
}

func TestMigrateConfigBacksUpAndAddsSecurityWithoutTouchingPSK(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "network.yaml")
	original := []byte("synoranet:\n  enabled: true\n  interface: wlP2p33s0\n  ap:\n    wpa: wpa2\n    passphrase_file: /tmp/psk-not-read\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := MigrateConfig(path, time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrated)
	for _, want := range []string{"mode: wpa2", "enabled: true", "interface: wlP2p33s0", "passphrase_file: /tmp/psk-not-read"} {
		if !strings.Contains(text, want) {
			t.Fatalf("migrated config missing %q: %s", want, text)
		}
	}
	if strings.Contains(text, "wpa: wpa2") {
		t.Fatalf("legacy field should be removed after migration: %s", text)
	}
	backupData, err := os.ReadFile(backup)
	if err != nil || string(backupData) != string(original) {
		t.Fatalf("backup changed: err=%v data=%q", err, backupData)
	}
}

func TestFirewallRenderIsScopedAndAllowsConfiguredServices(t *testing.T) {
	cfg := testConfig(t)
	rules := RenderFirewall(cfg)
	for _, want := range []string{"add table inet synora_net", "synora_input", "synora_forward", "dport { 53 }", "dport { 67, 68 }", "synora_output", "tcp dport { 443, 7443, 8554, 8889 }", "iifname \"synorabr0\" drop"} {
		if !strings.Contains(rules, want) {
			t.Fatalf("firewall missing %q: %s", want, rules)
		}
	}
	for _, forbidden := range []string{"dport { 8080 }", "dport { 8443 }", "dport { 7070 }", "dport { 8888 }"} {
		if strings.Contains(rules, forbidden) {
			t.Fatalf("normal firewall unexpectedly allows client service %q: %s", forbidden, rules)
		}
	}
	pairingRules := RenderFirewallState(cfg, true)
	for _, want := range []string{"dport { 8080 }", "dport { 8443 }", "dport { 7070 }"} {
		if !strings.Contains(pairingRules, want) {
			t.Fatalf("pairing firewall missing %q: %s", want, pairingRules)
		}
	}
	if strings.Contains(pairingRules, "dport { 8554 }") || strings.Contains(pairingRules, "dport { 8888 }") || strings.Contains(pairingRules, "dport { 8889 }") {
		t.Fatalf("pairing firewall unexpectedly allows media ingress: %s", pairingRules)
	}
	if strings.Contains(rules, "add rule inet synora_net synora_input drop") || strings.Contains(rules, "iifname \"enP4p65s0\"") || strings.Contains(rules, "iifname \"tailscale0\"") {
		t.Fatalf("firewall unexpectedly affects global/non-Synora INPUT: %s", rules)
	}
}

func TestAPFallbackIsUsableWhen5GHzFails(t *testing.T) {
	cfg := testConfig(t)
	paths := map[string]string{}
	result, err := startAPWith(cfg, "test-passphrase", func(string) bool { return true }, func(path, content string) error { paths[path] = content; return nil }, func(path string) error {
		if strings.Contains(path, "5ghz") {
			return errors.New("unsupported channel")
		}
		return nil
	})
	if err != nil || result.ActiveBand != "2.4GHz" || !result.AP2GHz.Active || result.AP2GHz.Status != "degraded" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if !strings.Contains(result.AP2GHz.Message, "5 GHz failed") || len(paths) != 2 {
		t.Fatalf("result=%#v paths=%v", result, paths)
	}
}

func TestDnsmasqConfigContainsRangeAndLocalDNS(t *testing.T) {
	cfg := testConfig(t)
	text := renderDnsmasqConfig(cfg)
	for _, want := range []string{"interface=synorabr0", "dhcp-range=10.77.0.50,10.77.0.200,12h", "dhcp-option=3,10.77.0.1", "address=/synora.local/10.77.0.1", "address=/rtsp.synora.local/10.77.0.1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dnsmasq config missing %q: %s", want, text)
		}
	}
}
