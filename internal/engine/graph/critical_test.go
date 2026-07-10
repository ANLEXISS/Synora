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

func TestReplaceCriticalSeedsRemovesOldActiveSetAndPreservesRealCounters(t *testing.T) {
	memory := NewGraphMemory()
	seed := contracts.CriticalSeed{
		ID:                 "replace-test",
		Name:               "Replace test",
		DangerScore:        0.72,
		RiskLevel:          "high",
		ExpectedState:      "suspicious",
		Sequence:           []contracts.CriticalSeedStep{{EventType: "vision.unknown"}},
		RequiresValidation: true,
		Enabled:            true,
	}
	if err := memory.ReplaceCriticalSeeds([]contracts.CriticalSeed{seed}, false); err != nil {
		t.Fatal(err)
	}
	event := realCGEEvent("vision.unknown", time.Now().UTC())
	if match := memory.LearnEvent(event); match == nil || match.CriticalSeedID != seed.ID {
		t.Fatalf("seed should match before replacement: %#v", match)
	}

	seed.DangerScore = 0.88
	if err := memory.ReplaceCriticalSeeds([]contracts.CriticalSeed{seed}, false); err != nil {
		t.Fatal(err)
	}
	behavior := criticalBehaviorBySeedID(t, memory, seed.ID)
	if behavior.RealCount != 1 || behavior.DangerScore != 0.88 {
		t.Fatalf("replace should retain real counters and refresh config: %#v", behavior)
	}

	if err := memory.ReplaceCriticalSeeds(nil, false); err != nil {
		t.Fatal(err)
	}
	if match := memory.LearnEvent(realCGEEvent("vision.unknown", time.Now().UTC().Add(time.Second))); match != nil {
		t.Fatalf("removed seed remained active: %#v", match)
	}
	behavior, ok := memory.LearnedBehavior(behavior.ID)
	if !ok || behavior.Enabled || behavior.Status != contracts.LearnedBehaviorDisabled || behavior.RealCount != 1 {
		t.Fatalf("removed seed behavior should remain inspectable and disabled: %#v", behavior)
	}
}

func TestCriticalSeedValidationRejectsDuplicateLowScoreAndUnsafeActions(t *testing.T) {
	base := contracts.CriticalSeed{
		ID:                 "validation-test",
		Name:               "Validation test",
		DangerScore:        0.70,
		RiskLevel:          "high",
		ExpectedState:      "intrusion",
		Sequence:           []contracts.CriticalSeedStep{{EventType: "vision.unknown"}},
		RequiresValidation: true,
		Enabled:            true,
	}
	if _, err := NormalizeCriticalSeeds([]contracts.CriticalSeed{base, base}, false); err == nil {
		t.Fatal("duplicate critical seed should be rejected")
	}
	low := base
	low.ID = "low"
	low.DangerScore = 0.64
	if _, err := NormalizeCriticalSeeds([]contracts.CriticalSeed{low}, false); err == nil {
		t.Fatal("low danger score should require explicit opt-in")
	}
	if _, err := NormalizeCriticalSeeds([]contracts.CriticalSeed{low}, true); err != nil {
		t.Fatalf("explicit low score should be accepted: %v", err)
	}
	unsafe := base
	unsafe.ID = "unsafe"
	unsafe.ProposedActions = []string{"door.unlock"}
	if _, err := NormalizeCriticalSeeds([]contracts.CriticalSeed{unsafe}, false); err == nil {
		t.Fatal("unsafe critical seed action should be rejected")
	}
}

func criticalBehaviorBySeedID(t *testing.T, memory *GraphMemory, seedID string) contracts.LearnedBehavior {
	t.Helper()
	for _, behavior := range memory.LearnedBehaviors() {
		if behavior.CriticalSeedID == seedID {
			return behavior
		}
	}
	t.Fatalf("critical behavior for seed %q missing", seedID)
	return contracts.LearnedBehavior{}
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
