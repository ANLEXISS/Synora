package network

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	for _, want := range []string{"country_code=FR", "ssid=SynoraNet", "hw_mode=a", "channel=36", "wpa=2"} {
		if !strings.Contains(five, want) {
			t.Fatalf("5GHz config missing %q: %s", want, five)
		}
	}
	for _, want := range []string{"hw_mode=g", "channel=6", "wpa_passphrase=test-passphrase"} {
		if !strings.Contains(two, want) {
			t.Fatalf("2.4GHz config missing %q: %s", want, two)
		}
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
