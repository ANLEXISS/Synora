package cge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"synora/internal/cge/calibrationledger"
	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/generations"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/decisioncomparison"
	"synora/internal/cge/fieldtrial"
	"synora/internal/cge/hypotheses"
	"synora/internal/cge/routines"
	"synora/internal/cge/shadowworkflow"
)

// RoutineTopologyProvider is a detached, read-only topology boundary. A
// provider may report unavailable; routine presence learning remains valid
// while transition learning is skipped explicitly.
type RoutineTopologyProvider interface {
	CurrentTopology(context.Context) (cgecontext.TopologySnapshot, bool, error)
}

type unavailableRoutineTopologyProvider struct{}

func (unavailableRoutineTopologyProvider) CurrentTopology(context.Context) (cgecontext.TopologySnapshot, bool, error) {
	return cgecontext.TopologySnapshot{}, false, nil
}

// StaticRoutineTopologyProvider is a detached test and integration adapter.
// It owns a value snapshot and never exposes mutable Core topology objects.
type StaticRoutineTopologyProvider struct {
	Topology  cgecontext.TopologySnapshot
	Available bool
}

func (p StaticRoutineTopologyProvider) CurrentTopology(context.Context) (cgecontext.TopologySnapshot, bool, error) {
	return p.Topology, p.Available, nil
}

// ShadowEngine records only enough state to validate Core integration. It has
// no decision, automation, action, persistence, or physical-device behavior.
type ShadowEngine struct {
	mu sync.RWMutex

	observationCount uint64
	lastObservedAt   time.Time
	lastEventType    string

	coordinator       *durable.Coordinator
	dataDir           string
	policy            association.Policy
	evidencePolicy    evidence.Policy
	allowlist         map[string]struct{}
	actor             string
	clock             Clock
	logger            Logger
	metrics           *shadowMetrics
	orchestrator      *ShadowOrchestrator
	contextProvider   cgecontext.Provider
	contextConfig     ShadowContextConfig
	routineConfig     ShadowRoutineConfig
	deviationConfig   ShadowDeviationConfig
	deviationStore    *RecentDeviationStore
	trialRecorder     *fieldtrial.Recorder
	fieldTrialConfig  fieldtrial.Config
	lastDeviation     ShadowDeviationResult
	lastAssessment    *DeviationAssessmentSummary
	lastOrchestration ShadowOrchestrationResult
	topologyProvider  RoutineTopologyProvider
	workflow          *shadowworkflow.Runtime
	closeOnce         sync.Once
	closeErr          error
}

// LastOrchestrationResult returns a detached, identifier-bearing diagnostic
// result for development harnesses. It is never consumed by the historical
// engine or any action path.
func (e *ShadowEngine) LastOrchestrationResult() ShadowOrchestrationResult {
	if e == nil {
		return ShadowOrchestrationResult{}
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.lastOrchestration
}

// LastDeviationAssessment returns the latest redacted assessment summary.
func (e *ShadowEngine) LastDeviationAssessment() (DeviationAssessmentSummary, bool) {
	if e == nil {
		return DeviationAssessmentSummary{}, false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.lastAssessment == nil {
		return DeviationAssessmentSummary{}, false
	}
	copy := *e.lastAssessment
	copy.ReasonCodes = append([]string(nil), copy.ReasonCodes...)
	return copy, true
}

// Status returns the durable coordinator status without exposing its mutable
// registries.
func (e *ShadowEngine) Status() durable.StatusSnapshot {
	if e == nil || e.coordinator == nil {
		return durable.StatusSnapshot{State: durable.StateClosed}
	}
	return e.coordinator.Status()
}

// WorkflowStatus returns the detached status of the optional experimental
// workflow. It is not part of the historical decision boundary.
func (e *ShadowEngine) WorkflowStatus() shadowworkflow.StatusSnapshot {
	if e == nil || e.workflow == nil {
		return shadowworkflow.StatusSnapshot{State: shadowworkflow.StateDisabled}
	}
	return e.workflow.Status()
}

// WorkflowProjection returns the detached read-only projections produced by
// the optional workflow. It exposes existing diagnostics only; the Core never
// consumes these values as decisions, commands, or actions.
func (e *ShadowEngine) WorkflowProjection() shadowworkflow.CognitiveProjectionSnapshot {
	if e == nil || e.workflow == nil {
		return shadowworkflow.CognitiveProjectionSnapshot{}
	}
	return e.workflow.CognitiveProjection()
}

// WorkflowCalibrationRecords returns defensive ledger records for diagnostics.
func (e *ShadowEngine) WorkflowCalibrationRecords(q calibrationledger.Query) (calibrationledger.QueryResult, error) {
	if e == nil || e.workflow == nil {
		return calibrationledger.QueryResult{}, calibrationledger.ErrSnapshotUnavailable
	}
	return e.workflow.CalibrationRecords(q)
}

// WorkflowCalibrationSnapshot returns the detached durable ledger snapshot.
func (e *ShadowEngine) WorkflowCalibrationSnapshot() calibrationledger.Snapshot {
	if e == nil || e.workflow == nil {
		return calibrationledger.Snapshot{}
	}
	return e.workflow.CalibrationSnapshot()
}

// ListRoutines returns defensive routine snapshots for development analysis.
func (e *ShadowEngine) ListRoutines() []routines.Snapshot {
	if e == nil || e.coordinator == nil {
		return nil
	}
	return e.coordinator.ListRoutines()
}

// ListChains returns defensive snapshots for development diagnostics. It is
// intentionally read-only and is not part of the historical decision path.
func (e *ShadowEngine) ListChains() []chains.Snapshot {
	if e == nil || e.coordinator == nil {
		return nil
	}
	return e.coordinator.List()
}

// ListHypotheses returns defensive hypothesis snapshots for development
// diagnostics. Hypothesis resolution remains unavailable to ShadowEngine.
func (e *ShadowEngine) ListHypotheses() []hypotheses.Snapshot {
	if e == nil || e.coordinator == nil {
		return nil
	}
	return e.coordinator.ListHypotheses()
}

// PlanAssociationForEvent exposes the same pure association plan used by the
// shadow runtime. It is a read-only diagnostic boundary for development tools;
// the returned plan is not applied by this method.
func (e *ShadowEngine) PlanAssociationForEvent(ctx context.Context, event Event) (association.Plan, error) {
	if err := contextErr(ctx); err != nil {
		return association.Plan{}, err
	}
	if e == nil || e.coordinator == nil {
		return association.Plan{}, ErrShadowStartup
	}
	adapted, err := AdaptEventWithAllowlist(event, mapKeys(e.allowlist))
	if err != nil {
		return association.Plan{}, err
	}
	if !adapted.Eligible {
		return association.Plan{}, fmt.Errorf("%w: %s", ErrShadowAdaptation, adapted.ReasonCode)
	}
	if e.contextProvider != nil {
		frame, contextErr := e.resolveContext(ctx, adapted.Input.Observation)
		if contextErr == nil {
			adapted.Input.Observation.Context = &frame
		}
	}
	return e.coordinator.PlanAssociation(adapted.Input, e.shadowNow(), e.policy)
}

// EvaluateEvidenceForObservation runs the existing pure evidence evaluator
// against a durable chain snapshot. It never appends a contribution or opens
// a hypothesis and is intended for live diagnostic explanation.
func (e *ShadowEngine) EvaluateEvidenceForObservation(ctx context.Context, chainID chains.ChainID, observationID string, at time.Time) (evidence.EvidenceEvaluation, error) {
	if err := contextErr(ctx); err != nil {
		return evidence.EvidenceEvaluation{}, err
	}
	if e == nil || e.coordinator == nil {
		return evidence.EvidenceEvaluation{}, ErrShadowStartup
	}
	chainSnapshot, err := e.coordinator.Get(chainID)
	if err != nil {
		return evidence.EvidenceEvaluation{}, err
	}
	return evidence.EvaluateObservation(chainSnapshot, observationID, at, e.evidencePolicy)
}

// SeedAssociationAmbiguityFixture creates a duplicate, synthetic branch from
// an already observed chain so the development harness can exercise the real
// association planner's equal-candidate path. It is deliberately explicit,
// append-only, and unavailable to any production decision path.
func (e *ShadowEngine) SeedAssociationAmbiguityFixture(ctx context.Context, at time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if e == nil || e.coordinator == nil {
		return ErrShadowStartup
	}
	items := e.coordinator.List()
	if len(items) == 0 {
		return fmt.Errorf("%w: no source chain", ErrShadowStartup)
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].LastSeenAt.After(items[j].LastSeenAt)
	})
	if len(items[0].Observations) == 0 {
		return fmt.Errorf("%w: source chain has no observations", ErrShadowStartup)
	}
	source := items[0].Observations[len(items[0].Observations)-1].Clone()
	mutationAt := source.Timestamp.UTC()
	if mutationAt.IsZero() {
		mutationAt = at.UTC()
	}
	digest := sha256.Sum256([]byte(string(items[0].ID)))
	branchID := chains.ChainID("cge-demo-ambiguity-" + hex.EncodeToString(digest[:6]))
	branch, err := chains.New(branchID, chains.MutationContext{At: mutationAt, Actor: DefaultShadowActor, Reason: "synthetic ambiguity fixture", CorrelationID: "cge-demo:ambiguity"})
	if err != nil {
		return err
	}
	if err := branch.AddObservation(source, chains.MutationContext{At: mutationAt.Add(time.Second), Actor: DefaultShadowActor, Reason: "synthetic ambiguity fixture", CorrelationID: "cge-demo:ambiguity", ObservationIDs: []string{source.ID}}); err != nil {
		return err
	}
	_, err = e.coordinator.AddChain(ctx, branch, DefaultShadowActor, "cge-demo:ambiguity", mutationAt)
	return err
}

func (e *ShadowEngine) SnapshotCount() int {
	if e == nil || e.deviationStore == nil {
		return 0
	}
	return e.deviationStore.Count()
}

// FieldTrialRecorder is an internal development boundary. It does not expose
// raw cognitive registries or the historical engine.
func (e *ShadowEngine) FieldTrialRecorder() *fieldtrial.Recorder {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.trialRecorder
}

// DurableStateDigest hashes sorted defensive snapshots of all three durable
// registries and the common journal head. Assessments are intentionally
// absent because they are ephemeral.
func (e *ShadowEngine) DurableStateDigest(ctx context.Context) (string, error) {
	if err := contextErr(ctx); err != nil {
		return "", err
	}
	if e == nil || e.coordinator == nil {
		return "", ErrShadowStartup
	}
	status := e.coordinator.Status()
	chains := e.coordinator.List()
	hypotheses := e.coordinator.ListHypotheses()
	routines := e.coordinator.ListRoutines()
	sort.Slice(chains, func(i, j int) bool { return chains[i].ID < chains[j].ID })
	sort.Slice(hypotheses, func(i, j int) bool { return hypotheses[i].ID < hypotheses[j].ID })
	sort.Slice(routines, func(i, j int) bool { return routines[i].ID < routines[j].ID })
	payload, err := json.Marshal(struct {
		Chains     any    `json:"chains"`
		Hypotheses any    `json:"hypotheses"`
		Routines   any    `json:"routines"`
		Sequence   uint64 `json:"sequence"`
		HeadHash   string `json:"head_hash"`
	}{chains, hypotheses, routines, status.JournalSequence, status.JournalHeadHash})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ValidateDurableState performs boundary validation for campaign probes.
// It does not run as a per-event runtime validation.
func (e *ShadowEngine) ValidateDurableState(ctx context.Context) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if e == nil || e.coordinator == nil {
		return ErrShadowStartup
	}
	for _, value := range e.coordinator.List() {
		if _, err := chains.Restore(value); err != nil {
			return fmt.Errorf("chain_state_invalid: %w", err)
		}
	}
	for _, value := range e.coordinator.ListHypotheses() {
		if _, err := hypotheses.Restore(value); err != nil {
			return fmt.Errorf("hypothesis_state_invalid: %w", err)
		}
	}
	for _, value := range e.coordinator.ListRoutines() {
		if _, err := routines.Restore(value); err != nil {
			return fmt.Errorf("routine_state_invalid: %w", err)
		}
	}
	return nil
}

// CreateCheckpoint exposes the existing durable generation mechanism to the
// development campaign only. Generations still contain chains; routines and
// hypotheses continue to replay from the global journal.
func (e *ShadowEngine) CreateCheckpoint(ctx context.Context, createdAt time.Time) (durable.SnapshotGenerationResult, error) {
	if e == nil || e.coordinator == nil || e.dataDir == "" {
		return durable.SnapshotGenerationResult{}, ErrShadowStartup
	}
	store, err := generations.NewStore(e.dataDir, generations.StoreOptions{})
	if err != nil {
		return durable.SnapshotGenerationResult{}, err
	}
	return e.coordinator.CreateSnapshotGeneration(ctx, store, createdAt, DefaultShadowActor, "cge-shadow:campaign:checkpoint")
}

// NewShadowEngine returns a concurrency-safe, non-decision-making observer.
func NewShadowEngine() *ShadowEngine {
	return &ShadowEngine{}
}

// SetContextProvider installs an explicit detached provider for embedding and
// qualification. It has no effect while context capture is disabled.
func (e *ShadowEngine) SetContextProvider(provider cgecontext.Provider) {
	if e == nil {
		return
	}
	e.contextProvider = provider
}

// SetRoutineTopologyProvider installs the read-only topology boundary used by
// optional routine transition extraction.
func (e *ShadowEngine) SetRoutineTopologyProvider(provider RoutineTopologyProvider) {
	if e == nil {
		return
	}
	if provider == nil {
		e.topologyProvider = unavailableRoutineTopologyProvider{}
		return
	}
	e.topologyProvider = provider
}

func (e *ShadowEngine) Observe(ctx context.Context, event Event) (ObservationResult, error) {
	if err := contextErr(ctx); err != nil {
		return ObservationResult{}, err
	}
	if e == nil {
		return ObservationResult{}, nil
	}
	if e.coordinator != nil {
		return e.observeRuntime(ctx, event, nil)
	}

	e.mu.Lock()
	e.observationCount++
	e.lastObservedAt = event.Timestamp
	e.lastEventType = event.Type
	result := ObservationResult{
		ObservedAt:       event.Timestamp,
		ObservationCount: e.observationCount,
		LastEventType:    e.lastEventType,
	}
	e.mu.Unlock()
	return result, nil
}

func (e *ShadowEngine) ObserveHistoricalDecision(ctx context.Context, event Event, historical decisioncomparison.HistoricalDecisionRef) (ObservationResult, error) {
	if err := contextErr(ctx); err != nil {
		return ObservationResult{}, err
	}
	if e == nil {
		return ObservationResult{}, nil
	}
	if e.coordinator != nil {
		return e.observeRuntime(ctx, event, &historical)
	}
	return e.Observe(ctx, event)
}

func (e *ShadowEngine) Snapshot(ctx context.Context) (Snapshot, error) {
	if err := contextErr(ctx); err != nil {
		return Snapshot{}, err
	}
	if e == nil {
		return Snapshot{}, nil
	}

	e.mu.RLock()
	defer e.mu.RUnlock()
	snapshot := Snapshot{
		ObservationCount:                      e.observationCount,
		LastObservedAt:                        e.lastObservedAt,
		LastEventType:                         e.lastEventType,
		ContextEnabled:                        e.contextConfig.Enabled,
		ContextAllowPartial:                   e.contextConfig.AllowPartial,
		ContextSchemaVersion:                  cgecontext.SchemaVersionCurrent.String(),
		RoutineLearningEnabled:                e.routineConfig.Enabled,
		RoutineTemporalBucketMinutes:          e.routineConfig.TemporalBucketMinutes,
		RoutineAllowPartialContext:            e.routineConfig.AllowPartialContext,
		RoutineMaxTransitionGap:               e.routineConfig.MaxTransitionGap,
		RoutineRequireSameTopologyRevision:    e.routineConfig.RequireSameTopologyRevision,
		DeviationEnabled:                      e.deviationConfig.Enabled,
		DeviationPolicyNamespace:              e.deviationConfig.Policy.Namespace,
		DeviationPolicyVersion:                e.deviationConfig.Policy.Version,
		DeviationRecentAssessmentLimit:        e.deviationConfig.RecentAssessmentLimit,
		DeviationMaxAssessmentsPerObservation: e.deviationConfig.MaxAssessmentsPerObservation,
		DeviationAssessmentStoreCount:         e.deviationStore.Count(),
	}
	if e.orchestrator != nil {
		snapshot.CognitiveShadowEnabled = e.orchestrator.config.Enabled
		snapshot.AutoEvidenceEnabled = e.orchestrator.config.AutoApplyDecisiveEvidence
		snapshot.MaxEvidenceReevaluations = e.orchestrator.config.MaxEvidenceReevaluationsPerObservation
		status := e.coordinator.Status()
		snapshot.CoordinatorState = string(status.State)
		snapshot.ChainCount = status.ChainCount
		snapshot.HypothesisCount = status.HypothesisCount
		snapshot.RoutineCount = status.RoutineCount
		snapshot.CognitiveMetrics = e.metrics.snapshot()
	}
	if (e.routineConfig.Enabled || e.deviationConfig.Enabled) && e.coordinator != nil {
		status := e.coordinator.Status()
		snapshot.CoordinatorState = string(status.State)
		snapshot.ChainCount = status.ChainCount
		snapshot.HypothesisCount = status.HypothesisCount
		snapshot.RoutineCount = status.RoutineCount
		snapshot.CognitiveMetrics = e.metrics.snapshot()
	}
	e.populateFieldTrialSnapshot(&snapshot)
	return snapshot, nil
}

func (e *ShadowEngine) Explain(ctx context.Context, situationID string) (Explanation, error) {
	if err := contextErr(ctx); err != nil {
		return Explanation{}, err
	}
	explanation := Explanation{SituationID: situationID}
	if e != nil {
		explanation.ContextEnabled = e.contextConfig.Enabled
		explanation.ContextAllowPartial = e.contextConfig.AllowPartial
		explanation.ContextSchemaVersion = cgecontext.SchemaVersionCurrent.String()
		explanation.RoutineLearningEnabled = e.routineConfig.Enabled
		explanation.RoutineTemporalBucketMinutes = e.routineConfig.TemporalBucketMinutes
		explanation.RoutineAllowPartialContext = e.routineConfig.AllowPartialContext
		explanation.RoutineMaxTransitionGap = e.routineConfig.MaxTransitionGap
		explanation.RoutineRequireSameTopologyRevision = e.routineConfig.RequireSameTopologyRevision
		explanation.DeviationEnabled = e.deviationConfig.Enabled
		explanation.DeviationPolicyNamespace = e.deviationConfig.Policy.Namespace
		explanation.DeviationPolicyVersion = e.deviationConfig.Policy.Version
		explanation.DeviationRecentAssessmentLimit = e.deviationConfig.RecentAssessmentLimit
		explanation.DeviationMaxAssessmentsPerObservation = e.deviationConfig.MaxAssessmentsPerObservation
		explanation.DeviationAssessmentStoreCount = e.deviationStore.Count()
	}
	if e != nil && e.orchestrator != nil {
		explanation.CognitiveShadowEnabled = true
		explanation.AutoEvidenceEnabled = e.orchestrator.config.AutoApplyDecisiveEvidence
		explanation.MaxEvidenceReevaluations = e.orchestrator.config.MaxEvidenceReevaluationsPerObservation
		status := e.coordinator.Status()
		explanation.CoordinatorState = string(status.State)
		explanation.ChainCount = status.ChainCount
		explanation.HypothesisCount = status.HypothesisCount
		explanation.RoutineCount = status.RoutineCount
		explanation.CognitiveMetrics = e.metrics.snapshot()
	}
	if e != nil && (e.routineConfig.Enabled || e.deviationConfig.Enabled) && e.coordinator != nil {
		status := e.coordinator.Status()
		explanation.CoordinatorState = string(status.State)
		explanation.ChainCount = status.ChainCount
		explanation.HypothesisCount = status.HypothesisCount
		explanation.RoutineCount = status.RoutineCount
		explanation.CognitiveMetrics = e.metrics.snapshot()
	}
	e.populateFieldTrialExplanation(&explanation)
	return explanation, nil
}

func (e *ShadowEngine) populateFieldTrialSnapshot(snapshot *Snapshot) {
	if e == nil || snapshot == nil {
		return
	}
	snapshot.FieldTrialEnabled = e.fieldTrialConfig.Enabled
	snapshot.FieldTrialState = "disabled"
	if e.trialRecorder == nil {
		return
	}
	stats := e.trialRecorder.Stats()
	manifest := e.trialRecorder.Manifest()
	snapshot.FieldTrialSessionOpen = manifest.Status == fieldtrial.SessionOpen || manifest.Status == fieldtrial.SessionRecovered
	snapshot.FieldTrialEventCount = manifest.EventCount
	snapshot.FieldTrialSegmentCount = manifest.SegmentCount
	snapshot.FieldTrialBytes = stats.EventsBytes + stats.AnnotationBytes
	snapshot.FieldTrialState = string(e.trialRecorder.Status())
	snapshot.FieldTrialErrors = stats.RecordErrors + stats.RecoveryErrors + stats.SyncErrors
	snapshot.FieldTrialLastSequence = manifest.LastSequence
}

func (e *ShadowEngine) populateFieldTrialExplanation(explanation *Explanation) {
	if e == nil || explanation == nil {
		return
	}
	explanation.FieldTrialEnabled = e.fieldTrialConfig.Enabled
	explanation.FieldTrialState = "disabled"
	if e.trialRecorder == nil {
		return
	}
	stats := e.trialRecorder.Stats()
	manifest := e.trialRecorder.Manifest()
	explanation.FieldTrialSessionOpen = manifest.Status == fieldtrial.SessionOpen || manifest.Status == fieldtrial.SessionRecovered
	explanation.FieldTrialEventCount = manifest.EventCount
	explanation.FieldTrialSegmentCount = manifest.SegmentCount
	explanation.FieldTrialBytes = stats.EventsBytes + stats.AnnotationBytes
	explanation.FieldTrialState = string(e.trialRecorder.Status())
	explanation.FieldTrialErrors = stats.RecordErrors + stats.RecoveryErrors + stats.SyncErrors
	explanation.FieldTrialLastSequence = manifest.LastSequence
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

var _ CognitiveEngine = (*ShadowEngine)(nil)
