package cge

import (
	"context"
	"time"

	cgecontext "synora/internal/cge/context"
)

// CoreContextProviderStatus is an aggregate-only diagnostic view of the live
// Core context boundary. It contains no state identifiers or raw failures.
type CoreContextProviderStatus struct {
	Enabled   bool
	Available bool
	Degraded  bool

	SnapshotsRequested uint64
	SnapshotsSucceeded uint64
	SnapshotsFailed    uint64

	FreshSnapshots uint64
	AgingSnapshots uint64
	StaleSnapshots uint64
	EmptySnapshots uint64

	LastSnapshotRevision    uint64
	LastSnapshotFingerprint string
	LastErrorCode           string
}

func (e *ShadowEngine) ContextProviderStatus() CoreContextProviderStatus {
	if e == nil {
		return CoreContextProviderStatus{}
	}
	e.contextStatusMu.RLock()
	defer e.contextStatusMu.RUnlock()
	return e.contextStatus
}

func (e *ShadowEngine) ContextProviderMetrics() map[string]uint64 {
	status := e.ContextProviderStatus()
	metrics := e.Metrics()
	return map[string]uint64{
		"cge_core_context_snapshots_requested_total": status.SnapshotsRequested,
		"cge_core_context_snapshots_succeeded_total": status.SnapshotsSucceeded,
		"cge_core_context_snapshots_failed_total":    status.SnapshotsFailed,
		"cge_core_context_fresh_total":               status.FreshSnapshots,
		"cge_core_context_aging_total":               status.AgingSnapshots,
		"cge_core_context_stale_total":               status.StaleSnapshots,
		"cge_core_context_empty_total":               status.EmptySnapshots,
		"cge_core_context_snapshot_duration_ns":      metrics.CoreContextSnapshotDurationNS,
	}
}

func (e *ShadowEngine) recordContextRequested() {
	if e == nil {
		return
	}
	e.contextStatusMu.Lock()
	e.contextStatus.Enabled = true
	e.contextStatus.SnapshotsRequested++
	e.contextStatusMu.Unlock()
	if e.metrics != nil {
		e.metrics.mu.Lock()
		e.metrics.value.CoreContextSnapshotsRequested++
		e.metrics.mu.Unlock()
	}
}

func (e *ShadowEngine) recordContextFailure(code string) {
	if e == nil {
		return
	}
	e.contextStatusMu.Lock()
	e.contextStatus.Available = false
	e.contextStatus.Degraded = true
	e.contextStatus.SnapshotsFailed++
	e.contextStatus.LastErrorCode = code
	e.contextStatusMu.Unlock()
	if e.metrics != nil {
		e.metrics.coreContextFailed()
	}
}

func (e *ShadowEngine) recordContextSuccess(snapshot cgecontext.CoreContextSnapshot, durationNS uint64) {
	if e == nil {
		return
	}
	e.contextStatusMu.Lock()
	e.contextStatus.Available = true
	e.contextStatus.SnapshotsSucceeded++
	e.contextStatus.LastSnapshotRevision = snapshot.SourceRevision
	e.contextStatus.LastSnapshotFingerprint = snapshot.Fingerprint
	e.contextStatus.LastErrorCode = ""
	switch snapshot.Freshness.Overall {
	case cgecontext.FreshnessFresh:
		e.contextStatus.FreshSnapshots++
	case cgecontext.FreshnessAging:
		e.contextStatus.AgingSnapshots++
		e.contextStatus.Degraded = true
	case cgecontext.FreshnessStale:
		e.contextStatus.StaleSnapshots++
		e.contextStatus.Degraded = true
	case cgecontext.FreshnessUnknown:
		// EmptySnapshots is counted from the actual content below, not merely
		// from an unknown item freshness code.
		e.contextStatus.Degraded = true
	}
	empty := len(snapshot.Residents) == 0 && len(snapshot.Devices) == 0 && len(snapshot.Cameras) == 0 && snapshot.Topology.Revision == ""
	if empty {
		e.contextStatus.EmptySnapshots++
	}
	e.contextStatusMu.Unlock()
	if e.metrics != nil {
		e.metrics.coreContextSucceeded(snapshot.Freshness.Overall, durationNS, empty)
	}
}

func (e *ShadowEngine) contextSnapshot(ctx context.Context, ctxRequest cgecontext.SnapshotRequest) (frame cgecontext.Frame, err error) {
	defer func() {
		if recover() != nil {
			e.recordContextFailure("context_snapshot_panic")
			frame = cgecontext.Frame{}
			err = ErrShadowPanic
		}
	}()
	if e == nil || e.contextProvider == nil {
		return cgecontext.Frame{}, ErrShadowStartup
	}
	provider, ok := e.contextProvider.(cgecontext.CoreContextProvider)
	if !ok {
		return cgecontext.Frame{}, nil
	}
	e.recordContextRequested()
	started := time.Now()
	snapshot, err := provider.Snapshot(ctx, ctxRequest)
	if err != nil {
		e.recordContextFailure("context_snapshot_failed")
		return cgecontext.Frame{}, err
	}
	if err := snapshot.Validate(); err != nil {
		e.recordContextFailure("context_snapshot_invalid")
		return cgecontext.Frame{}, err
	}
	frame, err = snapshot.Frame(ctxRequest)
	if err != nil {
		e.recordContextFailure("context_frame_invalid")
		return cgecontext.Frame{}, err
	}
	duration := uint64(0)
	elapsed := time.Since(started)
	if elapsed > 0 {
		duration = uint64(elapsed.Nanoseconds())
	}
	e.recordContextSuccess(snapshot, duration)
	return frame, nil
}
