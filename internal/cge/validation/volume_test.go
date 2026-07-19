package validation

import (
	"context"
	"testing"
)

func TestValidationCatalogVolumeSmoke(t *testing.T) {
	// The qualification runner remains intentionally deterministic; this smoke
	// test repeats the complete catalog without asserting wall-clock duration.
	for i := 0; i < 3; i++ {
		reports, err := (&Runner{RootDir: t.TempDir()}).RunCatalog(context.Background())
		if err != nil {
			t.Fatalf("volume catalog run failed: %v", err)
		}
		for _, report := range reports {
			if !report.Success {
				t.Fatalf("volume scenario failed: %s", report.ScenarioID)
			}
		}
	}
}

func TestRepresentativeMixedResolutionVolume(t *testing.T) {
	// The exhaustive 500-item workload remains in VolumeScenario(500) and is
	// executed by `qualify --full`. The standard Go suite keeps a representative
	// mixed workload so normal and race runs terminate without removing the
	// exhaustive qualification branch.
	const standardVolumeSize = 50
	report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), VolumeScenario(standardVolumeSize))
	if err != nil || !report.Success {
		t.Fatalf("standard volume scenario failed: %v", err)
	}
	if report.FinalState.ChainCount != standardVolumeSize || report.FinalState.HypothesisCount != standardVolumeSize {
		t.Fatalf("unexpected volume state: %+v", report.FinalState)
	}
	if report.Metrics.ResolutionSupportEffects == 0 || report.Metrics.ResolutionContradictionEffects == 0 || report.Metrics.ResolutionNeutralEffects == 0 || report.Metrics.ResolutionNoChainEffects == 0 {
		t.Fatalf("volume did not cover mixed effects: %+v", report.Metrics)
	}
}
