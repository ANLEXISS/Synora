package network

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPairingWindowLifecycleAndExpiryCleanup(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "pairing.json")
	t.Setenv("SYNORA_NETWORK_PAIRING_STATE_FILE", statePath)
	cfg := DefaultConfig().SynoraNet
	now := time.Now().UTC()
	state, err := StartPairingWindow(cfg, now)
	if err != nil || !state.Active || !state.SSIDVisible || state.NetworkPolicy != "pairing" {
		t.Fatalf("start state=%#v err=%v", state, err)
	}
	if err := AddPendingMAC("AA:BB:CC:DD:EE:FF"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil || !strings.Contains(string(data), "aa:bb:cc:dd:ee:ff") {
		t.Fatalf("pending MAC not persisted: err=%v data=%s", err, data)
	}
	loaded, err := LoadPairingState(statePath)
	if err != nil || !loaded.Active || loaded.PendingDevices != 1 {
		t.Fatalf("loaded state=%#v err=%v", loaded, err)
	}
	stopped, err := StopPairingWindow(now.Add(time.Minute))
	if err != nil || stopped.Active || stopped.SSIDVisible || stopped.NetworkPolicy != "runtime" || stopped.PendingDevices != 0 {
		t.Fatalf("stop state=%#v err=%v", stopped, err)
	}
}

func TestRenderDnsmasqKnownOnlyAndPairingModes(t *testing.T) {
	cfg := DefaultConfig().SynoraNet
	stations := []KnownStation{{DeviceID: "cam_01", MAC: "aa:bb:cc:dd:ee:ff", StaticIP: "10.77.0.51", AllowWiFi: true, Trust: "paired"}}
	normal := renderDnsmasqConfigState(cfg, false, stations)
	if !strings.Contains(normal, "dhcp-host=aa:bb:cc:dd:ee:ff,10.77.0.51,cam_01,12h,set:known") || !strings.Contains(normal, "dhcp-ignore=tag:!known") {
		t.Fatalf("known-only dnsmasq config missing static/ignore rules: %s", normal)
	}
	pairing := renderDnsmasqConfigState(cfg, true, stations)
	if strings.Contains(pairing, "dhcp-ignore=tag:!known") {
		t.Fatalf("pairing dnsmasq config must permit temporary pending leases: %s", pairing)
	}
}
