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

func TestCalibrationLedgerDisabledDoesNotOpenPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.CalibrationLedger.Path = filepath.Join(t.TempDir(), "disabled.ndjson")
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if status := r.CalibrationLedgerStatus(); status.Enabled || status.Available {
		t.Fatalf("status=%+v", status)
	}
	if _, err := os.Stat(cfg.CalibrationLedger.Path); !os.IsNotExist(err) {
		t.Fatalf("disabled path opened: %v", err)
	}
}

func TestCalibrationLedgerIsDurableAndSeparateFromProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calibration.ndjson")
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Path = path
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ref := decisioncomparison.HistoricalDecisionRef{ID: "ledger-historical", SourceEventRef: "ledger-event", PreviousStateCode: "activity", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(ref)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := testInput(at, "ledger-event")
	input.HistoricalDecision = &ref
	if got := r.TrySubmit(input); got.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", got)
	}
	waitForQualification(t, r, func(s StatusSnapshot) bool { return s.CommitsSucceeded == 1 && s.CalibrationLedger.RecordCount == 1 })
	if r.CalibrationSummary().RecordCount != 1 {
		t.Fatalf("summary=%+v", r.CalibrationSummary())
	}
	if len(r.HistoricalDecisionComparisons().Comparisons) != 1 {
		t.Fatal("volatile comparison missing")
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(context.Background())
	status := restarted.CalibrationLedgerStatus()
	if !status.RecoveryCompleted || !status.Available || status.RecordCount != 1 {
		t.Fatalf("status=%+v", status)
	}
	if len(restarted.HistoricalDecisionComparisons().Comparisons) != 0 {
		t.Fatal("ledger was mixed into current projection")
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
func (failingCalibrationStore) Query(calibrationledger.Query) (calibrationledger.QueryResult, error) {
	return calibrationledger.QueryResult{}, nil
}
func (failingCalibrationStore) Close() error { return nil }

func TestCalibrationLedgerFailureDoesNotFailDurableCycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.PipelineDepth = DepthEpisode
	cfg.CalibrationLedger.Enabled = true
	cfg.CalibrationLedger.Store = failingCalibrationStore{}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ref := decisioncomparison.HistoricalDecisionRef{ID: "failing-ledger-historical", SourceEventRef: "failing-ledger-event", CurrentStateCode: "activity", HistoricalDecisionHasProductionAuthority: true, DecidedAtUnixNano: at.UnixNano()}
	ref.Fingerprint = decisioncomparison.HistoricalDecisionFingerprint(ref)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(at, "failing-ledger-event")
	input.HistoricalDecision = &ref
	if got := r.TrySubmit(input); got.Status != SubmitAccepted {
		t.Fatalf("submit=%+v", got)
	}
	status := waitForQualification(t, r, func(s StatusSnapshot) bool { return s.CommitsSucceeded == 1 && s.CalibrationLedger.AppendFailures == 1 })
	if status.CyclesFailed != 0 || !status.CalibrationLedger.Degraded {
		t.Fatalf("status=%+v", status)
	}
	if len(r.HistoricalDecisionComparisons().Comparisons) != 1 {
		t.Fatal("comparison lost")
	}
}
