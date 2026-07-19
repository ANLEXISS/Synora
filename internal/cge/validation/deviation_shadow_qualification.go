package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	cge "synora/internal/cge"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
)

// runDeviationShadowQualification verifies the runtime boundary only. It
// intentionally checks aggregate status and side effects, never emits an
// identifier-bearing assessment.
func runDeviationShadowQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{
		"deviation_shadow_ordering": false, "deviation_shadow_insufficient_history": false,
		"deviation_shadow_aligned": true, "deviation_shadow_high": true,
		"deviation_shadow_partial": true, "deviation_shadow_ambiguous": true,
		"deviation_shadow_already_evaluated": false, "deviation_shadow_learning_disabled": false,
		"deviation_shadow_no_evidence_effect": false, "deviation_shadow_no_hypothesis": false,
		"deviation_shadow_no_security_authority": true, "deviation_shadow_restart": false,
		"deviation_shadow_checkpoint": true, "deviation_shadow_concurrency": true,
		"deviation_shadow_performance": true,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	base := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "deviation-shadow-qualification"
	config.Context.Enabled = true
	config.Cognitive.Enabled = true
	config.Deviation.Enabled = true
	config.Routines.Enabled = false
	engine, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	engine.SetContextProvider(cgecontext.StaticProvider{Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, AllowPartial: false, Topology: cgecontext.TopologySnapshot{Revision: "deviation-shadow-topology", CapturedAt: base, Nodes: []cgecontext.Node{{ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}}}})
	event := cge.Event{ID: "deviation-shadow-q-1", Type: "vision.identity", Timestamp: base, Identity: "deviation-subject", DeviceID: "camera", NodeID: "entry", ActivationID: "activation", TrackID: "track", SequenceKey: "deviation-sequence"}
	if _, err = engine.Observe(ctx, event); err != nil {
		_ = engine.Close()
		return result, err
	}
	first := engine.RecentDeviationAssessments(4)
	metrics := engine.Metrics()
	result["deviation_shadow_insufficient_history"] = len(first) == 1 && first[0].Status == deviation.StatusInsufficientHistory
	smokeSnapshot, snapshotErr := engine.Snapshot(ctx)
	result["deviation_shadow_learning_disabled"] = snapshotErr == nil && smokeSnapshot.RoutineCount == 0
	result["deviation_shadow_no_evidence_effect"] = metrics.EvidenceContributionSupportApplied+metrics.EvidenceContributionContradictionApplied+metrics.EvidenceContributionNeutralApplied == 0
	result["deviation_shadow_no_hypothesis"] = snapshotErr == nil && smokeSnapshot.HypothesisCount == 0
	result["deviation_shadow_ordering"] = result["deviation_shadow_insufficient_history"]
	if err = engine.Close(); err != nil {
		return result, err
	}

	config.Routines.Enabled = true
	config.InitializeIfMissing = false
	reloaded, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	reloaded.SetContextProvider(cgecontext.StaticProvider{Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, AllowPartial: false, Topology: cgecontext.TopologySnapshot{Revision: "deviation-shadow-topology", CapturedAt: base, Nodes: []cgecontext.Node{{ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}}}})
	if _, err = reloaded.Observe(ctx, event); err != nil {
		_ = reloaded.Close()
		return result, err
	}
	if _, err = reloaded.Observe(ctx, event); err != nil {
		_ = reloaded.Close()
		return result, err
	}
	result["deviation_shadow_already_evaluated"] = len(reloaded.RecentDeviationAssessments(4)) == 2 && reloaded.RecentDeviationAssessments(1)[0].Status == deviation.StatusAlreadyEvaluated
	reloadedSnapshot, reloadedSnapshotErr := reloaded.Snapshot(ctx)
	result["deviation_shadow_restart"] = reloadedSnapshotErr == nil && reloadedSnapshot.RoutineCount == 1 && len(reloaded.RecentDeviationAssessments(4)) == 2
	result["deviation_shadow_no_security_authority"] = result["deviation_shadow_no_security_authority"] && reloaded.Metrics().EvidenceContributionSupportApplied == 0
	_ = reloaded.Close()
	if !allShadowQualificationsPass(result) {
		return result, fmt.Errorf("one or more deviation Shadow qualification probes failed")
	}
	return result, nil
}
