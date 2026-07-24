package shadowworkflow

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testQualificationConfig(directory string) QualificationConfig {
	cfg := DefaultQualificationConfig()
	cfg.Enabled = true
	cfg.RunID = "qualification-test"
	cfg.OutputDirectory = directory
	cfg.SampleInterval = time.Hour
	cfg.FlushInterval = time.Hour
	return cfg
}

func testQualificationSample() QualificationSample {
	sample := QualificationSample{SampledAt: time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC), RuntimeState: string(StateRunning), PipelineDepth: string(DepthAdvisoryRequests), CircuitState: "closed", QueueCapacity: 4, Received: 2, Accepted: 2, CommitsSucceeded: 1, Process: ProcessSample{RSSBytes: 1024, HeapAlloc: 256, Goroutines: 1}}
	sample.SchemaVersion = qualificationSchemaVersion
	sample.RunID = "qualification-test"
	sample.Profile = QualificationProfileSmoke
	sample.SampleSequence = 1
	sample.Fingerprint = QualificationSampleFingerprint(sample)
	return sample
}

func TestQualificationDisabledCreatesNoFilesOrSampler(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "not-created")
	cfg := DefaultQualificationConfig()
	cfg.OutputDirectory = directory
	recorder, err := OpenQualificationRecorder(cfg, time.Now().UTC(), func() QualificationSample { return testQualificationSample() })
	if err != nil || recorder != nil {
		t.Fatalf("disabled recorder=%v err=%v", recorder, err)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("disabled instrumentation created output: %v", err)
	}
}

func TestQualificationRecorderRejectsSymlinkOutputFile(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(target, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, qualificationSamplesName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cfg := testQualificationConfig(directory)
	if recorder, err := OpenQualificationRecorder(cfg, time.Now().UTC(), func() QualificationSample { return testQualificationSample() }); recorder != nil || err == nil {
		t.Fatalf("symlink output accepted recorder=%v err=%v", recorder, err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "outside" {
		t.Fatalf("symlink target changed data=%q err=%v", data, err)
	}
}

func TestQualificationRecorderWritesBoundedRedactedFiles(t *testing.T) {
	directory := t.TempDir()
	cfg := testQualificationConfig(directory)
	recorder, err := OpenQualificationRecorder(cfg, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), func() QualificationSample { return testQualificationSample() })
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordStage(qualificationStageFullCycle, time.Now(), nil)
	recorder.RecordTransactionBytes(128)
	recorder.RecordSample()
	if err := recorder.Close(nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{qualificationSamplesName, qualificationSummaryName, qualificationManifestName} {
		path := filepath.Join(directory, name)
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("file=%s stat=%v info=%v", name, statErr, info)
		}
	}
	info, err := os.Stat(directory)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions=%v info=%v", err, info)
	}
	data, err := os.ReadFile(filepath.Join(directory, qualificationSamplesName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "EventID") || strings.Contains(string(data), "secret-marker") {
		t.Fatalf("qualification output contains forbidden identifier data: %s", data)
	}
	if _, invalid, err := ReadQualificationSamples(filepath.Join(directory, qualificationSamplesName), 10); err != nil || invalid != 0 {
		t.Fatalf("samples invalid=%d err=%v", invalid, err)
	}
}

func TestQualificationOutputLimitAndTruncatedSampleAreReported(t *testing.T) {
	directory := t.TempDir()
	cfg := testQualificationConfig(directory)
	cfg.MaxOutputBytes = 1
	recorder, err := OpenQualificationRecorder(cfg, time.Now().UTC(), func() QualificationSample { return testQualificationSample() })
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordSample()
	if err := recorder.Close(nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, qualificationSamplesName)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.WriteString("{\"truncated\":true")
	_ = file.Close()
	_, invalid, err := ReadQualificationSamples(path, 10)
	if err != nil || invalid == 0 {
		t.Fatalf("truncated sample invalid=%d err=%v", invalid, err)
	}
}

func TestQualificationReporterRejectsInvalidSampleFingerprint(t *testing.T) {
	path := filepath.Join(t.TempDir(), qualificationSamplesName)
	sample := testQualificationSample()
	sample.Fingerprint = "forged"
	data, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	_, invalid, err := ReadQualificationSamples(path, 10)
	if err != nil || invalid != 1 {
		t.Fatalf("invalid=%d err=%v", invalid, err)
	}
}

func TestQualificationReportExitCodesAndDeterministicFingerprint(t *testing.T) {
	if QualificationReportExitCode(QualificationPass) != 0 || QualificationReportExitCode(QualificationWarning) != 1 || QualificationReportExitCode(QualificationFail) != 2 || QualificationReportExitCode(QualificationIncomplete) != 3 {
		t.Fatal("invalid qualification exit code mapping")
	}
	manifest := QualificationManifest{SchemaVersion: "shadow-qualification-manifest-v1", RunID: "report-test", Profile: QualificationProfileSmoke, StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ConfigurationFingerprint: "config", SampleInterval: time.Second, MaxSamples: 10, MaxOutputBytes: 1000, System: "local", GoVersion: "test"}
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	sample := testQualificationSample()
	sample.RunID = manifest.RunID
	sample.Fingerprint = QualificationSampleFingerprint(sample)
	one := BuildQualificationReport([]QualificationSample{sample}, 0, manifest, DefaultQualificationConfig().Thresholds)
	two := BuildQualificationReport([]QualificationSample{sample}, 0, manifest, DefaultQualificationConfig().Thresholds)
	if one.Fingerprint != two.Fingerprint {
		t.Fatalf("report fingerprint is not deterministic: %s != %s", one.Fingerprint, two.Fingerprint)
	}
	if _, err := json.Marshal(one); err != nil {
		t.Fatal(err)
	}
}

func TestQualificationReportComputesTrendStorageAndCriticalGate(t *testing.T) {
	manifest := QualificationManifest{SchemaVersion: "shadow-qualification-manifest-v1", RunID: "trend-test", Profile: QualificationProfileEndurance, StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ConfigurationFingerprint: "config", SampleInterval: time.Minute, MaxSamples: 10, MaxOutputBytes: 1000, MaxWALBytes: 10_000, System: "local", GoVersion: "test"}
	manifest.Fingerprint = QualificationManifestFingerprint(manifest)
	samples := make([]QualificationSample, 2)
	for index := range samples {
		samples[index] = testQualificationSample()
		samples[index].RunID = manifest.RunID
		samples[index].SampleSequence = uint64(index + 1)
		samples[index].SampledAt = manifest.StartedAt.Add(time.Duration(index+1) * time.Minute)
		samples[index].Uptime = time.Duration(index+1) * time.Minute
		samples[index].Process.RSSBytes = int64(1000 + index*1000)
		samples[index].Process.HeapObjects = uint64(10 + index*10)
		samples[index].Process.CPUUserNS = int64(index+1) * int64(time.Second)
		samples[index].Storage.WALBytes = int64(1000 + index*1000)
		samples[index].CommitsSucceeded = uint64(index + 1)
		samples[index].HistoricalIsolation.DecisionMismatches = uint64(index)
		samples[index].Fingerprint = QualificationSampleFingerprint(samples[index])
	}
	report := BuildQualificationReport(samples, 0, manifest, DefaultQualificationConfig().Thresholds)
	if report.Process.RSSGrowthBytesPerHour <= 0 || report.Storage.WALGrowthBytesPerHour <= 0 || report.OverallStatus != QualificationFail {
		t.Fatalf("trend/report=%+v", report)
	}
	if findQualificationGate(report.GateResults, "historical_decision_mismatch").Status != QualificationFail {
		t.Fatalf("critical historical gate missing: %+v", report.GateResults)
	}
}

func findQualificationGate(values []QualificationGateResult, name string) QualificationGateResult {
	for _, value := range values {
		if value.Name == name {
			return value
		}
	}
	return QualificationGateResult{}
}

func TestQualificationRecorderFailureDoesNotStopWorkflow(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.Qualification = testQualificationConfig(filepath.Join(t.TempDir(), "qualification"))
	// The directory is deliberately replaced by a regular file after validation
	// to exercise the runtime's non-fatal recorder startup boundary.
	if err := os.RemoveAll(cfg.Qualification.OutputDirectory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Qualification.OutputDirectory, []byte("not-a-directory"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "recorder-failure")).Status != SubmitAccepted {
		t.Fatal("workflow rejected input after recorder failure")
	}
	status := waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if status.WorkflowRevision != 1 || r.Metrics()["qualification.recorder_failed"] == 0 {
		t.Fatalf("recorder failure affected workflow: %+v metrics=%v", status, r.Metrics())
	}
}

func TestQualificationRuntimeWritesLocalReportWithoutChangingWorkflow(t *testing.T) {
	directory := t.TempDir()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthAdvisoryRequests
	cfg.MaxProcessingDuration = 2 * time.Second
	cfg.Qualification = testQualificationConfig(directory)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result := r.TrySubmit(testInput(at, "runtime-qualification-event")); result.Status != SubmitAccepted {
		_ = r.Close(context.Background())
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CyclesSucceeded == 1 })
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{qualificationSamplesName, qualificationManifestName, qualificationSummaryName} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("missing runtime qualification file %s: %v", name, err)
		}
	}
	samples, invalid, err := ReadQualificationSamples(filepath.Join(directory, qualificationSamplesName), 10)
	if err != nil || invalid != 0 || len(samples) == 0 {
		t.Fatalf("runtime samples=%d invalid=%d err=%v", len(samples), invalid, err)
	}
	manifest, err := ReadQualificationManifest(filepath.Join(directory, qualificationManifestName))
	if err != nil {
		t.Fatal(err)
	}
	report := BuildQualificationReport(samples, invalid, manifest, cfg.Qualification.Thresholds)
	if report.SamplesRead == 0 || report.HistoricalIsolation.TrySubmitFailuresAffectingDecision != 0 {
		t.Fatalf("runtime report=%+v", report)
	}
}

func TestQualificationInstrumentationReadinessKeepsPhysicalFlagsFalse(t *testing.T) {
	readiness := QualificationInstrumentationReadiness()
	if !readiness.RecorderImplemented || !readiness.OfflineReporterImplemented || !readiness.QualificationGatesImplemented || !readiness.ReadyToStartPhysicalQualification {
		t.Fatalf("instrumentation readiness incomplete: %+v", readiness)
	}
	if readiness.PhysicalDeploymentPerformed || readiness.SmokeProfileExecutedOnHub || readiness.DurabilityProfileExecutedOnHub || readiness.EnduranceProfileExecutedOnHub || readiness.MultiDayStabilityValidated || readiness.ProductionAuthority || readiness.ActiveObservationImplemented || readiness.ActionExecutionImplemented || readiness.SecurityAuthority {
		t.Fatalf("instrumentation crossed physical or authority boundary: %+v", readiness)
	}
}
