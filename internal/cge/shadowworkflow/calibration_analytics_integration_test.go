package shadowworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/cge/calibrationanalytics"
	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/decisioncomparison"
)

func TestCalibrationAnalyticsDisabledDoesNotQueryLedger(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Path = filepath.Join(t.TempDir(), "ledger.ndjson")
	// The default analytics configuration is disabled and must not build a
	// report even though the ledger itself is enabled.
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if status := r.CalibrationAnalyticsStatus(); status.Enabled || status.Available || status.ReportsGenerated != 0 {
		t.Fatalf("status=%+v", status)
	}
	if report := r.CalibrationAnalyticsReport(); report.ReportFingerprint != "" {
		t.Fatalf("unexpected report=%+v", report)
	}
}

func TestCalibrationAnalyticsConfigDefaultsAndDependency(t *testing.T) {
	defaults, err := LoadCalibrationAnalyticsConfig(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Enabled || defaults.Policy.MinimumRecords != 100 || defaults.Policy.MinimumComparableRecords != 50 || defaults.Policy.WindowSizeRecords != 100 || defaults.Policy.MaximumWindows != 100 || defaults.RecomputeEveryRecords != 100 {
		t.Fatalf("defaults=%+v", defaults)
	}
	values := map[string]string{CalibrationAnalyticsEnabledEnv: "true", CalibrationAnalyticsMinRecordsEnv: "7", CalibrationAnalyticsMinComparableEnv: "3", CalibrationAnalyticsWindowSizeEnv: "4", CalibrationAnalyticsMaxWindowsEnv: "9"}
	configured, err := LoadCalibrationAnalyticsConfig(func(key string) string { return values[key] })
	if err != nil || !configured.Enabled || configured.Policy.MinimumRecords != 7 || configured.Policy.MinimumComparableRecords != 3 || configured.Policy.WindowSizeRecords != 4 || configured.Policy.MaximumWindows != 9 {
		t.Fatalf("configured=%+v err=%v", configured, err)
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CalibrationAnalytics.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("analytics enabled without ledger was accepted")
	}
}

func TestCalibrationAnalyticsIsReadOnlyAndPublishedAfterLedgerAppend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Path = filepath.Join(t.TempDir(), "ledger.ndjson")
	cfg.CalibrationAnalytics.Enabled = true
	cfg.CalibrationAnalytics.RecomputeEveryRecords = 1
	cfg.CalibrationAnalytics.Policy.MinimumRecords = 1
	cfg.CalibrationAnalytics.Policy.MinimumComparableRecords = 1
	cfg.CalibrationAnalytics.Policy.MinimumRecordsPerCohort = 1
	cfg.CalibrationAnalytics.Policy.MinimumWindowsForTrend = 2
	cfg.CalibrationAnalytics.Policy.WindowSizeRecords = 1
	cfg.CalibrationAnalytics.Policy.DriftMinimumSampleSize = 1
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(at, "analytics-event")
	ref := decisioncomparison.HistoricalDecisionRef{ID: "analytics-historical", SourceEventRef: "analytics-event", PreviousStateCode: "activity", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(ref)
	input.HistoricalDecision = &ref
	if result := r.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool {
		return status.CommitsSucceeded == 1 && status.CalibrationLedger.RecordCount == 1
	})
	status := r.CalibrationAnalyticsStatus()
	if !status.Enabled || !status.Available || status.ReportsGenerated == 0 || status.LastRecordCount != 1 {
		t.Fatalf("analytics status=%+v", status)
	}
	report := r.CalibrationAnalyticsReport()
	if report.RecordCount != 1 || report.ReportFingerprint == "" || !report.Markers.DoesNotChangeThresholds {
		t.Fatalf("report=%+v", report)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || string(raw) == "{}" {
		t.Fatal("empty analytics report")
	}
	clone := r.CalibrationAnalyticsReport()
	if len(clone.Categories) > 0 {
		clone.Categories[0].Category = "mutated"
		if r.CalibrationAnalyticsReport().Categories[0].Category == "mutated" {
			t.Fatal("analytics report was not defensive")
		}
	}
	if _, err := r.RecomputeCalibrationAnalytics(); err != nil {
		t.Fatal(err)
	}
	if r.Status().CommitsSucceeded != 1 {
		t.Fatal("analytics recomputation changed durable workflow state")
	}
}

func TestCalibrationAnalyticsConcurrentReadersAndRecomputes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Path = filepath.Join(t.TempDir(), "ledger.ndjson")
	cfg.CalibrationAnalytics.Enabled = true
	cfg.CalibrationAnalytics.Policy.MinimumRecords = 1
	cfg.CalibrationAnalytics.Policy.MinimumComparableRecords = 1
	cfg.CalibrationAnalytics.Policy.MinimumRecordsPerCohort = 1
	cfg.CalibrationAnalytics.Policy.MinimumWindowsForTrend = 2
	cfg.CalibrationAnalytics.Policy.WindowSizeRecords = 1
	cfg.CalibrationAnalytics.Policy.DriftMinimumSampleSize = 1
	cfg.CalibrationAnalytics.RecomputeEveryRecords = 1
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(at, "analytics-concurrent")
	ref := decisioncomparison.HistoricalDecisionRef{ID: "analytics-concurrent-historical", SourceEventRef: "analytics-concurrent", PreviousStateCode: "activity", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(ref)
	input.HistoricalDecision = &ref
	if result := r.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, r, func(status StatusSnapshot) bool { return status.CalibrationLedger.RecordCount == 1 })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				_, _ = r.RecomputeCalibrationAnalytics()
				_ = r.CalibrationAnalyticsReport()
				_ = r.CalibrationAnalyticsStatus()
			}
		}()
	}
	wg.Wait()
	if r.CalibrationAnalyticsReport().ReportFingerprint == "" {
		t.Fatal("concurrent recomputes lost the published report")
	}
}

type analyticsUnavailableStore struct{}

func (analyticsUnavailableStore) Append(context.Context, calibrationledger.CalibrationRecord) (calibrationledger.AppendResult, error) {
	return calibrationledger.AppendResult{}, calibrationledger.ErrSnapshotUnavailable
}
func (analyticsUnavailableStore) Recover(context.Context) (calibrationledger.RecoveryResult, error) {
	return calibrationledger.RecoveryResult{Completed: true}, nil
}
func (analyticsUnavailableStore) Snapshot() calibrationledger.Snapshot {
	return calibrationledger.Snapshot{SchemaVersion: calibrationledger.SummarySchemaVersion, RecordCount: 1, FirstSequence: 1, LastSequence: 1, Digest: "snapshot-digest"}
}
func (analyticsUnavailableStore) Query(calibrationledger.Query) (calibrationledger.QueryResult, error) {
	return calibrationledger.QueryResult{}, calibrationledger.ErrSnapshotUnavailable
}
func (analyticsUnavailableStore) Close() error { return nil }

func TestCalibrationAnalyticsFailureIsolatedFromShadowRuntime(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Store = analyticsUnavailableStore{}
	cfg.CalibrationAnalytics.Enabled = true
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	status := r.CalibrationAnalyticsStatus()
	if !status.Enabled || status.Available || !status.Degraded || status.AnalysisFailures != 1 {
		t.Fatalf("analytics failure status=%+v", status)
	}
	if _, err := r.RecomputeCalibrationAnalytics(); !errors.Is(err, calibrationanalytics.ErrLedgerUnavailable) {
		t.Fatalf("recompute error=%v", err)
	}
	if r.Status().State == StateDegraded {
		t.Fatal("analytics failure degraded the Shadow workflow")
	}
}
