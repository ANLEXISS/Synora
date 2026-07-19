package validation

import (
	"context"
	"testing"
)

func TestManifestRestartDoesNotDuplicateResolutionEffects(t *testing.T) {
	for _, scenario := range CheckpointMatrix() {
		report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
		if err != nil || !report.Success {
			t.Fatalf("manifest replay failed for %s: %v", scenario.ID, err)
		}
		if report.Metrics.ReplaysPerformed == 0 {
			t.Fatalf("scenario %s did not replay", scenario.ID)
		}
	}
}

func TestJournalOnlyReplayIsExact(t *testing.T) {
	for _, scenario := range JournalReplayScenarios() {
		report, err := (&Runner{RootDir: t.TempDir()}).Run(context.Background(), scenario)
		if err != nil || !report.Success {
			t.Fatalf("journal-only replay failed for %s: %v", scenario.ID, err)
		}
		if report.Metrics.ReplaysPerformed == 0 {
			t.Fatalf("scenario %s did not replay from journal", scenario.ID)
		}
	}
}
