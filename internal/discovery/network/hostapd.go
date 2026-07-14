package network

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type APStartResult struct {
	ActiveBand string
	AP5GHz     RuntimePart
	AP2GHz     RuntimePart
}

type RuntimePart struct {
	Status  string `json:"status"`
	Active  bool   `json:"active"`
	Message string `json:"message,omitempty"`
}

func renderHostapdConfig(cfg SynoraNetConfig, band string, passphrase string) string {
	lines := []string{
		"interface=" + cfg.Interface,
		"bridge=" + BridgeName,
		"ssid=" + cfg.SSID,
		"country_code=" + cfg.CountryCode,
		"wmm_enabled=1",
		"auth_algs=1",
		"wpa=2",
		"wpa_passphrase=" + passphrase,
		"wpa_key_mgmt=WPA-PSK",
		"rsn_pairwise=CCMP",
	}
	if band == "5GHz" {
		lines = append(lines, "hw_mode=a", fmt.Sprintf("channel=%d", cfg.AP.Channel5GHz), "ieee80211n=1", "ieee80211ac=1", "ht_capab=[HT40+]")
	} else {
		lines = append(lines, "hw_mode=g", fmt.Sprintf("channel=%d", cfg.AP.Channel2GHz), "ieee80211n=1")
	}
	if cfg.AP.Hidden {
		lines = append(lines, "ignore_broadcast_ssid=1")
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeHostapdConfig(path string, content string) error {
	if err := os.MkdirAll(DefaultRunDir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func supports5GHz(iface string) bool {
	output, err := exec.Command("iw", "list").Output()
	if err != nil {
		return true
	} // Detection is advisory; hostapd remains authoritative.
	text := string(output)
	return strings.Contains(text, "Band 2:") || strings.Contains(text, "5180 MHz") || strings.Contains(text, "5 GHz")
}

func startHostapd(configPath string) error {
	cmd := exec.Command("hostapd", configPath, "-B")
	if err := cmd.Run(); err != nil {
		// Do not include hostapd stderr: some builds echo configuration values.
		return fmt.Errorf("hostapd exited for %s: %w", configPath, err)
	}
	return nil
}

func startAP(cfg SynoraNetConfig) (APStartResult, error) {
	passphrase, err := EnsurePassphrase(cfg.AP.PassphraseFile)
	if err != nil {
		return APStartResult{}, err
	}
	return startAPWith(cfg, passphrase, supports5GHz, writeHostapdConfig, startHostapd)
}

func startAPWith(cfg SynoraNetConfig, passphrase string, supports5 func(string) bool, write func(string, string) error, run func(string) error) (APStartResult, error) {
	result := APStartResult{AP5GHz: RuntimePart{Status: "unavailable"}, AP2GHz: RuntimePart{Status: "unavailable"}}
	if supports5(cfg.Interface) {
		err := write(Hostapd5GHzConfigPath, renderHostapdConfig(cfg, "5GHz", passphrase))
		if err == nil {
			err = run(Hostapd5GHzConfigPath)
		}
		if err == nil {
			result.ActiveBand = "5GHz"
			result.AP5GHz = RuntimePart{Status: "ok", Active: true, Message: "5 GHz AP active"}
			return result, nil
		}
		result.AP5GHz = RuntimePart{Status: "degraded", Message: "5 GHz AP failed"}
	} else {
		result.AP5GHz = RuntimePart{Status: "unavailable", Message: "5 GHz AP not supported by adapter"}
	}
	if err := write(Hostapd2GHzConfigPath, renderHostapdConfig(cfg, "2.4GHz", passphrase)); err != nil {
		return result, err
	}
	if err := run(Hostapd2GHzConfigPath); err != nil {
		result.AP2GHz = RuntimePart{Status: "unavailable", Message: "hostapd failed on 5 GHz and 2.4 GHz"}
		return result, fmt.Errorf("5 GHz failed; 2.4 GHz fallback failed: %w", err)
	}
	result.ActiveBand = "2.4GHz"
	result.AP2GHz = RuntimePart{Status: "degraded", Active: true, Message: "5 GHz failed, running 2.4 GHz fallback"}
	return result, nil
}

// Kept for compatibility with callers outside the manager. It uses the safe
// file config and the same 5 GHz -> 2.4 GHz policy as Manager.Start.
func EnsureHostapd() error {
	cfg, err := LoadConfig(os.Getenv("SYNORA_NETWORK_CONFIG"))
	if err != nil {
		return err
	}
	if !cfg.SynoraNet.Enabled {
		return nil
	}
	_, err = startAP(cfg.SynoraNet)
	return err
}
