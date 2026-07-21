package cge

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/durable"
	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/decisioncomparison"
	"synora/internal/cge/deviation"
	"synora/internal/cge/fieldtrial"
	"synora/internal/cge/routines"
	"synora/internal/cge/shadowworkflow"
)

var (
	ErrShadowAdaptation = errors.New("cge_shadow_adaptation_failed")
	ErrShadowPlanning   = errors.New("cge_shadow_planning_failed")
	ErrShadowApply      = errors.New("cge_shadow_apply_failed")
	ErrShadowPanic      = errors.New("cge_shadow_panic_recovered")
)

// Logger is the minimal structured-safe logging boundary used by shadow.
type Logger interface {
	Printf(string, ...any)
}

type shadowError struct {
	code  string
	stage string
	err   error
}

func (e shadowError) Error() string { return e.code }
func (e shadowError) Unwrap() error { return e.err }
func (e shadowError) Code() string  { return e.code }

// ErrorCode returns a stable non-sensitive diagnostic code.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var coded interface{ Code() string }
	if errors.As(err, &coded) {
		return coded.Code()
	}
	if errors.Is(err, ErrShadowAdaptation) {
		return "adaptation_error"
	}
	if errors.Is(err, ErrShadowPlanning) {
		return "planning_error"
	}
	if errors.Is(err, ErrShadowApply) {
		return "apply_error"
	}
	if errors.Is(err, ErrShadowPanic) {
		return "panic_recovered"
	}
	if errors.Is(err, shadowworkflow.ErrComparisonBuildFailed) {
		return "comparison_build_failed"
	}
	if errors.Is(err, ErrInvalidShadowConfig) {
		return "invalid_config"
	}
	if errors.Is(err, ErrShadowStartup) {
		return "startup_error"
	}
	return "shadow_error"
}

// ConfiguredShadowEngine is created only for enabled shadow operation. It
// owns a durable coordinator but never exposes its registry or chains.
func NewShadowEngineWithConfig(ctx context.Context, config ShadowConfig, clock Clock, logger Logger) (*ShadowEngine, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !config.Enabled {
		return NewShadowEngine(), nil
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrShadowStartup)
	}
	if clock == nil {
		return nil, fmt.Errorf("%w: clock is required", ErrShadowStartup)
	}
	if logger == nil {
		return nil, fmt.Errorf("%w: logger is required", ErrShadowStartup)
	}
	fileJournal, err := journal.NewFileJournal(config.JournalPath, journal.FileJournalOptions{CreateParentDirs: true})
	if err != nil {
		return nil, fmt.Errorf("%w: journal configuration", ErrShadowStartup)
	}
	coordinator, err := openShadowCoordinator(ctx, config, clock, fileJournal)
	if err != nil {
		return nil, err
	}
	allowlist := make(map[string]struct{}, len(config.EligibleEventTypes))
	for _, eventType := range config.EligibleEventTypes {
		allowlist[eventType] = struct{}{}
	}
	metrics := &shadowMetrics{}
	engine := &ShadowEngine{
		coordinator: coordinator, policy: config.AssociationPolicy, evidencePolicy: config.EvidencePolicy, allowlist: allowlist,
		actor: config.Actor, clock: clock, logger: logger, metrics: metrics,
		dataDir:          config.DataDir,
		contextConfig:    config.Context,
		routineConfig:    config.Routines,
		deviationConfig:  config.Deviation,
		fieldTrialConfig: config.FieldTrial,
		topologyProvider: unavailableRoutineTopologyProvider{},
	}
	engine.deviationStore, err = NewRecentDeviationStore(config.Deviation.RecentAssessmentLimit)
	if err != nil {
		return nil, fmt.Errorf("%w: deviation store", ErrShadowStartup)
	}
	if config.Context.Enabled {
		// Keep the Core topology behind the adapter boundary. Until a stable
		// read-only source is supplied, the event node and explicit timezone
		// form a valid partial frame without inventing topology data.
		engine.contextProvider = cgecontext.StaticProvider{Timezone: config.Context.Timezone, Occupancy: cgecontext.OccupancyUnknown, HouseMode: cgecontext.HouseModeUnknown, AllowPartial: config.Context.AllowPartial}
	}
	if config.FieldTrial.TopologyFile != "" {
		topology, topologyErr := fieldtrial.LoadTopologyFile(config.FieldTrial.TopologyFile)
		if topologyErr != nil {
			metrics.cognitive("field_trial_topology_errors")
		} else {
			engine.topologyProvider = StaticRoutineTopologyProvider{Topology: topology, Available: true}
			if config.Context.Enabled {
				engine.contextProvider = cgecontext.StaticProvider{Topology: topology, Timezone: config.Context.Timezone, Occupancy: cgecontext.OccupancyUnknown, HouseMode: cgecontext.HouseModeUnknown, AllowPartial: config.Context.AllowPartial}
			}
			metrics.cognitive("field_trial_topology_loaded")
		}
	}
	if config.Cognitive.Enabled {
		engine.orchestrator = newShadowOrchestrator(coordinator, config, clock, metrics)
	}
	if config.Workflow.Enabled {
		workflowRuntime, workflowErr := shadowworkflow.NewRuntime(ctx, config.Workflow, clock, logger, nil, nil)
		engine.workflow = workflowRuntime
		if workflowErr != nil {
			engine.safeLog("workflow_recovery_failed")
		}
	}
	if config.FieldTrial.Enabled {
		recorder, recorderErr := fieldtrial.Open(ctx, config.FieldTrial, fieldTrialMetadata(config))
		if recorderErr != nil {
			metrics.cognitive("field_trial_record_errors")
			engine.safeLog("field_trial_recorder_unavailable")
		} else {
			engine.trialRecorder = recorder
			metrics.fieldTrialDelta(fieldtrial.Stats{}, recorder.Stats())
		}
	}
	return engine, nil
}

func openShadowCoordinator(ctx context.Context, config ShadowConfig, clock Clock, source *journal.FileJournal) (*durable.Coordinator, error) {
	manifestPath := filepath.Join(config.DataDir, "manifest.json")
	manifestInfo, manifestErr := os.Stat(manifestPath)
	if config.JournalOnlyRecovery {
		manifestErr = os.ErrNotExist
	}
	if manifestErr == nil {
		if manifestInfo.IsDir() {
			return nil, fmt.Errorf("%w: manifest is a directory", ErrShadowStartup)
		}
		store, err := generations.NewStore(config.DataDir, generations.StoreOptions{})
		if err != nil {
			return nil, fmt.Errorf("%w: generation store", ErrShadowStartup)
		}
		coordinator, _, err := durable.FromGenerationManifest(ctx, store, source)
		if err != nil {
			return nil, fmt.Errorf("%w: manifest recovery", ErrShadowStartup)
		}
		return coordinator, nil
	}
	if !errors.Is(manifestErr, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: inspect manifest", ErrShadowStartup)
	}

	journalInfo, journalErr := os.Stat(config.JournalPath)
	if journalErr == nil {
		if journalInfo.IsDir() {
			return nil, fmt.Errorf("%w: journal is a directory", ErrShadowStartup)
		}
		coordinator, _, err := durable.FromJournal(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("%w: journal recovery", ErrShadowStartup)
		}
		return coordinator, nil
	}
	if !errors.Is(journalErr, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: inspect journal", ErrShadowStartup)
	}
	if !config.InitializeIfMissing {
		return nil, fmt.Errorf("%w: journal initialization is disabled", ErrShadowStartup)
	}
	now := clock.Now().UTC()
	if now.IsZero() {
		return nil, fmt.Errorf("%w: clock returned zero time", ErrShadowStartup)
	}
	if _, err := source.Initialize(ctx, journal.GenesisInput{
		JournalID: config.JournalID, CreatedAt: now, RecordedAt: now,
		Purpose: "cognitive graph shadow journal", Actor: config.Actor,
		CorrelationID: "cge-shadow:journal-genesis",
	}); err != nil {
		return nil, fmt.Errorf("%w: initialize journal", ErrShadowStartup)
	}
	coordinator, _, err := durable.FromJournal(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("%w: reload initialized journal", ErrShadowStartup)
	}
	return coordinator, nil
}

// Observe performs the explicit post-history shadow flow. It recovers its own
// panic so callers never inherit shadow failures.
func (e *ShadowEngine) observeRuntime(ctx context.Context, event Event, historical *decisioncomparison.HistoricalDecisionRef) (result ObservationResult, err error) {
	var trialObservation chains.ObservationRef
	if e.trialRecorder != nil {
		started := time.Now()
		beforeMetrics := e.metrics.snapshot()
		defer func() {
			e.recordTrialEvent(ctx, event, trialObservation, started, beforeMetrics)
		}()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			e.metrics.panicRecovered(e.shadowNow())
			if e.orchestrator != nil {
				e.metrics.cognitive("orchestration_panics")
			}
			e.safeLog("panic_recovered")
			result = ObservationResult{ObservedAt: event.Timestamp, ObservationCount: result.ObservationCount, LastEventType: event.Type}
			err = ErrShadowPanic
		}
	}()
	now := e.shadowNow()
	result.ObservedAt = event.Timestamp
	result.LastEventType = event.Type
	result.ObservationCount = e.metrics.observed(now)
	e.mu.Lock()
	e.observationCount = result.ObservationCount
	e.lastObservedAt = event.Timestamp
	e.lastEventType = event.Type
	e.mu.Unlock()
	adapted, adaptErr := AdaptEventWithAllowlist(event, mapKeys(e.allowlist))
	if adaptErr != nil {
		e.metrics.malformed(now, ErrorCode(adaptErr))
		e.safeLog(ErrorCode(adaptErr))
		return result, adaptErr
	}
	if !adapted.Eligible {
		e.metrics.skipped()
		return result, nil
	}
	e.metrics.eligible()
	if e.contextProvider != nil {
		e.metrics.cognitive("context_resolution_attempted")
		frame, contextErr := e.resolveContext(ctx, adapted.Input.Observation)
		if contextErr != nil {
			e.metrics.cognitive("context_resolution_errors")
			e.metrics.cognitive("context_resolution_missing")
		} else {
			adapted.Input.Observation.Context = &frame
			if frame.Quality == cgecontext.QualityComplete {
				e.metrics.cognitive("context_resolution_complete")
			} else {
				e.metrics.cognitive("context_resolution_partial")
			}
			if frame.NodeKind == cgecontext.NodeUnknown {
				e.metrics.cognitive("context_topology_unknown_node")
			}
		}
	}
	if e.workflow != nil {
		e.submitWorkflow(adapted.Input.Observation, historical)
	}
	trialObservation = adapted.Input.Observation.Clone()
	e.mu.Lock()
	e.lastOrchestration = ShadowOrchestrationResult{}
	e.lastAssessment = nil
	e.lastDeviation = ShadowDeviationResult{HighestBand: deviation.BandUnknown}
	e.mu.Unlock()
	if e.coordinator.Status().State == durable.StateDegraded {
		e.metrics.degraded(now)
		if e.orchestrator != nil {
			e.metrics.cognitive("orchestration_degraded")
		}
		e.safeLog("coordinator_degraded")
		return result, shadowError{code: "coordinator_degraded", stage: "plan", err: durable.ErrCoordinatorDegraded}
	}
	if e.orchestrator != nil {
		orchestrationResult, orchestrationErr := e.orchestrator.ProcessObservation(ctx, adapted.Input.Observation, adapted.Input.SituationKind)
		e.mu.Lock()
		e.lastOrchestration = orchestrationResult
		e.mu.Unlock()
		if orchestrationErr != nil {
			return result, shadowError{code: ErrorCode(orchestrationErr), stage: "cognitive_orchestration", err: orchestrationErr}
		}
		e.mu.Lock()
		e.lastOrchestration = orchestrationResult
		e.mu.Unlock()
		if (e.routineConfig.Enabled || e.deviationConfig.Enabled) && orchestrationResult.ChainID != "" {
			deviationResult, routineErr := e.learnRoutines(ctx, adapted.Input.Observation, orchestrationResult.ChainID, now)
			orchestrationResult.Deviation = deviationResult
			e.mu.Lock()
			e.lastOrchestration = orchestrationResult
			e.mu.Unlock()
			e.setLastDeviationResult(deviationResult)
			if routineErr != nil {
				e.safeLog(ErrorCode(routineErr))
				if e.coordinator.Status().State == durable.StateDegraded {
					return result, shadowError{code: "routine_orchestration_degraded", stage: "routine_learning", err: routineErr}
				}
			}
		}
		return result, nil
	}
	plannedAt := e.shadowNow()
	plan, planErr := e.coordinator.PlanAssociation(adapted.Input, plannedAt, e.policy)
	if planErr != nil {
		e.metrics.planningError(now, ErrorCode(planErr))
		e.safeLog(ErrorCode(planErr))
		return result, shadowError{code: "planning_error", stage: "plan", err: errors.Join(ErrShadowPlanning, planErr)}
	}
	e.metrics.plan(string(plan.Decision))
	if plan.Decision == association.DecisionAmbiguous || plan.Decision == association.DecisionAlreadyAttached {
		if plan.Decision == association.DecisionAlreadyAttached {
			e.metrics.applied(string(plan.Decision), now, true)
			if (e.routineConfig.Enabled || e.deviationConfig.Enabled) && plan.SelectedChainID != "" {
				deviationResult, routineErr := e.learnRoutines(ctx, adapted.Input.Observation, plan.SelectedChainID, now)
				e.setLastDeviationResult(deviationResult)
				if routineErr != nil {
					e.safeLog(ErrorCode(routineErr))
				}
			}
		}
		return result, nil
	}
	correlationID := "cge-shadow:" + adapted.Input.Observation.ID
	applyAt := e.shadowNow()
	applyResult, applyErr := e.coordinator.ApplyAssociationPlan(ctx, plan, e.actor, correlationID, applyAt, applyAt)
	if applyErr != nil {
		if e.coordinator.Status().State == durable.StateDegraded {
			e.metrics.degraded(now)
		}
		e.metrics.applyError(now, ErrorCode(applyErr))
		e.safeLog(ErrorCode(applyErr))
		return result, shadowError{code: "apply_error", stage: "apply", err: errors.Join(ErrShadowApply, applyErr)}
	}
	e.metrics.applied(string(plan.Decision), now, applyResult.Idempotent)
	if (e.routineConfig.Enabled || e.deviationConfig.Enabled) && applyResult.ChainID != "" {
		deviationResult, routineErr := e.learnRoutines(ctx, adapted.Input.Observation, applyResult.ChainID, now)
		e.setLastDeviationResult(deviationResult)
		if routineErr != nil {
			e.safeLog(ErrorCode(routineErr))
		}
	}
	return result, nil
}

func (e *ShadowEngine) setLastDeviationResult(result ShadowDeviationResult) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.lastDeviation = result
	e.mu.Unlock()
}

func (e *ShadowEngine) learnRoutines(ctx context.Context, observation chains.ObservationRef, chainID chains.ChainID, plannedAt time.Time) (ShadowDeviationResult, error) {
	var deviationResult ShadowDeviationResult
	if e == nil || (!e.routineConfig.Enabled && !e.deviationConfig.Enabled) || e.coordinator == nil {
		return deviationResult, nil
	}
	chainSnapshot, err := e.coordinator.Get(chainID)
	if err != nil {
		e.metrics.cognitive("routine_learning_errors")
		return deviationResult, err
	}
	topology := cgecontext.TopologySnapshot{}
	if e.topologyProvider != nil {
		resolved, available, providerErr := e.topologyProvider.CurrentTopology(ctx)
		if providerErr != nil {
			e.metrics.cognitive("routine_learning_errors")
		} else if available {
			topology = resolved
		}
	}
	policy := routines.ExtractionPolicy{Namespace: "synora.cge.routines", Version: "routine-extraction-v1", TemporalBucketMinutes: e.routineConfig.TemporalBucketMinutes, AllowPartialContext: e.routineConfig.AllowPartialContext, MaxTransitionGap: e.routineConfig.MaxTransitionGap, RequireSameTopologyRevision: e.routineConfig.RequireSameTopologyRevision}
	plan, err := routines.PlanLearning(chainSnapshot, observation.ID, topology, plannedAt, policy)
	if err != nil {
		e.metrics.cognitive("routine_learning_errors")
		return deviationResult, err
	}
	e.metrics.cognitive("routine_learning_planned")
	for _, skipped := range plan.Skipped {
		e.metrics.cognitive("routine_learning_skipped")
		switch skipped.Code {
		case routines.SkipContextMissing:
			e.metrics.cognitive("routine_context_missing")
		case routines.SkipPartialDisallowed:
			e.metrics.cognitive("routine_partial_disallowed")
		case routines.SkipPreviousContextMissing:
			e.metrics.cognitive("routine_previous_context_missing")
		case routines.SkipTransitionGapExceeded:
			e.metrics.cognitive("routine_transition_gap_exceeded")
		case routines.SkipTopologyMissing:
			e.metrics.cognitive("routine_topology_missing")
		case routines.SkipTopologyRevisionMismatch:
			e.metrics.cognitive("routine_topology_revision_mismatch")
		case routines.SkipTransitionUnreachable:
			e.metrics.cognitive("routine_transition_unreachable")
		}
	}
	for _, occurrence := range plan.Occurrences {
		if occurrence.Kind == routines.KindPresence {
			e.metrics.cognitive("routine_presence_extracted")
		} else {
			e.metrics.cognitive("routine_transition_extracted")
		}
	}
	if e.deviationConfig.Enabled {
		deviationResult = e.evaluateRoutineDeviation(ctx, plan, plannedAt)
	}
	if !e.routineConfig.Enabled {
		return deviationResult, nil
	}
	learning := e.coordinator.ApplyRoutineLearningPlan(ctx, plan, e.actor, "cge-shadow:"+observation.ID+":routine")
	for _, item := range learning.Results {
		switch {
		case item.ErrorCode != "":
			e.metrics.cognitive("routine_learning_errors")
		case item.Idempotent:
			e.metrics.cognitive("routine_occurrence_idempotent")
		case item.Created:
			e.metrics.cognitive("routine_created")
		case item.Applied:
			e.metrics.cognitive("routine_occurrence_added")
		}
	}
	if e.coordinator.Status().State == durable.StateDegraded {
		e.metrics.cognitive("routine_orchestration_degraded")
		return deviationResult, durable.ErrCoordinatorDegraded
	}
	return deviationResult, nil
}

// evaluateRoutineDeviation evaluates the already extracted plan against the
// pre-learning, subject-scoped baseline. It is deliberately read-only; an
// evaluation error is recorded and does not prevent descriptive learning.
func (e *ShadowEngine) evaluateRoutineDeviation(ctx context.Context, plan routines.LearningPlan, evaluatedAt time.Time) ShadowDeviationResult {
	result := ShadowDeviationResult{HighestBand: deviation.BandUnknown}
	if e == nil || !e.deviationConfig.Enabled {
		return result
	}
	if len(plan.Occurrences) == 0 {
		e.metrics.cognitive("deviation_skipped_no_occurrence")
		return result
	}
	result.Attempted = true
	e.metrics.cognitive("deviation_evaluation_attempted")
	if evaluatedAt.IsZero() {
		evaluatedAt = plan.Occurrences[0].ObservedAt
	}
	for _, occurrence := range plan.Occurrences {
		if evaluatedAt.Before(occurrence.ObservedAt) {
			evaluatedAt = occurrence.ObservedAt
		}
	}
	limited := append([]routines.Occurrence(nil), plan.Occurrences...)
	if len(limited) > e.deviationConfig.MaxAssessmentsPerObservation {
		limited = limited[:e.deviationConfig.MaxAssessmentsPerObservation]
	}
	plan.Occurrences = limited
	candidates := make(map[routines.OccurrenceID][]routines.Snapshot, len(limited))
	for _, occurrence := range limited {
		baseline, err := e.coordinator.ListRoutinesBySubjectAndKind(occurrence.Subject, occurrence.Kind)
		if err != nil {
			result.ErrorCode = deviationErrorCode(err)
			e.metrics.deviationError(e.shadowNow(), result.ErrorCode)
			continue
		}
		candidates[occurrence.ID] = baseline
	}
	assessmentPlan, err := deviation.EvaluateLearningPlan(plan, candidates, evaluatedAt, e.deviationConfig.Policy)
	if err != nil {
		result.ErrorCode = deviationErrorCode(err)
		e.metrics.deviationError(e.shadowNow(), result.ErrorCode)
		if errors.Is(err, deviation.ErrCandidateLimitExceeded) {
			e.metrics.cognitive("deviation_candidate_limit_exceeded")
		}
		return result
	}
	result.Completed = true
	result.AssessmentCount = len(assessmentPlan.Assessments)
	e.metrics.cognitive("deviation_evaluation_completed")
	lowestSet := false
	for _, assessment := range assessmentPlan.Assessments {
		summary := deviationSummary(assessment)
		e.mu.Lock()
		e.lastAssessment = &summary
		e.mu.Unlock()
		switch assessment.Status {
		case deviation.StatusEvaluated:
			e.metrics.cognitive("deviation_evaluated")
		case deviation.StatusPartial:
			e.metrics.cognitive("deviation_partial")
		case deviation.StatusInsufficientHistory:
			e.metrics.cognitive("deviation_insufficient_history")
		case deviation.StatusAmbiguous:
			e.metrics.cognitive("deviation_ambiguous")
		case deviation.StatusAlreadyEvaluated:
			e.metrics.cognitive("deviation_already_evaluated")
		case deviation.StatusNotApplicable:
			e.metrics.cognitive("deviation_not_applicable")
		}
		switch assessment.Band {
		case deviation.BandAligned:
			e.metrics.cognitive("deviation_band_aligned")
		case deviation.BandLow:
			e.metrics.cognitive("deviation_band_low")
		case deviation.BandModerate:
			e.metrics.cognitive("deviation_band_moderate")
		case deviation.BandHigh:
			e.metrics.cognitive("deviation_band_high")
		default:
			e.metrics.cognitive("deviation_band_unknown")
		}
		if bandRank(assessment.Band) > bandRank(result.HighestBand) {
			result.HighestBand = assessment.Band
		}
		if assessment.Score > result.HighestScore {
			result.HighestScore = assessment.Score
		}
		if assessment.Status != deviation.StatusAlreadyEvaluated && (!lowestSet || assessment.Coverage < result.LowestCoverage) {
			result.LowestCoverage = assessment.Coverage
			lowestSet = true
		}
		if e.deviationStore != nil && e.deviationStore.limit > 0 {
			evicted, storeErr := e.deviationStore.add(assessment)
			if storeErr != nil {
				result.ErrorCode = "deviation_store_error"
				e.metrics.deviationError(e.shadowNow(), result.ErrorCode)
			} else {
				e.metrics.cognitive("deviation_store_added")
				if evicted {
					e.metrics.cognitive("deviation_store_evicted")
				}
			}
		} else {
			e.metrics.cognitive("deviation_store_disabled")
		}
	}
	for _, occurrence := range plan.Occurrences {
		switch occurrence.ContextQuality {
		case cgecontext.QualityComplete:
			e.metrics.cognitive("deviation_context_complete")
		case cgecontext.QualityPartial:
			e.metrics.cognitive("deviation_context_partial")
		default:
			e.metrics.cognitive("deviation_context_unknown")
		}
	}
	return result
}

func bandRank(band deviation.Band) int {
	switch band {
	case deviation.BandAligned:
		return 1
	case deviation.BandLow:
		return 2
	case deviation.BandModerate:
		return 3
	case deviation.BandHigh:
		return 4
	default:
		return 0
	}
}

func deviationErrorCode(err error) string {
	switch {
	case errors.Is(err, deviation.ErrCandidateLimitExceeded):
		return "baseline.candidate_limit_exceeded"
	case errors.Is(err, deviation.ErrInvalidDeviationPolicy):
		return "invalid_deviation_policy"
	case errors.Is(err, deviation.ErrInvalidTimestamp):
		return "invalid_timestamp"
	default:
		return "deviation_evaluation_error"
	}
}

func (e *ShadowEngine) resolveContext(ctx context.Context, observation chains.ObservationRef) (frame cgecontext.Frame, err error) {
	defer func() {
		if recover() != nil {
			err = ErrShadowPanic
		}
	}()
	return e.contextProvider.Resolve(ctx, observation.ID, observation.Timestamp, observation.NodeID)
}

func mapKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	// The adapter only performs membership checks; sorting makes tests and
	// diagnostics deterministic without retaining caller-owned configuration.
	sort.Strings(result)
	return result
}

func (e *ShadowEngine) safeLog(code string) {
	if e.logger != nil {
		defer func() { _ = recover() }()
		e.logger.Printf("cge shadow code=%s", code)
	}
}

func (e *ShadowEngine) shadowNow() (now time.Time) {
	defer func() { _ = recover() }()
	if e == nil || e.clock == nil {
		return time.Time{}
	}
	return e.clock.Now().UTC()
}

// Metrics returns a defensive in-memory metric snapshot.
func (e *ShadowEngine) Metrics() MetricsSnapshot {
	if e == nil || e.metrics == nil {
		return MetricsSnapshot{}
	}
	return e.metrics.snapshot()
}

// Close closes the coordinator once and never creates a snapshot.
func (e *ShadowEngine) Close() error {
	if e == nil {
		return nil
	}
	e.closeOnce.Do(func() {
		if e.workflow != nil {
			if err := e.workflow.Close(context.Background()); err != nil {
				e.safeLog("workflow_close_error")
			}
		}
		if e.coordinator != nil {
			e.closeErr = e.coordinator.Close()
		}
		if e.trialRecorder != nil {
			if err := e.trialRecorder.Close(context.Background(), e.shadowNow()); err != nil {
				e.metrics.cognitive("field_trial_record_errors")
				e.safeLog("field_trial_close_error")
			}
		}
	})
	return e.closeErr
}

// Keep standard log import available for runtime composition helpers.
var _ Logger = (*log.Logger)(nil)
