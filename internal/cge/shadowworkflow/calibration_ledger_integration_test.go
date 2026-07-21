package shadowworkflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/decisioncomparison"
)

func calibrationTestReference(at time.Time) *decisioncomparison.HistoricalDecisionRef {
	value := &decisioncomparison.HistoricalDecisionRef{ID: "historical-ledger-test", SourceEventRef: "ledger-event", PreviousStateCode: "activity", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	value.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(*value)
	return value
}

func TestCalibrationLedgerDisabledCreatesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disabled.ndjson")
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CalibrationLedger.Path = path
	runtime, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	if runtime.CalibrationLedgerStatus().Enabled || runtime.CalibrationLedgerStatus().Available {
		t.Fatalf("disabled ledger status=%+v", runtime.CalibrationLedgerStatus())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled ledger created file err=%v", err)
	}
}

func TestCalibrationLedgerFileRecoveryAndProjectionSeparation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calibration.ndjson")
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Path = path
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runtime, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := testInput(at, "ledger-event")
	input.HistoricalDecision = calibrationTestReference(at)
	if result := runtime.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	waitForQualification(t, runtime, func(status StatusSnapshot) bool {
		return status.CommitsSucceeded == 1 && status.CalibrationLedger.RecordCount == 1
	})
	if runtime.CalibrationSummary().RecordCount != 1 {
		t.Fatalf("summary=%+v", runtime.CalibrationSummary())
	}
	if len(runtime.HistoricalDecisionComparisons().Comparisons) != 1 {
		t.Fatal("current comparison was not published")
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	status := restarted.CalibrationLedgerStatus()
	if !status.RecoveryCompleted || status.RecordCount != 1 || !status.Available {
		t.Fatalf("ledger status=%+v", status)
	}
	if restarted.CalibrationSnapshot().RecordCount != 1 {
		t.Fatal("calibration history was not recovered")
	}
	if len(restarted.HistoricalDecisionComparisons().Comparisons) != 0 {
		t.Fatal("durable calibration records were mixed into current comparisons")
	}
}

type failingCalibrationStore struct{}

func (failingCalibrationStore) Append(context.Context, calibrationledger.CalibrationRecord) (calibrationledger.AppendResult, error) {
	return calibrationledger.AppendResult{}, calibrationledger.ErrSyncFailed
}
func (failingCalibrationStore) Recover(context.Context) (calibrationledger.RecoveryResult, error) {
	return calibrationledger.RecoveryResult{Completed: true}, nil
}
func (failingCalibrationStore) Snapshot() calibrationledger.Snapshot {
	return calibrationledger.Snapshot{}
}
func (failingCalibrationStore) Close() error { return nil }

func TestCalibrationLedgerAppendFailurePreservesCognitiveProjection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Store = failingCalibrationStore{}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	runtime, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(context.Background())
	input := testInput(at, "ledger-failure-event")
	input.HistoricalDecision = calibrationTestReference(at)
	if result := runtime.TrySubmit(input); result.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", result)
	}
	status := waitForQualification(t, runtime, func(status StatusSnapshot) bool {
		return status.CommitsSucceeded == 1 && status.CalibrationLedger.AppendFailures == 1
	})
	if status.CommitsSucceeded != 1 || status.CyclesFailed != 0 {
		t.Fatalf("ledger failure changed committed cycle status=%+v", status)
	}
	if _, ok := runtime.CognitiveSituation("episode-ledger-failure-event"); !ok && len(runtime.CognitiveSituations().Situations) == 0 {
		t.Fatal("cognitive projection was lost")
	}
	if len(runtime.HistoricalDecisionComparisons().Comparisons) != 1 {
		t.Fatal("comparison was lost after ledger failure")
	}
	if !runtime.CalibrationLedgerStatus().Degraded || runtime.CalibrationLedgerStatus().LastErrorCode == "" {
		t.Fatalf("ledger status=%+v", runtime.CalibrationLedgerStatus())
	}
}
