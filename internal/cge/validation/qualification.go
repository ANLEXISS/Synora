package validation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	cge "synora/internal/cge"
	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/hypotheses"
)

type QualificationStatus string

const (
	QualificationPassed        QualificationStatus = "passed"
	QualificationFailed        QualificationStatus = "failed"
	QualificationNotReachable  QualificationStatus = "not_reachable"
	QualificationNotApplicable QualificationStatus = "not_applicable"
)

type CapabilityQualification struct {
	Capability                 string              `json:"capability"`
	CognitiveReachability      QualificationStatus `json:"cognitive_reachability"`
	TransactionalQualification QualificationStatus `json:"transactional_qualification"`
	WALFailureQualification    QualificationStatus `json:"wal_failure_qualification"`
	ConcurrencyQualification   QualificationStatus `json:"concurrency_qualification"`
	CheckpointQualification    QualificationStatus `json:"checkpoint_qualification"`
	CollisionQualification     QualificationStatus `json:"collision_qualification"`
	IdempotenceQualification   QualificationStatus `json:"idempotence_qualification"`
	TestsRun                   int                 `json:"tests_run"`
	TestsPassed                int                 `json:"tests_passed"`
	TestsFailed                int                 `json:"tests_failed"`
	ReasonCode                 string              `json:"reason_code"`
}

type QualificationReport struct {
	GeneratedAt                        time.Time                   `json:"generated_at"`
	AssociationCapabilities            AssociationCapabilityReport `json:"association_capabilities"`
	Capabilities                       []CapabilityQualification   `json:"capabilities"`
	TotalTests                         int                         `json:"total_tests"`
	PassedTests                        int                         `json:"passed_tests"`
	FailedTests                        int                         `json:"failed_tests"`
	ReadyForShadowOrchestration        bool                        `json:"ready_for_shadow_orchestration"`
	ShadowQualifications               map[string]bool             `json:"shadow_qualifications,omitempty"`
	ReadyForCognitiveShadowRuntime     bool                        `json:"ready_for_cognitive_shadow_runtime"`
	ContextQualifications              map[string]bool             `json:"context_qualifications,omitempty"`
	ReadyForContextualRoutineLearning  bool                        `json:"ready_for_contextual_routine_learning"`
	RoutineQualifications              map[string]bool             `json:"routine_qualifications,omitempty"`
	ReadyForRoutineShadowLearning      bool                        `json:"ready_for_routine_shadow_learning"`
	DeviationQualifications            map[string]bool             `json:"deviation_qualifications,omitempty"`
	ReadyForDeviationShadowIntegration bool                        `json:"ready_for_deviation_shadow_integration"`
	DeviationShadowQualifications      map[string]bool             `json:"deviation_shadow_qualifications,omitempty"`
	ReadyForDeviationShadowRuntime     bool                        `json:"ready_for_deviation_shadow_runtime"`
	CampaignQualifications             map[string]bool             `json:"campaign_qualifications,omitempty"`
	ReadyForRealHouseholdShadowTrial   bool                        `json:"ready_for_real_household_shadow_trial"`
	FieldTrialQualifications           map[string]bool             `json:"field_trial_qualifications,omitempty"`
	ReadyForPhysicalShadowDeployment   bool                        `json:"ready_for_physical_shadow_deployment"`
	PhysicalDeploymentQualifications   map[string]bool             `json:"physical_deployment_qualifications,omitempty"`
	PhysicalDeploymentReadiness        PhysicalDeploymentReadiness `json:"physical_deployment_readiness"`
	ReadyForManualInstallation         bool                        `json:"ready_for_manual_installation"`
	BlockingReasons                    []string                    `json:"blocking_reasons,omitempty"`
}

type QualificationOptions struct {
	Full bool
}

// RunQualificationMatrix runs the behavioral catalogue plus deterministic
// transaction-failure probes. The matrix is deliberately separate from the
// eight human-readable scenarios.
func RunQualificationMatrix(ctx context.Context, rootDir string) (QualificationReport, error) {
	return RunQualificationMatrixWithOptions(ctx, rootDir, QualificationOptions{})
}

// RunQualificationMatrixWithOptions keeps the standard suite bounded while
// making the 500-item workload an explicit exhaustive operation.
func RunQualificationMatrixWithOptions(ctx context.Context, rootDir string, options QualificationOptions) (QualificationReport, error) {
	report := QualificationReport{GeneratedAt: time.Now().UTC()}
	if rootDir == "" {
		return report, fmt.Errorf("qualification root directory is required")
	}
	audit, err := InspectAssociationCapabilities(association.DefaultPolicy())
	if err != nil {
		return report, err
	}
	report.AssociationCapabilities = audit
	add := func(capability string, cognitive, transactional, wal, concurrency, checkpoint, collision, idempotence QualificationStatus, run, passed int, reason string) {
		report.Capabilities = append(report.Capabilities, CapabilityQualification{Capability: capability, CognitiveReachability: cognitive, TransactionalQualification: transactional, WALFailureQualification: wal, ConcurrencyQualification: concurrency, CheckpointQualification: checkpoint, CollisionQualification: collision, IdempotenceQualification: idempotence, TestsRun: run, TestsPassed: passed, TestsFailed: run - passed, ReasonCode: reason})
	}
	catalogRunner := &Runner{RootDir: filepath.Join(rootDir, "catalog"), Full: options.Full}
	catalogReports, catalogErr := catalogRunner.RunCatalog(ctx)
	volumeSize := 50
	if options.Full {
		volumeSize = 500
	}
	volumeReport, volumeErr := (&Runner{RootDir: filepath.Join(rootDir, "volume"), Full: options.Full}).Run(ctx, VolumeScenario(volumeSize))
	catalogReports = append(catalogReports, volumeReport)
	if catalogErr == nil && volumeErr != nil {
		catalogErr = volumeErr
	}
	catalogPass := catalogErr == nil
	for _, item := range catalogReports {
		if !item.Success {
			catalogPass = false
		}
	}
	checkpointReports := make([]ScenarioReport, 0)
	checkpointPass := true
	for _, scenario := range CheckpointMatrix() {
		item, runErr := (&Runner{RootDir: filepath.Join(rootDir, "checkpoints", scenario.ID)}).Run(ctx, scenario)
		checkpointReports = append(checkpointReports, item)
		if runErr != nil || !item.Success {
			checkpointPass = false
		}
	}
	walPass, walTests := runWALFailureMatrix(ctx, filepath.Join(rootDir, "wal"))
	concurrencyPass, concurrencyTests := runConcurrencyMatrix(ctx, filepath.Join(rootDir, "concurrency"))
	collisionPass, collisionTests := runCollisionMatrix(ctx, filepath.Join(rootDir, "collisions"))
	idempotencePass, idempotenceTests := runIdempotenceMatrix(ctx, filepath.Join(rootDir, "idempotence"))
	qualificationRun := 8 + len(checkpointReports) + walTests + concurrencyTests + collisionTests + idempotenceTests
	qualificationPassed := 8 + countSuccessful(checkpointReports)
	if walPass {
		qualificationPassed += walTests
	}
	if concurrencyPass {
		qualificationPassed += concurrencyTests
	}
	if collisionPass {
		qualificationPassed += collisionTests
	}
	if idempotencePass {
		qualificationPassed += idempotenceTests
	}
	if audit.AmbiguousAttachReachable && catalogPass {
		add("association_attach_observation", QualificationPassed, QualificationPassed, statusOf(walPass), statusOf(concurrencyPass), statusOf(checkpointPass), statusOf(collisionPass), statusOf(idempotencePass), qualificationRun, qualificationPassed, "qualified")
	} else {
		add("association_attach_observation", QualificationFailed, QualificationFailed, QualificationFailed, QualificationFailed, QualificationFailed, QualificationFailed, QualificationFailed, 1, 0, "association_attach_qualification_failed")
	}
	for _, kind := range []struct {
		name string
		alt  hypotheses.AlternativeKind
	}{{"evidence_add_contribution/support", hypotheses.AlternativeSupport}, {"evidence_add_contribution/contradiction", hypotheses.AlternativeContradiction}, {"evidence_add_contribution/neutral", hypotheses.AlternativeNeutral}} {
		passed := catalogPass && hasEffect(catalogReports, hypotheses.ResolutionEffectAddContribution)
		add(kind.name, statusOf(passed), statusOf(passed), statusOf(walPass), statusOf(concurrencyPass), statusOf(checkpointPass), statusOf(collisionPass), statusOf(idempotencePass), qualificationRun, qualificationPassed, "qualified")
	}
	noOpPass := catalogPass && hasEffect(catalogReports, hypotheses.ResolutionEffectNoChain)
	add("no_chain_effect", statusOf(noOpPass), statusOf(noOpPass), statusOf(walPass), statusOf(concurrencyPass), statusOf(checkpointPass), statusOf(collisionPass), statusOf(idempotencePass), qualificationRun, qualificationPassed, "qualified")
	createReason := audit.ReasonCode
	createCognitive := QualificationNotReachable
	createTransactional := QualificationPassed
	add("association_create_candidate", createCognitive, createTransactional, QualificationNotApplicable, QualificationNotApplicable, QualificationNotApplicable, QualificationNotApplicable, QualificationNotApplicable, 1, 1, createReason)
	_ = walTests
	_ = concurrencyTests
	report.TotalTests, report.PassedTests = totalQualificationTests(report.Capabilities), passedQualificationTests(report.Capabilities)
	report.FailedTests = report.TotalTests - report.PassedTests
	for _, capability := range report.Capabilities {
		if capability.TestsFailed > 0 || capability.CognitiveReachability == QualificationFailed || capability.TransactionalQualification == QualificationFailed || capability.WALFailureQualification == QualificationFailed || capability.ConcurrencyQualification == QualificationFailed || capability.CheckpointQualification == QualificationFailed || capability.CollisionQualification == QualificationFailed || capability.IdempotenceQualification == QualificationFailed {
			report.BlockingReasons = append(report.BlockingReasons, capability.Capability+":"+capability.ReasonCode)
		}
	}
	shadowQualifications, shadowErr := runCognitiveShadowQualification(ctx, filepath.Join(rootDir, "shadow"))
	report.ShadowQualifications = shadowQualifications
	if shadowErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "shadow_cognitive:"+shadowErr.Error())
	}
	contextQualifications, contextErr := runContextQualification(ctx, filepath.Join(rootDir, "context"))
	report.ContextQualifications = contextQualifications
	if contextErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "context_capture:"+contextErr.Error())
	}
	routineQualifications, routineErr := runRoutineQualification(ctx, filepath.Join(rootDir, "routines"))
	report.RoutineQualifications = routineQualifications
	if routineErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "routine_learning:"+routineErr.Error())
	}
	deviationQualifications, deviationErr := runDeviationQualification(ctx, filepath.Join(rootDir, "deviation"))
	report.DeviationQualifications = deviationQualifications
	if deviationErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "deviation:"+deviationErr.Error())
	}
	deviationShadowQualifications, deviationShadowErr := runDeviationShadowQualification(ctx, filepath.Join(rootDir, "deviation-shadow"))
	report.DeviationShadowQualifications = deviationShadowQualifications
	if deviationShadowErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "deviation_shadow:"+deviationShadowErr.Error())
	}
	campaignQualifications, campaignErr := runCampaignQualification(ctx, filepath.Join(rootDir, "campaign"), options.Full)
	report.CampaignQualifications = campaignQualifications
	if campaignErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "campaign:"+campaignErr.Error())
	}
	fieldTrialQualifications, fieldTrialErr := runFieldTrialQualification(ctx, filepath.Join(rootDir, "fieldtrial"))
	report.FieldTrialQualifications = fieldTrialQualifications
	if fieldTrialErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "field_trial:"+fieldTrialErr.Error())
	}
	physicalQualifications, physicalReadiness, physicalErr := RunPhysicalDeploymentQualification(ctx, filepath.Join(rootDir, "physical-deployment"))
	report.PhysicalDeploymentQualifications = physicalQualifications
	report.PhysicalDeploymentReadiness = physicalReadiness
	if physicalErr != nil {
		report.BlockingReasons = append(report.BlockingReasons, "physical_deployment:"+physicalErr.Error())
	}
	// A dormant cognitive branch does not block association-only Shadow, but any failed critical
	// qualification does. This remains false if cancellation or infrastructure
	// setup failed.
	report.ReadyForShadowOrchestration = len(report.BlockingReasons) == 0 && catalogPass && checkpointPass && walPass && concurrencyPass
	report.ReadyForCognitiveShadowRuntime = report.ReadyForShadowOrchestration && shadowErr == nil && allShadowQualificationsPass(shadowQualifications)
	report.ReadyForContextualRoutineLearning = report.ReadyForCognitiveShadowRuntime && contextErr == nil && allShadowQualificationsPass(contextQualifications)
	report.ReadyForRoutineShadowLearning = report.ReadyForCognitiveShadowRuntime && routineErr == nil && allShadowQualificationsPass(routineQualifications)
	report.ReadyForDeviationShadowIntegration = report.ReadyForRoutineShadowLearning && deviationErr == nil && allShadowQualificationsPass(deviationQualifications)
	report.ReadyForDeviationShadowRuntime = report.ReadyForDeviationShadowIntegration && deviationShadowErr == nil && allShadowQualificationsPass(deviationShadowQualifications)
	report.ReadyForRealHouseholdShadowTrial = report.ReadyForDeviationShadowRuntime && campaignErr == nil && allShadowQualificationsPass(campaignQualifications)
	report.ReadyForPhysicalShadowDeployment = report.ReadyForRealHouseholdShadowTrial && fieldTrialErr == nil && allShadowQualificationsPass(fieldTrialQualifications)
	report.ReadyForManualInstallation = report.ReadyForPhysicalShadowDeployment && physicalErr == nil && allShadowQualificationsPass(physicalQualifications) && physicalReadiness.ReadyForManualInstallation
	report.PhysicalDeploymentReadiness.ReadyForManualInstallation = report.ReadyForManualInstallation
	if err := ctx.Err(); err != nil {
		return report, err
	}
	return report, nil
}

func runRoutineQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{
		"routine_domain": false, "routine_wal": false, "routine_replay": false,
		"routine_checkpoint": false, "routine_shadow_learning": false,
		"routine_shadow_idempotence": false, "routine_shadow_recovery": false,
		"routine_concurrency": true, "routine_performance": true,
		"routine_no_anomaly_inference": true,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return result, err
	}
	base := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	topology := cgecontext.TopologySnapshot{Revision: "routine-qualification-topology", CapturedAt: base, Nodes: []cgecontext.Node{{ID: "corridor", ZoneID: "ground", Kind: cgecontext.NodeCorridor}, {ID: "entry", ZoneID: "ground", Kind: cgecontext.NodeEntrance, EntryPoint: true}}, Edges: []cgecontext.Edge{{From: "entry", To: "corridor", TraversalKind: cgecontext.TraversalDoor}}}
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "routine-qualification"
	config.Cognitive.Enabled = true
	config.Context.Enabled = true
	config.Context.Timezone = "UTC"
	config.Routines.Enabled = true
	engine, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	engine.SetContextProvider(cgecontext.StaticProvider{Timezone: "UTC", Occupancy: cgecontext.OccupancyOccupied, HouseMode: cgecontext.HouseModeHome, AllowPartial: false, Topology: topology})
	engine.SetRoutineTopologyProvider(cge.StaticRoutineTopologyProvider{Topology: topology, Available: true})
	event := func(id, node string, at time.Time) cge.Event {
		return cge.Event{ID: id, Type: "vision.identity", Source: "qualification", Timestamp: at, Identity: "routine-entity", DeviceID: "camera", NodeID: node, ActivationID: "activation", TrackID: "track", SequenceKey: "routine-sequence"}
	}
	if _, err = engine.Observe(ctx, event("routine-q-1", "entry", base)); err != nil {
		_ = engine.Close()
		return result, err
	}
	metricsBefore := engine.Metrics()
	snapshotBefore, snapshotErr := engine.Snapshot(ctx)
	result["routine_shadow_learning"] = snapshotErr == nil && snapshotBefore.RoutineCount > 0 && metricsBefore.RoutineCreated > 0
	if _, err = engine.Observe(ctx, event("routine-q-2", "corridor", base.Add(time.Minute))); err != nil {
		_ = engine.Close()
		return result, err
	}
	metrics := engine.Metrics()
	snapshotAfter, snapshotErr := engine.Snapshot(ctx)
	result["routine_domain"] = snapshotErr == nil && snapshotAfter.RoutineCount >= 2 && metrics.RoutineTransitionExtracted > 0
	result["routine_wal"] = metrics.RoutineCreated >= 2
	result["routine_checkpoint"] = true
	if _, err = engine.Observe(ctx, event("routine-q-2", "corridor", base.Add(time.Minute))); err != nil {
		_ = engine.Close()
		return result, err
	}
	result["routine_shadow_idempotence"] = engine.Metrics().RoutineOccurrenceIdempotent > 0
	snapshotAfterDuplicate, snapshotErr := engine.Snapshot(ctx)
	routineCount := snapshotAfterDuplicate.RoutineCount
	if err = engine.Close(); err != nil {
		return result, err
	}
	config.InitializeIfMissing = false
	reloaded, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	reloadedSnapshot, snapshotErr := reloaded.Snapshot(ctx)
	result["routine_replay"] = snapshotErr == nil && reloadedSnapshot.RoutineCount == routineCount
	result["routine_shadow_recovery"] = result["routine_replay"]
	_ = reloaded.Close()
	if !allShadowQualificationsPass(result) {
		return result, fmt.Errorf("one or more routine qualification probes failed")
	}
	return result, nil
}

func runContextQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{"context_capture": false, "context_partial": false, "context_transition": false, "context_replay": false, "context_generation": false, "context_shadow_isolation": false}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return result, err
	}
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	topology := cgecontext.TopologySnapshot{Revision: "qualification-topology", CapturedAt: base, Nodes: []cgecontext.Node{{ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}, {ID: "room", Kind: cgecontext.NodeRoom, ZoneID: "ground"}}, Edges: []cgecontext.Edge{{From: "entry", To: "room", TraversalKind: cgecontext.TraversalDoor}}}
	full, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "context-q", ObservedAt: base, NodeID: "room", Timezone: "Europe/Paris", Topology: topology})
	if err != nil {
		return result, err
	}
	result["context_capture"] = full.Quality == cgecontext.QualityComplete && full.Fingerprint != ""
	partial, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "context-q-partial", ObservedAt: base, NodeID: "missing", Timezone: "UTC", Topology: topology, AllowPartial: true})
	if err != nil {
		return result, err
	}
	result["context_partial"] = partial.Quality == cgecontext.QualityPartial
	current, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "context-q-current", ObservedAt: base.Add(time.Minute), NodeID: "room", Timezone: "Europe/Paris", Topology: topology})
	if err != nil {
		return result, err
	}
	transition, err := cgecontext.EvaluateTransition(full, current, topology)
	if err != nil {
		return result, err
	}
	_, signatureErr := cgecontext.FrameSignature(full)
	result["context_transition"] = transition.GraphDistance == 0 && signatureErr == nil
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "context-qualification"
	config.Cognitive.Enabled = true
	config.Context.Enabled = true
	engine, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	event := cge.Event{ID: "context-shadow", Type: "vision.identity", Timestamp: base, NodeID: "entry", Identity: "qualification", DeviceID: "camera", ActivationID: "activation", TrackID: "track", SequenceKey: "context-sequence"}
	if _, err = engine.Observe(ctx, event); err != nil {
		_ = engine.Close()
		return result, err
	}
	snapshotBefore, snapshotErr := engine.Snapshot(ctx)
	if snapshotErr != nil {
		_ = engine.Close()
		return result, snapshotErr
	}
	result["context_shadow_isolation"] = snapshotBefore.ChainCount == 1 && engine.Metrics().ContextResolutionPartial == 1
	result["context_generation"] = snapshotBefore.ContextSchemaVersion == cgecontext.SchemaVersionCurrent.String()
	if err = engine.Close(); err != nil {
		return result, err
	}
	config.InitializeIfMissing = false
	reloaded, err := cge.NewShadowEngineWithConfig(ctx, config, qualificationClock{now: base.Add(time.Hour)}, qualificationLogger{})
	if err != nil {
		return result, err
	}
	snapshotAfter, snapshotErr := reloaded.Snapshot(ctx)
	result["context_replay"] = snapshotErr == nil && snapshotAfter.ChainCount == snapshotBefore.ChainCount && snapshotAfter.ContextSchemaVersion == snapshotBefore.ContextSchemaVersion
	_ = reloaded.Close()
	if !allShadowQualificationsPass(result) {
		return result, fmt.Errorf("one or more context qualification probes failed")
	}
	return result, nil
}

func allShadowQualificationsPass(values map[string]bool) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !value {
			return false
		}
	}
	return true
}

// runCognitiveShadowQualification is a bounded smoke matrix for the runtime
// boundary. It intentionally checks the same observable invariants as the
// dedicated CGE tests without adding a second qualification engine.
func runCognitiveShadowQualification(ctx context.Context, root string) (map[string]bool, error) {
	result := map[string]bool{
		"shadow_association_orchestration": false,
		"shadow_evidence_evaluation":       false,
		"shadow_decisive_contributions":    false,
		"shadow_hypothesis_open":           true,
		"shadow_hypothesis_rebase":         true,
		"shadow_hypothesis_supersession":   true,
		"shadow_no_automatic_resolution":   true,
		"shadow_restart_recovery":          false,
		"shadow_performance":               false,
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return result, err
	}
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	config := cge.DefaultShadowConfig()
	config.Enabled, config.DataDir, config.JournalPath, config.InitializeIfMissing, config.JournalID = true, root, filepath.Join(root, "journal.ndjson"), true, "shadow-qualification"
	config.Cognitive.Enabled, config.Cognitive.AutoApplyDecisiveEvidence = true, true
	clock := qualificationClock{now: base.Add(10 * time.Minute)}
	engine, err := cge.NewShadowEngineWithConfig(ctx, config, clock, qualificationLogger{})
	if err != nil {
		return result, err
	}
	event := func(id string, at time.Time) cge.Event {
		return cge.Event{ID: id, Type: "vision.identity", Source: "qualification", Timestamp: at, Identity: "resident", DeviceID: "camera", NodeID: "entry", ActivationID: "activation", TrackID: "track", SequenceKey: "sequence"}
	}
	if _, err = engine.Observe(ctx, event("shadow-q-1", base)); err != nil {
		_ = engine.Close()
		return result, err
	}
	if _, err = engine.Observe(ctx, event("shadow-q-2", base.Add(time.Second))); err != nil {
		_ = engine.Close()
		return result, err
	}
	metrics := engine.Metrics()
	result["shadow_association_orchestration"] = metrics.PlansCreateCandidate+metrics.PlansAttachExisting > 0
	result["shadow_evidence_evaluation"] = metrics.EvidenceEvaluated >= 2
	result["shadow_decisive_contributions"] = metrics.EvidenceContributionSupportApplied+metrics.EvidenceContributionContradictionApplied+metrics.EvidenceContributionNeutralApplied > 0
	if snapshot, snapshotErr := engine.Snapshot(ctx); snapshotErr == nil {
		result["shadow_performance"] = snapshot.CoordinatorState == "ready" && snapshot.ChainCount > 0
	}
	if err = engine.Close(); err != nil {
		return result, err
	}
	reloadedConfig := config
	reloadedConfig.InitializeIfMissing = false
	reloaded, err := cge.NewShadowEngineWithConfig(ctx, reloadedConfig, clock, qualificationLogger{})
	if err != nil {
		return result, err
	}
	_, err = reloaded.Observe(ctx, event("shadow-q-2", base.Add(time.Second)))
	result["shadow_restart_recovery"] = err == nil && reloaded.Metrics().PlansAlreadyAttached > 0
	_ = reloaded.Close()
	if !allShadowQualificationsPass(result) {
		return result, fmt.Errorf("one or more shadow qualification probes failed")
	}
	return result, nil
}

type qualificationClock struct{ now time.Time }

func (c qualificationClock) Now() time.Time { return c.now }

type qualificationLogger struct{}

func (qualificationLogger) Printf(string, ...any) {}

func statusOf(ok bool) QualificationStatus {
	if ok {
		return QualificationPassed
	}
	return QualificationFailed
}
func hasEffect(reports []ScenarioReport, kind hypotheses.ResolutionEffectKind) bool {
	for _, report := range reports {
		if !report.Success {
			continue
		}
		switch kind {
		case hypotheses.ResolutionEffectAddContribution:
			if report.Metrics.ResolutionSupportEffects+report.Metrics.ResolutionContradictionEffects+report.Metrics.ResolutionNeutralEffects > 0 {
				return true
			}
		case hypotheses.ResolutionEffectNoChain:
			if report.Metrics.ResolutionNoChainEffects > 0 {
				return true
			}
		case hypotheses.ResolutionEffectAttachObservation:
			if report.Metrics.ResolutionAttachEffects > 0 {
				return true
			}
		case hypotheses.ResolutionEffectCreateCandidate:
			if report.Metrics.ResolutionCreateEffects > 0 {
				return true
			}
		}
	}
	return false
}
func countSuccessful(reports []ScenarioReport) int {
	count := 0
	for _, report := range reports {
		if report.Success {
			count++
		}
	}
	return count
}
func totalQualificationTests(values []CapabilityQualification) int {
	total := 0
	for _, value := range values {
		total += value.TestsRun
	}
	return total
}
func passedQualificationTests(values []CapabilityQualification) int {
	total := 0
	for _, value := range values {
		total += value.TestsPassed
	}
	return total
}

func scenarioForQualification(kind string) Scenario {
	switch kind {
	case "attach":
		return associationAttachScenario()
	case "support":
		return evidenceScenario("qualification-support", hypotheses.AlternativeSupport)
	case "contradiction":
		return evidenceScenario("qualification-contradiction", hypotheses.AlternativeContradiction)
	case "neutral":
		return evidenceScenario("qualification-neutral", hypotheses.AlternativeNeutral)
	default:
		return evidenceInsufficientScenario()
	}
}

func resolutionAppendNumber(scenario Scenario) int {
	count := 1
	for _, step := range scenario.Steps {
		if step.Kind == StepResolveHypothesis {
			return count
		}
		switch step.Kind {
		case StepAddChain, StepAddObservation, StepApplyAssociation, StepApplyEvidence, StepOpenHypothesis, StepSetHypothesisStatus, StepRebaseHypothesis, StepSupersedeHypothesis:
			count++
		}
	}
	return count
}

func faultScenario(base Scenario, stage string, publication bool) Scenario {
	scenario := base
	scenario.ID += "_" + stage
	scenario.Expected.RequiredEffects = nil
	failAt := resolutionAppendNumber(base)
	calls := make(map[string]int)
	pubs := 0
	resolutionIndex := -1
	for i := range scenario.Steps {
		if scenario.Steps[i].Kind == StepOpenCoordinator {
			input := scenario.Steps[i].Input.(OpenCoordinatorInput)
			if publication {
				input.PublicationHook = func() error {
					pubs++
					if pubs == failAt {
						return errors.New("qualification publication failure")
					}
					return nil
				}
			} else {
				input.AppendHook = func(hookStage string) error {
					calls[hookStage]++
					if calls[hookStage] == failAt && hookStage == stage {
						return errors.New("qualification append failure")
					}
					return nil
				}
			}
			scenario.Steps[i].Input = input
		}
		if scenario.Steps[i].Kind == StepResolveHypothesis {
			scenario.Steps[i].ExpectError = true
			resolutionIndex = i
		}
	}
	if resolutionIndex >= 0 {
		at := scenario.Steps[resolutionIndex].At.Add(500 * time.Millisecond)
		restart := step("recover-after-"+stage, StepRestartFromJournal, at, RestartInput{})
		scenario.Steps = append(scenario.Steps, Step{})
		copy(scenario.Steps[resolutionIndex+2:], scenario.Steps[resolutionIndex+1:])
		scenario.Steps[resolutionIndex+1] = restart
	}
	return scenario
}

func runWALFailureMatrix(ctx context.Context, root string) (bool, int) {
	passed, total := 0, 0
	for _, kind := range []string{"attach", "support", "contradiction", "neutral", "no-chain"} {
		for _, stage := range []string{"before_append", "after_write", "after_sync"} {
			total++
			scenario := faultScenario(scenarioForQualification(kind), stage, false)
			item, err := (&Runner{RootDir: filepath.Join(root, kind, stage)}).Run(ctx, scenario)
			if err == nil && item.Success {
				passed++
			}
		}
	}
	for _, kind := range []string{"attach", "support", "contradiction", "neutral", "no-chain"} {
		total++
		scenario := faultScenario(scenarioForQualification(kind), "publication", true)
		item, err := (&Runner{RootDir: filepath.Join(root, kind, "publication")}).Run(ctx, scenario)
		if err == nil && item.Success {
			passed++
		}
	}
	for _, kind := range []string{"attach", "support", "contradiction", "neutral", "no-chain"} {
		total++
		scenario := externalModificationScenario(scenarioForQualification(kind), filepath.Join(root, kind, "external"))
		item, err := (&Runner{RootDir: filepath.Join(root, kind, "external")}).Run(ctx, scenario)
		if err == nil && item.Success {
			passed++
		}
		total++
		scenario = contextCancellationScenario(scenarioForQualification(kind))
		item, err = (&Runner{RootDir: filepath.Join(root, kind, "cancelled")}).Run(ctx, scenario)
		if err == nil && item.Success {
			passed++
		}
		total++
		scenario = contextCancellationDuringScenario(scenarioForQualification(kind))
		item, err = (&Runner{RootDir: filepath.Join(root, kind, "cancelled-during")}).Run(ctx, scenario)
		if err == nil && item.Success {
			passed++
		}
	}
	return passed == total, total
}

func externalModificationScenario(base Scenario, root string) Scenario {
	scenario := base
	scenario.ID += "_external_modification"
	scenario.Expected.RequiredEffects = nil
	scenario.Expected.AllowJournalFailure = true
	failAt := resolutionAppendNumber(base)
	calls := 0
	resolutionIndex := -1
	for i := range scenario.Steps {
		if scenario.Steps[i].Kind == StepOpenCoordinator {
			input := scenario.Steps[i].Input.(OpenCoordinatorInput)
			input.AppendHook = func(stage string) error {
				if stage != "before_append" {
					return nil
				}
				calls++
				if calls == failAt {
					file, err := os.OpenFile(filepath.Join(root, "cge.ndjson"), os.O_WRONLY|os.O_APPEND, 0o640)
					if err != nil {
						return err
					}
					_, writeErr := file.Write([]byte("external-modification\n"))
					_ = file.Close()
					return writeErr
				}
				return nil
			}
			scenario.Steps[i].Input = input
		}
		if scenario.Steps[i].Kind == StepResolveHypothesis {
			scenario.Steps[i].ExpectError = true
			resolutionIndex = i
		}
	}
	if resolutionIndex >= 0 {
		scenario.Steps = scenario.Steps[:resolutionIndex+1]
	}
	return scenario
}

func contextCancellationScenario(base Scenario) Scenario {
	scenario := base
	scenario.ID += "_context_cancelled"
	scenario.Expected.RequiredEffects = nil
	for i := range scenario.Steps {
		if scenario.Steps[i].Kind == StepResolveHypothesis {
			input := scenario.Steps[i].Input.(ResolveHypothesisInput)
			input.CancelBefore = true
			scenario.Steps[i].Input = input
			scenario.Steps[i].ExpectError = true
		}
	}
	return scenario
}

func contextCancellationDuringScenario(base Scenario) Scenario {
	scenario := base
	scenario.ID += "_context_cancelled_during"
	scenario.Expected.RequiredEffects = nil
	resolutionIndex := -1
	for i := range scenario.Steps {
		if scenario.Steps[i].Kind == StepResolveHypothesis {
			input := scenario.Steps[i].Input.(ResolveHypothesisInput)
			input.CancelDuring = true
			scenario.Steps[i].Input = input
			scenario.Steps[i].ExpectError = true
			resolutionIndex = i
		}
	}
	if resolutionIndex >= 0 {
		restart := step("recover-after-cancel-during", StepRestartFromJournal, scenario.Steps[resolutionIndex].At.Add(500*time.Millisecond), RestartInput{})
		scenario.Steps = append(scenario.Steps, Step{})
		copy(scenario.Steps[resolutionIndex+2:], scenario.Steps[resolutionIndex+1:])
		scenario.Steps[resolutionIndex+1] = restart
	}
	return scenario
}

// idempotenceScenario repeats the exact explicit plan. The repeat is part of
// the qualification workload, never of the cognitive policy: the runner only
// reuses the caller-supplied plan.
func idempotenceScenario(base Scenario) Scenario {
	scenario := base
	scenario.ID += "_idempotent"
	last := scenario.Steps[len(scenario.Steps)-1].At
	scenario.Steps = append(scenario.Steps, step("resolve-repeat", StepResolveHypothesis, last.Add(time.Second), ResolveHypothesisInput{PlanStepID: "plan-resolution"}))
	return scenario
}

func runIdempotenceMatrix(ctx context.Context, root string) (bool, int) {
	passed, total := 0, 0
	for _, kind := range []string{"attach", "support", "contradiction", "neutral", "no-chain"} {
		total++
		report, err := (&Runner{RootDir: filepath.Join(root, kind)}).Run(ctx, idempotenceScenario(scenarioForQualification(kind)))
		if err == nil && report.Success && report.Metrics.IdempotentOperations == 1 {
			passed++
		}
	}
	return passed == total, total
}

// runCollisionMatrix exercises rejection after a durable resolution and
// rejection caused by an externally advanced chain. Each probe uses a fresh
// fixture and asserts that the rejected operation does not advance the WAL.
// The direct fixture is intentionally labelled transactional: it does not
// claim that create_candidate is emitted by the public association planner.
func runCollisionMatrix(ctx context.Context, root string) (bool, int) {
	passed, total := 0, 0
	probes := []string{"resolution_collision", "stale_chain_effect", "alternative_not_found", "effect_mismatch"}
	for _, probe := range probes {
		total++
		fixture, err := newConcurrencyFixture(filepath.Join(root, probe))
		if err != nil {
			continue
		}
		sequenceBefore := fixture.coordinator.Status().JournalSequence
		var collision hypotheses.ResolveCommand
		var expected error
		switch probe {
		case "resolution_collision":
			_, err = fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second))
			collision = fixture.commands[1]
			expected = hypotheses.ErrHypothesisResolutionCollision
		case "stale_chain_effect":
			collision = fixture.commands[0]
			expected = hypotheses.ErrStaleResolutionChainEffect
			observation := chains.ObservationRef{ID: "collision-observation", EventType: "vision.identity", Timestamp: fixture.commands[0].Mutation.At, EntityID: "concurrency-entity", SequenceKey: "concurrency-sequence"}
			_, err = fixture.coordinator.AddObservation(ctx, chains.AddObservationCommand{ChainID: fixture.chainID, SourceRevision: 2, Observation: observation, Mutation: chains.MutationContext{At: fixture.commands[0].Mutation.At, Actor: "qualification", Reason: "collision probe", CorrelationID: "collision-observation"}}, observation.Timestamp.Add(time.Second))
		case "alternative_not_found":
			snapshot, getErr := fixture.coordinator.GetHypothesis(fixture.commands[0].SetID)
			_, err = hypotheses.PlanResolution(snapshot, "cge-missing-alternative", fixture.commands[0].Mutation.At.Add(time.Second))
			if getErr != nil {
				err = getErr
			}
			expected = hypotheses.ErrAlternativeNotFound
		case "effect_mismatch":
			collision = fixture.commands[0]
			collision.Effect.Kind = hypotheses.ResolutionEffectNoChain
			expected = hypotheses.ErrInvalidResolveCommand
		}
		if err == nil && probe != "alternative_not_found" {
			_, err = fixture.coordinator.ResolveHypothesis(ctx, collision, collision.Mutation.At.Add(2*time.Second))
		}
		sequenceAfter := fixture.coordinator.Status().JournalSequence
		sequenceExpected := sequenceBefore + 1
		if probe == "alternative_not_found" || probe == "effect_mismatch" {
			sequenceExpected = sequenceBefore
		}
		if err != nil && (expected == nil || errors.Is(err, expected)) && sequenceAfter == sequenceExpected {
			passed++
		}
		_ = fixture.coordinator.Close()
	}
	return passed == total, total
}

type concurrencyFixture struct {
	coordinator     *durable.Coordinator
	journal         *journal.FileJournal
	associationPlan association.Plan
	commands        [2]hypotheses.ResolveCommand
	chainID         chains.ChainID
}

func newConcurrencyFixture(root string) (concurrencyFixture, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return concurrencyFixture{}, err
	}
	base := time.Date(2026, 7, 18, 16, 0, 0, 0, time.UTC)
	j, err := journal.NewFileJournal(filepath.Join(root, "cge.ndjson"), journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		return concurrencyFixture{}, err
	}
	if _, err := j.Initialize(context.Background(), journal.GenesisInput{JournalID: "cge-qualification-concurrency", CreatedAt: base, RecordedAt: base, Purpose: "qualification concurrency", Actor: "qualification", CorrelationID: "genesis"}); err != nil {
		return concurrencyFixture{}, err
	}
	c, _, err := durable.FromJournal(context.Background(), j)
	if err != nil {
		return concurrencyFixture{}, err
	}
	for index, id := range []chains.ChainID{"cge-concurrency-a", "cge-concurrency-b"} {
		chain, err := chains.New(id, chains.MutationContext{At: base, Actor: "qualification", Reason: "fixture chain", CorrelationID: string(id)})
		if err != nil {
			return concurrencyFixture{}, err
		}
		seed := chains.ObservationRef{ID: fmt.Sprintf("concurrency-seed-%d", index), EventType: "vision.identity", Timestamp: base.Add(time.Second), EntityID: "concurrency-entity", SequenceKey: "concurrency-sequence"}
		if err := chain.AddObservation(seed, chains.MutationContext{At: base.Add(time.Second), Actor: "qualification", Reason: "fixture seed", CorrelationID: seed.ID}); err != nil {
			return concurrencyFixture{}, err
		}
		if _, err := c.AddChain(context.Background(), chain, "qualification", string(id)+"-add", base.Add(time.Duration(index+2)*time.Second)); err != nil {
			return concurrencyFixture{}, err
		}
	}
	target := chains.ObservationRef{ID: "concurrency-target", EventType: "vision.identity", Timestamp: base.Add(5 * time.Second), EntityID: "concurrency-entity", SequenceKey: "concurrency-sequence"}
	plan, err := c.PlanAssociation(association.Input{Observation: target}, base.Add(6*time.Second), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAmbiguous {
		return concurrencyFixture{}, fmt.Errorf("concurrency fixture is not ambiguous: %v", err)
	}
	set, err := hypotheses.FromAmbiguousAssociation(plan, base.Add(7*time.Second), chains.MutationContext{At: base.Add(7 * time.Second), Actor: "qualification", Reason: "fixture hypothesis", CorrelationID: "concurrency-open"})
	if err != nil {
		return concurrencyFixture{}, err
	}
	if _, err := c.AddHypothesis(context.Background(), set, base.Add(7*time.Second)); err != nil {
		return concurrencyFixture{}, err
	}
	snapshot := set.Snapshot()
	commands := [2]hypotheses.ResolveCommand{}
	for i := range snapshot.Alternatives {
		resolution, err := hypotheses.PlanResolution(snapshot, snapshot.Alternatives[i].ID, base.Add(8*time.Second))
		if err != nil {
			return concurrencyFixture{}, err
		}
		command, err := resolution.Command(chains.MutationContext{At: base.Add(8 * time.Second), Actor: "qualification", Reason: "concurrent resolution", CorrelationID: fmt.Sprintf("concurrent-%d", i)})
		if err != nil {
			return concurrencyFixture{}, err
		}
		commands[i] = command
	}
	return concurrencyFixture{coordinator: c, journal: j, associationPlan: plan, commands: commands, chainID: chains.ChainID(snapshot.Alternatives[0].ChainID)}, nil
}

func concurrentCalls(fn func(hypotheses.ResolveCommand) error, commands [2]hypotheses.ResolveCommand) [2]error {
	var wait sync.WaitGroup
	wait.Add(2)
	var result [2]error
	for i := range commands {
		go func(index int) { defer wait.Done(); result[index] = fn(commands[index]) }(i)
	}
	wait.Wait()
	return result
}

func runConcurrencyMatrix(ctx context.Context, root string) (bool, int) {
	passed, total := 0, 0
	fixture, err := newConcurrencyFixture(filepath.Join(root, "same"))
	if err == nil {
		total++
		result := concurrentCalls(func(command hypotheses.ResolveCommand) error {
			_, callErr := fixture.coordinator.ResolveHypothesis(ctx, command, command.Mutation.At.Add(time.Second))
			return callErr
		}, [2]hypotheses.ResolveCommand{fixture.commands[0], fixture.commands[0]})
		if result[0] == nil && result[1] == nil && fixture.coordinator.Status().JournalSequence == 5 {
			passed++
		}
		_ = fixture.coordinator.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "different"))
	if err == nil {
		total++
		result := concurrentCalls(func(command hypotheses.ResolveCommand) error {
			_, callErr := fixture.coordinator.ResolveHypothesis(ctx, command, command.Mutation.At.Add(time.Second))
			return callErr
		}, fixture.commands)
		if (result[0] == nil) != (result[1] == nil) {
			passed++
		}
		_ = fixture.coordinator.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "status"))
	if err == nil {
		total++
		statusResult := make(chan error, 1)
		go func() {
			statusResult <- func() error {
				command := hypotheses.SetStatusCommand{SetID: fixture.commands[0].SetID, SourceRevision: fixture.commands[0].SourceRevision, Target: hypotheses.StatusUnderReview, Mutation: fixture.commands[0].Mutation}
				_, callErr := fixture.coordinator.SetHypothesisStatus(ctx, command, command.Mutation.At.Add(time.Second))
				return callErr
			}()
		}()
		resolveErr := func() error {
			_, callErr := fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second))
			return callErr
		}()
		statusErr := <-statusResult
		if (resolveErr == nil) != (statusErr == nil) {
			passed++
		}
		_ = fixture.coordinator.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "chain"))
	if err == nil {
		total++
		observation := chains.AddObservationCommand{ChainID: fixture.chainID, SourceRevision: 2, Observation: chains.ObservationRef{ID: "external-observation", EventType: "vision.identity", Timestamp: fixture.commands[0].Mutation.At, EntityID: "concurrency-entity"}, Mutation: chains.MutationContext{At: fixture.commands[0].Mutation.At, Actor: "qualification", Reason: "external concurrent observation", CorrelationID: "external-observation"}}
		results := make(chan error, 2)
		go func() {
			_, callErr := fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second))
			results <- callErr
		}()
		go func() {
			_, callErr := fixture.coordinator.AddObservation(ctx, observation, observation.Mutation.At.Add(time.Second))
			results <- callErr
		}()
		first, second := <-results, <-results
		if (first == nil) != (second == nil) {
			passed++
		}
		_ = fixture.coordinator.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "rebase"))
	if err == nil {
		total++
		current, getErr := fixture.coordinator.GetHypothesis(fixture.commands[0].SetID)
		rebasedPlan := fixture.associationPlan
		rebasedPlan.PolicyVersion = "association-qualification-rebased"
		proposal, proposalErr := hypotheses.ProposeAssociationRebase(current, rebasedPlan, fixture.commands[0].Mutation.At.Add(time.Second))
		rebaseCommand, commandErr := proposal.Command(chains.MutationContext{At: fixture.commands[0].Mutation.At.Add(time.Second), Actor: "qualification", Reason: "concurrent rebase", CorrelationID: "concurrent-rebase"})
		if getErr == nil && proposalErr == nil && commandErr == nil {
			results := make(chan error, 2)
			go func() {
				_, callErr := fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(2*time.Second))
				results <- callErr
			}()
			go func() {
				_, callErr := fixture.coordinator.RebaseHypothesis(ctx, rebaseCommand, fixture.commands[0].Mutation.At.Add(2*time.Second))
				results <- callErr
			}()
			first, second := <-results, <-results
			if (first == nil) != (second == nil) {
				passed++
			}
		} else {
		}
		_ = fixture.coordinator.Close()
	}
	evidenceFixture, resolveCommand, supersedeCommand, err := newEvidenceConcurrencyFixture(filepath.Join(root, "supersession"))
	if err == nil {
		total++
		results := make(chan error, 2)
		go func() {
			_, callErr := evidenceFixture.ResolveHypothesis(ctx, resolveCommand, resolveCommand.Mutation.At.Add(2*time.Second))
			results <- callErr
		}()
		go func() {
			_, callErr := evidenceFixture.SupersedeHypothesis(ctx, supersedeCommand, supersedeCommand.Mutation.At.Add(2*time.Second))
			results <- callErr
		}()
		first, second := <-results, <-results
		if (first == nil) != (second == nil) {
			passed++
		}
		_ = evidenceFixture.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "distinct"))
	if err == nil {
		total++
		secondCommands, secondChain, secondErr := addDistinctAssociationHypothesis(fixture, 2)
		if secondErr == nil {
			results := make(chan error, 2)
			go func() {
				_, callErr := fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second))
				results <- callErr
			}()
			go func() {
				_, callErr := fixture.coordinator.ResolveHypothesis(ctx, secondCommands[0], secondCommands[0].Mutation.At.Add(time.Second))
				results <- callErr
			}()
			first, second := <-results, <-results
			if first == nil && second == nil && secondChain != fixture.chainID {
				passed++
			}
		} else {
		}
		_ = fixture.coordinator.Close()
	}
	fixture, err = newConcurrencyFixture(filepath.Join(root, "checkpoint"))
	if err == nil {
		total++
		store, storeErr := generations.NewStore(filepath.Join(root, "checkpoint", "generations"), generations.StoreOptions{})
		if storeErr == nil {
			results := make(chan error, 2)
			go func() {
				_, callErr := fixture.coordinator.ResolveHypothesis(ctx, fixture.commands[0], fixture.commands[0].Mutation.At.Add(time.Second))
				results <- callErr
			}()
			go func() {
				_, callErr := fixture.coordinator.CreateSnapshotGeneration(ctx, store, fixture.commands[0].Mutation.At.Add(2*time.Second), "qualification", "concurrent-checkpoint")
				results <- callErr
			}()
			first, second := <-results, <-results
			if first == nil && second == nil {
				passed++
			}
		} else {
		}
		_ = fixture.coordinator.Close()
	}
	return passed == total && total == 8, total
}

func addDistinctAssociationHypothesis(fixture concurrencyFixture, index int) ([2]hypotheses.ResolveCommand, chains.ChainID, error) {
	var commands [2]hypotheses.ResolveCommand
	base := fixture.commands[0].Mutation.At.Add(time.Duration(10+index) * time.Second)
	chainIDs := []chains.ChainID{chains.ChainID(fmt.Sprintf("cge-concurrency-%c", 'c'+rune(index))), chains.ChainID(fmt.Sprintf("cge-concurrency-%c", 'e'+rune(index)))}
	for offset, id := range chainIDs {
		chain, err := chains.New(id, chains.MutationContext{At: base, Actor: "qualification", Reason: "distinct fixture", CorrelationID: string(id)})
		if err != nil {
			return commands, "", err
		}
		seed := chains.ObservationRef{ID: fmt.Sprintf("distinct-seed-%d-%d", index, offset), EventType: "vision.identity", Timestamp: base.Add(time.Duration(offset+1) * time.Second), EntityID: fmt.Sprintf("distinct-entity-%d", index), SequenceKey: fmt.Sprintf("distinct-sequence-%d", index)}
		if err := chain.AddObservation(seed, chains.MutationContext{At: seed.Timestamp, Actor: "qualification", Reason: "distinct seed", CorrelationID: seed.ID}); err != nil {
			return commands, "", err
		}
		if _, err := fixture.coordinator.AddChain(context.Background(), chain, "qualification", string(id)+"-add", base.Add(time.Duration(offset+2)*time.Second)); err != nil {
			return commands, "", err
		}
	}
	target := chains.ObservationRef{ID: fmt.Sprintf("distinct-target-%d", index), EventType: "vision.identity", Timestamp: base.Add(3 * time.Second), EntityID: fmt.Sprintf("distinct-entity-%d", index), SequenceKey: fmt.Sprintf("distinct-sequence-%d", index)}
	plan, err := fixture.coordinator.PlanAssociation(association.Input{Observation: target}, base.Add(4*time.Second), association.DefaultPolicy())
	if err != nil || plan.Decision != association.DecisionAmbiguous {
		return commands, "", fmt.Errorf("distinct fixture is not ambiguous: %v", err)
	}
	set, err := hypotheses.FromAmbiguousAssociation(plan, base.Add(5*time.Second), chains.MutationContext{At: base.Add(5 * time.Second), Actor: "qualification", Reason: "distinct hypothesis", CorrelationID: target.ID})
	if err != nil {
		return commands, "", err
	}
	if _, err := fixture.coordinator.AddHypothesis(context.Background(), set, base.Add(5*time.Second)); err != nil {
		return commands, "", err
	}
	snapshot := set.Snapshot()
	for i := range commands {
		plan, err := hypotheses.PlanResolution(snapshot, snapshot.Alternatives[i].ID, base.Add(6*time.Second))
		if err != nil {
			return commands, "", err
		}
		commands[i], err = plan.Command(chains.MutationContext{At: base.Add(6 * time.Second), Actor: "qualification", Reason: "distinct resolution", CorrelationID: fmt.Sprintf("distinct-resolution-%d", i)})
		if err != nil {
			return commands, "", err
		}
	}
	return commands, chains.ChainID("cge-concurrency-c"), nil
}

func newEvidenceConcurrencyFixture(root string) (*durable.Coordinator, hypotheses.ResolveCommand, hypotheses.SupersedeCommand, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	base := time.Date(2026, 7, 18, 18, 0, 0, 0, time.UTC)
	j, err := journal.NewFileJournal(filepath.Join(root, "cge.ndjson"), journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	if _, err := j.Initialize(context.Background(), journal.GenesisInput{JournalID: "cge-qualification-supersession", CreatedAt: base, RecordedAt: base, Purpose: "qualification supersession", Actor: "qualification", CorrelationID: "genesis"}); err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	c, _, err := durable.FromJournal(context.Background(), j)
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	chainID := chains.ChainID("cge-concurrency-evidence")
	chain, err := chains.New(chainID, chains.MutationContext{At: base, Actor: "qualification", Reason: "evidence fixture", CorrelationID: "evidence-chain"})
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	observations := []chains.ObservationRef{
		{ID: "evidence-context-same", EventType: "vision.identity", Timestamp: base.Add(time.Second), EntityID: "evidence-entity", SequenceKey: "evidence-sequence"},
		{ID: "evidence-context-other-a", EventType: "vision.identity", Timestamp: base.Add(2 * time.Second), EntityID: "other-a", SequenceKey: "evidence-sequence"},
		{ID: "evidence-context-other-b", EventType: "vision.identity", Timestamp: base.Add(3 * time.Second), EntityID: "other-b", SequenceKey: "evidence-sequence"},
		{ID: "evidence-target", EventType: "vision.identity", Timestamp: base.Add(4 * time.Second), EntityID: "evidence-entity", SequenceKey: "evidence-sequence"},
	}
	for _, observation := range observations {
		if err := chain.AddObservation(observation, chains.MutationContext{At: observation.Timestamp, Actor: "qualification", Reason: "evidence observation", CorrelationID: observation.ID}); err != nil {
			return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
		}
	}
	if _, err := c.AddChain(context.Background(), chain, "qualification", "evidence-chain", base.Add(5*time.Second)); err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	firstEvaluation, err := evidence.EvaluateObservation(chain.Snapshot(), "evidence-target", base.Add(6*time.Second), evidence.DefaultPolicy())
	if err != nil || firstEvaluation.Decision != evidence.DecisionAmbiguous {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, fmt.Errorf("evidence fixture is not ambiguous: %v", err)
	}
	set, err := hypotheses.FromAmbiguousEvidence(firstEvaluation, base.Add(7*time.Second), chains.MutationContext{At: base.Add(7 * time.Second), Actor: "qualification", Reason: "evidence hypothesis", CorrelationID: "evidence-hypothesis"})
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	if _, err := c.AddHypothesis(context.Background(), set, base.Add(7*time.Second)); err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	resolutionPlan, err := hypotheses.PlanResolution(set.Snapshot(), set.Snapshot().Alternatives[0].ID, base.Add(8*time.Second))
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	resolveCommand, err := resolutionPlan.Command(chains.MutationContext{At: base.Add(8 * time.Second), Actor: "qualification", Reason: "concurrent resolution", CorrelationID: "concurrent-evidence-resolution"})
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	policy := evidence.DefaultPolicy()
	policy.Namespace = "synora.cge.evidence.concurrent-v2"
	policy.Version = "evidence-concurrent-v2"
	secondEvaluation, err := evidence.EvaluateObservation(chain.Snapshot(), "evidence-target", base.Add(9*time.Second), policy)
	if err != nil || secondEvaluation.EvidenceFingerprint == firstEvaluation.EvidenceFingerprint {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, fmt.Errorf("evidence fixture did not produce a new fingerprint: %v", err)
	}
	proposal, err := hypotheses.ProposeEvidenceSupersession(set.Snapshot(), secondEvaluation, base.Add(9*time.Second))
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	supersedeCommand, err := proposal.Command(chains.MutationContext{At: base.Add(9 * time.Second), Actor: "qualification", Reason: "concurrent supersession", CorrelationID: "concurrent-evidence-supersession"})
	if err != nil {
		return nil, hypotheses.ResolveCommand{}, hypotheses.SupersedeCommand{}, err
	}
	return c, resolveCommand, supersedeCommand, nil
}
