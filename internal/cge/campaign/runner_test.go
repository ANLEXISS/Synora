package campaign

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRunShortCampaign(t *testing.T) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	report, err := Run(context.Background(), profile, RunOptions{RootDir: filepath.Join(t.TempDir(), "campaign"), DaysOverride: 7})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Success || report.EventsFailed != 0 {
		t.Fatalf("campaign failed: success=%t failed=%d invariants=%v", report.Success, report.EventsFailed, report.InvariantFailures)
	}
	if report.EventCount == 0 || report.EventsSucceeded != report.EventCount {
		t.Fatalf("unexpected event counts: %+v", report)
	}
	if report.IdempotenceChecks == 0 || report.IdempotenceFailures != 0 {
		t.Fatalf("idempotence checks failed: %d/%d", report.IdempotenceFailures, report.IdempotenceChecks)
	}
	if report.DurableStateDigest == "" {
		t.Fatal("missing durable digest")
	}
}

func TestRunRestartAndCheckpointCampaign(t *testing.T) {
	profile, _ := ProfileByID("restart_stress_14d")
	report, err := Run(context.Background(), profile, RunOptions{RootDir: filepath.Join(t.TempDir(), "restart"), DaysOverride: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Success || report.RestartCount < 2 || report.CheckpointCount < 1 {
		t.Fatalf("restart campaign incomplete: %+v", report)
	}
}

func TestExperimentalLabelsDoNotReachCGE(t *testing.T) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	profile.DurationDays = 7
	profile.Episodes = []EpisodeTemplate{{ID: "label-only", Label: LabelOrdinary, StartDay: 2, StartMinuteOfDay: 21 * 60, ResidentID: "resident-a", Path: []string{"living-room", "hallway"}}}
	other := profile
	other.Episodes = append([]EpisodeTemplate(nil), profile.Episodes...)
	other.Episodes[0].Label = LabelSyntheticIntrusion
	first, err := Run(context.Background(), profile, RunOptions{RootDir: filepath.Join(t.TempDir(), "ordinary"), DaysOverride: 7})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), other, RunOptions{RootDir: filepath.Join(t.TempDir(), "synthetic"), DaysOverride: 7})
	if err != nil {
		t.Fatal(err)
	}
	if first.DurableStateDigest != second.DurableStateDigest {
		t.Fatal("label-only change modified durable state")
	}
	if len(first.Events) != len(second.Events) {
		t.Fatal("label-only change modified event count")
	}
	for index := range first.Events {
		left, right := first.Events[index], second.Events[index]
		left.Label, right.Label = "", ""
		if left != right {
			t.Fatalf("label-only change modified CGE result at event %d", index)
		}
	}
}
