package shadowworkflow

import (
	"sort"
	"sync/atomic"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/durableworkflow"
)

type RuntimeState string

const (
	StateDisabled            RuntimeState = "disabled"
	StateStarting            RuntimeState = "starting"
	StateRecovering          RuntimeState = "recovering"
	StateRunning             RuntimeState = "running"
	StateDegraded            RuntimeState = "degraded"
	StateCircuitOpen         RuntimeState = "circuit_open"
	StateStorageLimitReached RuntimeState = "storage_limit_reached"
	StateRecoveryFailed      RuntimeState = "recovery_failed"
	StateStopping            RuntimeState = "stopping"
	StateStopped             RuntimeState = "stopped"
)

type StatusSnapshot struct {
	State                RuntimeState
	Enabled              bool
	PipelineDepth        PipelineDepth
	StoreMode            StoreMode
	StorePersistent      bool
	QueueDepth           int
	QueueCapacity        int
	CircuitState         string
	WorkflowRevision     uint64
	LastSequence         uint64
	WorkflowDigest       string
	EpisodeCount         int
	FreshLayerCounts     map[durableworkflow.LayerKind]int
	StaleLayerCounts     map[durableworkflow.LayerKind]int
	Received             uint64
	Accepted             uint64
	Rejected             uint64
	DroppedQueueFull     uint64
	Duplicates           uint64
	CyclesSucceeded      uint64
	CyclesFailed         uint64
	CyclesTimedOut       uint64
	CommitsSucceeded     uint64
	CommitsFailed        uint64
	CheckpointsSucceeded uint64
	CheckpointsFailed    uint64
	RecoveryPerformed    bool
	RecoveryWarnings     []string
	ConsecutiveFailures  int
	LastErrorCode        string
	CalibrationLedger    CalibrationLedgerStatus
	CalibrationAnalytics CalibrationAnalyticsStatus
}

type CalibrationAnalyticsStatus struct {
	Enabled                 bool
	Available               bool
	Degraded                bool
	LastAnalyzedSequence    uint64
	LastReportFingerprint   string
	ReportsGenerated        uint64
	AnalysisFailures        uint64
	InsufficientDataCount   uint64
	LastRecordCount         uint64
	LastEligibleCohortCount int
	LastWindowCount         int
	LastErrorCode           string
}

type CalibrationLedgerStatus struct {
	Enabled                        bool
	Available                      bool
	Degraded                       bool
	RecordCount                    uint64
	LastSequence                   uint64
	LastRecordFingerprint          string
	RecoveryCompleted              bool
	RecoveryRepairedTrailingRecord bool
	AppendFailures                 uint64
	DuplicateRecords               uint64
	IntegrityFailures              uint64
	LastErrorCode                  string
}

func calibrationStatusFromSnapshot(snapshot calibrationledger.Snapshot) CalibrationLedgerStatus {
	return CalibrationLedgerStatus{Enabled: true, Available: true, RecordCount: snapshot.RecordCount, LastSequence: snapshot.LastSequence, LastRecordFingerprint: snapshot.LastRecordFingerprint}
}

type counters struct{ received, accepted, rejected, dropped, duplicates, success, failed, timeout, commits, commitFailed, checkpoints, checkpointFailed atomic.Uint64 }

func layerCounts(state durableworkflow.WorkflowState) (map[durableworkflow.LayerKind]int, map[durableworkflow.LayerKind]int) {
	fresh, stale := map[durableworkflow.LayerKind]int{}, map[durableworkflow.LayerKind]int{}
	for _, episode := range state.Episodes {
		for layer, value := range episode.Freshness {
			if value == durableworkflow.FreshnessFresh {
				fresh[layer]++
			}
			if value == durableworkflow.FreshnessStale {
				stale[layer]++
			}
		}
	}
	return fresh, stale
}
func cloneCounts(in map[durableworkflow.LayerKind]int) map[durableworkflow.LayerKind]int {
	out := make(map[durableworkflow.LayerKind]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func sortedLayerKeys(in map[durableworkflow.LayerKind]int) []durableworkflow.LayerKind {
	out := make([]durableworkflow.LayerKind, 0, len(in))
	for k := range in {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
