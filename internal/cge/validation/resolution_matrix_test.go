package validation

import (
	"context"
	"testing"
)

func TestResolutionEffectMatrixIsDeterministic(t *testing.T) {
	for _, scenario := range []Scenario{
		evidenceScenario("matrix-support", "support"),
		evidenceScenario("matrix-contradiction", "contradiction"),
		evidenceScenario("matrix-neutral", "neutral"),
		evidenceInsufficientScenario(),
	} {
		first, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
		if err != nil || !first.Success {
			t.Fatalf("first run failed for %s: %v", scenario.ID, err)
		}
		second, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
		if err != nil || !second.Success {
			t.Fatalf("second run failed for %s: %v", scenario.ID, err)
		}
		if first.FinalState.ChainsSHA256 != second.FinalState.ChainsSHA256 || first.FinalState.HypothesesSHA256 != second.FinalState.HypothesesSHA256 || first.FinalState.JournalHeadHash != second.FinalState.JournalHeadHash {
			t.Fatalf("non-deterministic state for %s", scenario.ID)
		}
	}
}
