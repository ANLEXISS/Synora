package main

import (
	"os"
	"path/filepath"
	"testing"

	"synora/internal/device"
	"synora/internal/engine"
	"synora/internal/topology"
)

func TestLoadCGECriticalChainsUsesConfiguredStartupPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cge_critical_chains.yaml")
	config := []byte(`critical_chains:
  - id: startup_path_test
    name: Startup path test
    danger_score: 0.9
    risk_level: high
    expected_state: suspicious
    requires_validation: true
    enabled: true
    sequence:
      - event_type: vision.unknown
`)
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatalf("write critical chains config: %v", err)
	}
	t.Setenv("SYNORA_CGE_CRITICAL_CHAINS", path)

	engineInstance := engine.NewEngine(
		&topology.Topology{Nodes: map[string]*topology.Node{}},
		device.NewRegistry(),
		nil,
	)
	loadedPath, err := loadCGECriticalChains(engineInstance)
	if err != nil {
		t.Fatalf("load CGE critical chains: %v", err)
	}
	if loadedPath != path {
		t.Fatalf("loaded path = %q, want %q", loadedPath, path)
	}

	seeds := engineInstance.CriticalSeeds()
	if len(seeds) != 1 || seeds[0].ID != "startup_path_test" {
		t.Fatalf("startup critical seeds = %#v", seeds)
	}
}

func TestCGECriticalChainsPathDefaultsToInstalledPath(t *testing.T) {
	t.Setenv("SYNORA_CGE_CRITICAL_CHAINS", "")

	if got := cgeCriticalChainsPath(); got != defaultCGECriticalChainsPath {
		t.Fatalf("CGE critical chains path = %q, want %q", got, defaultCGECriticalChainsPath)
	}
}
