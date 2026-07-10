package graph

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/engine/contracts"
)

func TestLoadCriticalSeedsIncludesUnknownPersistentEntrance(t *testing.T) {
	seeds := loadTestCriticalSeeds(t)

	var found bool
	for _, seed := range seeds {
		if seed.ID != "unknown_persistent_entrance" {
			continue
		}
		found = true
		signature := seedSignature(seed)
		if signature != "vision.unknown > vision.motion > vision.unknown" {
			t.Fatalf("unexpected seed signature: %s", signature)
		}
		if !seed.Enabled || seed.DangerScore == 0 || seed.RiskLevel == "" {
			t.Fatalf("seed should be enabled with risk metadata: %#v", seed)
		}
	}
	if !found {
		t.Fatalf("unknown_persistent_entrance seed missing from config")
	}
}

func TestCriticalSeedMatchesUnknownPersistentEntrance(t *testing.T) {
	memory := NewGraphMemory()
	memory.ApplyCriticalSeeds(loadTestCriticalSeeds(t))

	base := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	events := []string{"vision.unknown", "vision.motion", "vision.unknown"}
	steps := []string{"unknown_first", "entry_motion", "unknown_confirmed"}
	var matchID string
	for i, eventType := range events {
		match := memory.LearnEvent(simulatedCGEEvent(eventType, "run-critical-seed", steps[i], base.Add(time.Duration(i)*time.Second)))
		if match != nil {
			matchID = match.CriticalSeedID
		}
	}

	if matchID != "unknown_persistent_entrance" {
		t.Fatalf("expected unknown_persistent_entrance match, got %q", matchID)
	}
}

func loadTestCriticalSeeds(t *testing.T) []contracts.CriticalSeed {
	t.Helper()
	path := testCriticalSeedsPath(t)
	seeds, err := LoadCriticalSeeds(path)
	if err != nil {
		t.Fatalf("LoadCriticalSeeds(%q): %v", path, err)
	}
	return seeds
}

func testCriticalSeedsPath(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("configs", "cge_critical_chains.yaml"),
		filepath.Join("..", "..", "..", "configs", "cge_critical_chains.yaml"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("critical seeds config not found in candidates: %#v", candidates)
	return ""
}
