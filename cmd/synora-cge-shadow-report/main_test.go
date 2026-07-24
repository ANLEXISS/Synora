package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/cge/shadowworkflow"
)

func TestWriteReportIsAtomicAndPrivate(t *testing.T) {
	directory := t.TempDir()
	report := shadowworkflow.QualificationReport{SchemaVersion: "shadow-qualification-report-v1", RunID: "report-test", OverallStatus: shadowworkflow.QualificationPass, PhysicalDeploymentPerformed: false, MultiDayStabilityValidated: false}
	path := filepath.Join(directory, "summary.json")
	if err := writeReport(path, report); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("report permissions=%o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Name()) >= len(".qualification-report-") && entry.Name()[:len(".qualification-report-")] == ".qualification-report-" {
			t.Fatalf("temporary report was not cleaned: %s", entry.Name())
		}
	}
}

func TestOfflineReporterBuildsValidSummary(t *testing.T) {
	directory := t.TempDir()
	cfg := shadowworkflow.DefaultQualificationConfig()
	cfg.Enabled = true
	cfg.RunID = "report-valid"
	cfg.OutputDirectory = directory
	cfg.SampleInterval = time.Hour
	cfg.FlushInterval = time.Hour
	recorder, err := shadowworkflow.OpenQualificationRecorder(cfg, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), func() shadowworkflow.QualificationSample {
		return shadowworkflow.QualificationSample{SampledAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), RuntimeState: "running", PipelineDepth: "advisory_requests", CircuitState: "closed"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(nil); err != nil {
		t.Fatal(err)
	}
	samples, invalid, err := shadowworkflow.ReadQualificationSamples(filepath.Join(directory, "qualification.samples.ndjson"), cfg.MaxSamples)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := shadowworkflow.ReadQualificationManifest(filepath.Join(directory, "qualification.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	report := shadowworkflow.BuildQualificationReport(samples, invalid, manifest, cfg.Thresholds)
	output := filepath.Join(directory, "offline-summary.json")
	if err := writeReport(output, report); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}
