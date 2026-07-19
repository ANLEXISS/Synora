package demo

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
)

// ScenarioRunner is a presentation-layer controller. It never writes a CGE
// snapshot and never supplies an expected value to the engine.
type ScenarioRunner struct {
	mu           sync.Mutex
	session      *LiveSession
	scenario     Scenario
	currentStep  int
	status       ScenarioRunStatus
	results      []ScenarioStepResult
	modified     bool
	startedAt    time.Time
	memoryMatrix *MemoryFieldMatrix
}

func NewScenarioRunner(session *LiveSession, scenario Scenario) (*ScenarioRunner, error) {
	if session == nil {
		return nil, errors.New("scenario_session_required")
	}
	if err := scenario.ValidateAgainstTopology(session.topologyForScenario()); err != nil {
		return nil, err
	}
	return &ScenarioRunner{session: session, scenario: cloneScenario(scenario), status: ScenarioReady}, nil
}

func (r *ScenarioRunner) State() ScenarioRunState {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matrix *MemoryFieldMatrix
	if r.memoryMatrix != nil {
		copy := *r.memoryMatrix
		copy.Rows = append([]MemoryFieldMatrixRow(nil), r.memoryMatrix.Rows...)
		matrix = &copy
	}
	return ScenarioRunState{Scenario: cloneScenario(r.scenario), CurrentStep: r.currentStep, Status: r.status, Modified: r.modified, Results: cloneStepResults(r.results), MemoryMatrix: matrix}
}

func (r *ScenarioRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scenario.UnsupportedReason != nil {
		r.status = ScenarioFailed
		return errors.New("scenario_unsupported")
	}
	if err := r.session.Reset(); err != nil {
		r.status = ScenarioFailed
		return err
	}
	if r.scenario.InitialState.Mode == InitialStateGeneratedBaseline && r.scenario.InitialState.BaselineDays > 0 {
		if err := r.session.LoadBaseline(r.scenario.InitialState.BaselineDays); err != nil {
			r.status = ScenarioFailed
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		r.status = ScenarioCancelled
		return err
	}
	r.currentStep = 0
	r.results = nil
	r.modified = false
	r.memoryMatrix = nil
	if r.scenario.MemoryFieldIsolation != nil {
		matrix, matrixErr := r.session.RunMemoryFieldIsolation(ctx, *r.scenario.MemoryFieldIsolation)
		if matrixErr != nil {
			r.status = ScenarioFailed
			return matrixErr
		}
		r.memoryMatrix = &matrix
	}
	r.startedAt = time.Now().UTC()
	r.status = ScenarioRunning
	return nil
}

func (r *ScenarioRunner) Next(ctx context.Context) (ScenarioStepResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == ScenarioReady {
		return ScenarioStepResult{}, errors.New("scenario_not_started")
	}
	if r.status == ScenarioPaused {
		return ScenarioStepResult{}, errors.New("scenario_paused")
	}
	if r.status != ScenarioRunning {
		return ScenarioStepResult{}, fmt.Errorf("scenario_not_running: %s", r.status)
	}
	if r.currentStep >= len(r.scenario.Steps) {
		r.status = ScenarioCompleted
		return ScenarioStepResult{}, errors.New("scenario_complete")
	}
	step := r.scenario.Steps[r.currentStep]
	result := ScenarioStepResult{ScenarioID: r.scenario.ID, StepID: step.ID, StepIndex: r.currentStep, StartedAt: time.Now().UTC(), Expected: append([]ExpectedProperty(nil), step.Expected...)}
	result.Modified = r.modified
	var err error
	switch step.Kind {
	case StepInjectEvent:
		result, err = r.executeEvent(ctx, step, result)
	case StepAdvanceTime:
		var state LiveState
		if step.AdvanceTime.At != nil {
			state, err = r.session.Advance(LiveAdvanceRequest{At: *step.AdvanceTime.At})
		} else {
			state, err = r.session.Advance(LiveAdvanceRequest{Minutes: step.AdvanceTime.Minutes})
		}
		result.Observed = []ObservedProperty{{Code: "clock.advanced", Scope: ExpectedScopeCoordinatorState, Value: state.SimulatedAt}}
	case StepRepeatEvent:
		result, err = r.executeRepeat(ctx, step, result)
	case StepRestartEngine:
		var replay map[string]any
		replay, err = r.session.Restart()
		if err == nil {
			result.Observed = []ObservedProperty{{Code: "replay.digest_equal", Scope: ExpectedScopeReplayEqual, Value: replay["equal"]}, {Code: "chain.count", Scope: ExpectedScopeChainCount, Value: replay["chains"]}, {Code: "routine.count", Scope: ExpectedScopeRoutineCount, Value: replay["routines"]}}
		}
	case StepPause:
		r.status = ScenarioPaused
		result.Observed = []ObservedProperty{{Code: "scenario.paused", Scope: ExpectedScopeCoordinatorState, Value: string(ScenarioPaused)}}
	case StepCheckpoint:
		var checkpoint map[string]any
		checkpoint, err = r.session.Checkpoint(ctx)
		if err == nil {
			result.Observed = []ObservedProperty{{Code: "checkpoint.created", Scope: ExpectedScopeWALDelta, Value: checkpoint["journal_sequence"]}}
		}
	default:
		err = errors.New("unsupported_scenario_step")
	}
	result.CompletedAt = time.Now().UTC()
	if err != nil {
		result.Status = StepResultFailed
		result.ErrorCode = scenarioErrorCode(err)
		r.status = ScenarioFailed
	} else if result.Status == "" {
		result.Comparison = comparisons(step.Expected, result.Observed, result.Modified)
		result.Status = resultStatus(result.Comparison)
	}
	r.results = append(r.results, cloneStepResult(result))
	r.currentStep++
	if r.currentStep >= len(r.scenario.Steps) && r.status == ScenarioRunning {
		r.status = ScenarioCompleted
	}
	return result, err
}

func (r *ScenarioRunner) executeEvent(ctx context.Context, step ScenarioStep, result ScenarioStepResult) (ScenarioStepResult, error) {
	input, err := r.inputForEvent(step.Event, 0, "")
	if err != nil {
		return result, err
	}
	before := r.session.stateForScenario()
	value, err := r.session.Submit(input)
	if err != nil {
		return result, err
	}
	result.Injection = &value
	result.Injections = []LiveInjectionResult{value}
	result.Observed = observedForInjection(value, before)
	return result, nil
}

func (r *ScenarioRunner) executeRepeat(ctx context.Context, step ScenarioStep, result ScenarioStepResult) (ScenarioStepResult, error) {
	var source *ScenarioEvent
	for _, candidate := range r.scenario.Steps {
		if candidate.ID == step.Repeat.SourceStepID {
			source = candidate.Event
			break
		}
	}
	if source == nil {
		return result, errors.New("repeat_source_is_not_event")
	}
	values := make([]LiveInjectionResult, 0, step.Repeat.Count)
	for i := 0; i < step.Repeat.Count; i++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		input, err := r.inputForEvent(source, i+1, string(step.Repeat.EventIDMode))
		if err != nil {
			return result, err
		}
		if source.TimestampMode == TimestampRelative {
			input.SimulatedAt = r.session.stateForScenario().SimulatedAt.Add(time.Duration(i+1) * step.Repeat.Interval.Duration())
		} else if source.AbsoluteTime != nil {
			input.SimulatedAt = source.AbsoluteTime.Add(time.Duration(i+1) * step.Repeat.Interval.Duration())
		}
		before := r.session.stateForScenario()
		value, err := r.session.Submit(input)
		if err != nil {
			return result, err
		}
		if i == 0 {
			result.Observed = observedForInjection(value, before)
		}
		values = append(values, value)
	}
	result.Injections = values
	if len(values) > 0 {
		result.Injection = &values[len(values)-1]
	}
	return result, nil
}

func (r *ScenarioRunner) inputForEvent(event *ScenarioEvent, repeat int, mode string) (LiveEventInput, error) {
	if event == nil {
		return LiveEventInput{}, errors.New("scenario_event_missing")
	}
	at := r.session.stateForScenario().SimulatedAt
	if event.TimestampMode == TimestampAbsolute && event.AbsoluteTime != nil {
		at = event.AbsoluteTime.UTC()
	} else {
		at = at.Add(event.RelativeOffset.Duration())
	}
	eventID := event.EventID
	if repeat > 0 {
		switch mode {
		case string(RepeatSameID):
		case string(RepeatSequence):
			eventID += fmt.Sprintf("-seq-%03d", repeat)
		default:
			eventID += fmt.Sprintf("-repeat-%03d", repeat)
		}
	}
	eventType, identity := "vision.unknown", ""
	switch event.Identity.Kind {
	case IdentityKnown:
		eventType, identity = "vision.identity", event.Identity.EntityID
	case IdentityUncertain:
		eventType = "vision.uncertain"
	case IdentityUnknown, IdentityNone:
		eventType = "vision.unknown"
	}
	if event.EventType != "" {
		eventType = event.EventType
	}
	return LiveEventInput{EventID: eventID, EventType: eventType, Identity: identity, IdentityLabel: identity, NodeID: event.NodeID, HouseMode: string(event.HouseMode), Occupancy: string(event.Occupancy), ContextQuality: string(event.ContextQuality), SimulatedAt: at, PrepareAmbiguity: event.PrepareAmbiguity, SequenceKey: "scenario-" + event.EventID, DeviceID: "scenario-camera", TrackID: "scenario-track"}, nil
}

func observedForInjection(value LiveInjectionResult, before LiveState) []ObservedProperty {
	occurrenceAdded := value.Learning.OccurrenceCountAfter > value.Learning.OccurrenceCountBefore
	routineCreated := value.Learning.RoutineCountAfter > value.Learning.RoutineCountBefore
	associationAmbiguous := value.Association.Decision == "ambiguous"
	hypothesisOpened := value.Hypothesis.Action != "" && value.Hypothesis.Action != "none"
	props := []ObservedProperty{
		{Code: "association.decision", Scope: ExpectedScopeAssociationDecision, Value: value.Association.Decision},
		{Code: "association.ambiguous", Scope: ExpectedScopeAssociationAmbiguous, Value: associationAmbiguous},
		{Code: "association.candidate_count", Scope: ExpectedScopeAssociationCandidateCount, Value: len(value.Association.Candidates)},
		{Code: "hypothesis.count", Scope: ExpectedScopeHypothesisCount, Value: value.Hypothesis.Count},
		{Code: "routine.count", Scope: ExpectedScopeRoutineCount, Value: value.GlobalState.RoutineCount},
		{Code: "routine.created", Scope: ExpectedScopeRoutineCreated, Value: routineCreated},
		{Code: "routine.occurrence_count", Scope: ExpectedScopeRoutineOccurrenceCount, Value: value.Learning.OccurrenceCountAfter},
		{Code: "routine.occurrence_added", Scope: ExpectedScopeRoutineOccurrenceAdded, Value: occurrenceAdded},
		{Code: "routine.readiness", Scope: ExpectedScopeRoutineReadiness, Value: value.Deviation.RoutineReady},
		{Code: "chain.count", Scope: ExpectedScopeChainCount, Value: value.GlobalState.ChainCount},
		{Code: "chain.observation_count", Scope: ExpectedScopeObservationCount, Value: value.GlobalState.ObservationCount},
		{Code: "coordinator.state", Scope: ExpectedScopeCoordinatorState, Value: value.GlobalState.CoordinatorState},
		{Code: "deviation.status", Scope: ExpectedScopeDeviationStatus, Value: value.Deviation.Status},
		{Code: "deviation.band", Scope: ExpectedScopeDeviationBand, Value: value.Deviation.Band},
		{Code: "deviation.score", Scope: ExpectedScopeDeviationScore, Value: value.Deviation.Score},
		{Code: "deviation.score_positive", Scope: ExpectedScopeDeviationScorePositive, Value: value.Deviation.Attempted && value.Deviation.Score > 0},
		{Code: "deviation.coverage", Scope: ExpectedScopeDeviationCoverage, Value: value.Deviation.Coverage},
		{Code: "wal.sequence_delta", Scope: ExpectedScopeWALDelta, Value: value.GlobalState.JournalSequence - before.Global.JournalSequence},
	}
	props = append(props, ObservedProperty{Code: "hypothesis.association_opened", Scope: ExpectedScopeHypothesisAssociationOpened, Value: hypothesisOpened})
	for _, factor := range value.Deviation.Factors {
		switch factor.Kind {
		case "structural":
			props = append(props,
				ObservedProperty{Code: "deviation.structural_available", Scope: ExpectedScopeDeviationStructuralAvailable, Value: factor.Available},
				ObservedProperty{Code: "deviation.structural_positive", Scope: ExpectedScopeDeviationStructuralPositive, Value: factor.Available && factor.Score > 0})
		case "temporal":
			props = append(props,
				ObservedProperty{Code: "deviation.temporal_available", Scope: ExpectedScopeDeviationTemporalAvailable, Value: factor.Available},
				ObservedProperty{Code: "deviation.temporal_positive", Scope: ExpectedScopeDeviationTemporalPositive, Value: factor.Available && factor.Score > 0},
				ObservedProperty{Code: "deviation.temporal_score", Scope: ExpectedScopeDeviationTemporalScore, Value: factor.Score})
		case "interval":
			props = append(props,
				ObservedProperty{Code: "deviation.interval_available", Scope: ExpectedScopeDeviationIntervalAvailable, Value: factor.Available},
				ObservedProperty{Code: "deviation.interval_positive", Scope: ExpectedScopeDeviationIntervalPositive, Value: factor.Available && factor.Score > 0})
		}
	}
	return props
}

func resultStatus(values []PropertyComparison) StepResultStatus {
	inconclusive := false
	for _, value := range values {
		if value.Status == ComparisonNotObserved {
			return StepResultUnexpected
		}
		if value.Status == ComparisonInconclusive || value.Status == ComparisonNotApplicable {
			inconclusive = true
		}
	}
	if inconclusive {
		return StepResultInconclusive
	}
	return StepResultCompleted
}
func propertyMatches(expected ExpectedProperty, actual any) bool {
	if expected.Operator == OperatorExists {
		return actual != nil
	}
	if expected.Operator == OperatorNotExists {
		return actual == nil
	}
	if left, ok := numberValue(actual); ok {
		if right, ok := numberValue(expected.Value); ok {
			switch expected.Operator {
			case OperatorEquals:
				return left == right
			case OperatorNotEquals:
				return left != right
			case OperatorGreaterThan:
				return left > right
			case OperatorGreaterOrEqual:
				return left >= right
			case OperatorLessThan:
				return left < right
			}
		}
	}
	if expected.Operator == OperatorOneOf {
		if values, ok := expected.Value.([]any); ok {
			for _, item := range values {
				if reflect.DeepEqual(item, actual) || fmt.Sprint(item) == fmt.Sprint(actual) {
					return true
				}
			}
			return false
		}
		if values, ok := expected.Value.([]string); ok {
			for _, item := range values {
				if item == fmt.Sprint(actual) {
					return true
				}
			}
			return false
		}
	}
	if expected.Operator == OperatorEquals {
		return reflect.DeepEqual(expected.Value, actual) || fmt.Sprint(expected.Value) == fmt.Sprint(actual)
	}
	if expected.Operator == OperatorNotEquals {
		return !propertyMatches(ExpectedProperty{Operator: OperatorEquals, Value: expected.Value}, actual)
	}
	return false
}
func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	case uint16:
		return float64(v), true
	case float64:
		return v, true
	case jsonNumber:
		return 0, false
	}
	return 0, false
}

type jsonNumber string

func scenarioErrorCode(err error) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(err.Error())), " ", "_")
}

func (r *ScenarioRunner) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == ScenarioRunning {
		r.status = ScenarioPaused
	}
}
func (r *ScenarioRunner) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == ScenarioPaused {
		r.status = ScenarioRunning
	}
}
func (r *ScenarioRunner) Cancel() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status == ScenarioRunning || r.status == ScenarioPaused {
		r.status = ScenarioCancelled
	}
}
func (r *ScenarioRunner) Reset(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.session.Reset(); err != nil {
		return err
	}
	r.currentStep = 0
	r.results = nil
	r.modified = false
	r.memoryMatrix = nil
	r.status = ScenarioReady
	return nil
}
func (r *ScenarioRunner) RunToEnd(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		r.mu.Lock()
		status, index := r.status, r.currentStep
		r.mu.Unlock()
		if status == ScenarioCompleted || status == ScenarioCancelled || status == ScenarioFailed || index >= len(r.scenario.Steps) {
			return nil
		}
		if status == ScenarioPaused {
			return nil
		}
		if _, err := r.Next(ctx); err != nil {
			return err
		}
	}
}
func (r *ScenarioRunner) PreviousView() ScenarioStepResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.results) == 0 {
		return ScenarioStepResult{}
	}
	return cloneStepResult(r.results[len(r.results)-1])
}
func (r *ScenarioRunner) ModifyEvent(stepID string, event ScenarioEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := event.validate(); err != nil {
		return err
	}
	for i := range r.scenario.Steps {
		if r.scenario.Steps[i].ID == stepID && r.scenario.Steps[i].Kind == StepInjectEvent {
			r.scenario.Steps[i].Event = &event
			r.modified = true
			return nil
		}
	}
	return errors.New("scenario_event_step_not_found")
}
func (r *ScenarioRunner) Report() ScenarioReport {
	r.mu.Lock()
	report := ScenarioReport{ScenarioID: r.scenario.ID, Seed: r.scenario.Seed, StartedAt: r.startedAt, StepCount: len(r.scenario.Steps), CompletedSteps: len(r.results), NoSecurityAuthority: true, Results: cloneStepResults(r.results)}
	for _, result := range r.results {
		for _, comparison := range result.Comparison {
			switch comparison.Status {
			case ComparisonObserved:
				report.ObservedProperties++
			case ComparisonNotObserved:
				report.UnexpectedProperties++
			case ComparisonInconclusive, ComparisonNotApplicable:
				report.InconclusiveProperties++
			}
		}
	}
	r.mu.Unlock()
	report.FinalGlobalState = r.session.State().Global
	if digest, err := r.session.DurableDigest(); err == nil {
		report.DurableDigest = digest
	}
	report.CompletedAt = time.Now().UTC()
	return report
}
func cloneStepResults(values []ScenarioStepResult) []ScenarioStepResult {
	out := make([]ScenarioStepResult, len(values))
	for i, v := range values {
		out[i] = v
		out[i].Expected = append([]ExpectedProperty(nil), v.Expected...)
		out[i].Observed = append([]ObservedProperty(nil), v.Observed...)
		out[i].Comparison = append([]PropertyComparison(nil), v.Comparison...)
		out[i].Injections = append([]LiveInjectionResult(nil), v.Injections...)
	}
	return out
}
func cloneStepResult(value ScenarioStepResult) ScenarioStepResult {
	return cloneStepResults([]ScenarioStepResult{value})[0]
}

// CompareScenarioRuns uses two isolated real sessions. Neither run shares a
// registry, WAL, clock, or result with the other.
func CompareScenarioRuns(ctx context.Context, library *ScenarioLibrary, leftID, rightID string, seed uint64) (ScenarioComparison, error) {
	left, ok := library.Get(leftID)
	if !ok {
		return ScenarioComparison{}, errors.New("left_scenario_not_found")
	}
	right, ok := library.Get(rightID)
	if !ok {
		return ScenarioComparison{}, errors.New("right_scenario_not_found")
	}
	run := func(scenario Scenario) (ScenarioReport, error) {
		session, err := NewLiveSession(ctx, LiveOptions{Seed: seed})
		if err != nil {
			return ScenarioReport{}, err
		}
		defer session.Close()
		session.SetScenarioLibrary(library)
		if _, err := session.StartScenario(ctx, scenario.ID); err != nil {
			return ScenarioReport{}, err
		}
		if err := session.ScenarioRunToEnd(ctx); err != nil {
			return ScenarioReport{}, err
		}
		return session.ScenarioReport()
	}
	leftReport, err := run(left)
	if err != nil {
		return ScenarioComparison{}, err
	}
	rightReport, err := run(right)
	if err != nil {
		return ScenarioComparison{}, err
	}
	comparison := ScenarioComparison{LeftID: leftID, RightID: rightID, Left: leftReport, Right: rightReport}
	if leftReport.FinalGlobalState.ChainCount != rightReport.FinalGlobalState.ChainCount {
		comparison.Differences = append(comparison.Differences, "chain.count")
	}
	if leftReport.FinalGlobalState.RoutineCount != rightReport.FinalGlobalState.RoutineCount {
		comparison.Differences = append(comparison.Differences, "routine.count")
	}
	if leftReport.FinalGlobalState.ObservationCount != rightReport.FinalGlobalState.ObservationCount {
		comparison.Differences = append(comparison.Differences, "chain.observation_count")
	}
	if leftReport.FinalGlobalState.JournalSequence != rightReport.FinalGlobalState.JournalSequence {
		comparison.Differences = append(comparison.Differences, "wal.sequence")
	}
	return comparison, nil
}
func (r *ScenarioRunner) ComparisonsFor(result ScenarioStepResult) []PropertyComparison {
	r.mu.Lock()
	defer r.mu.Unlock()
	return comparisons(result.Expected, result.Observed, result.Modified)
}
func comparisons(expected []ExpectedProperty, observed []ObservedProperty, modified bool) []PropertyComparison {
	out := make([]PropertyComparison, 0, len(expected))
	for _, property := range expected {
		comparison := PropertyComparison{Expected: property, Status: ComparisonNotApplicable, Detail: LocalizedText{FR: "Propriété non disponible dans cette étape.", EN: "Property is not available in this step."}}
		if modified {
			comparison.Status = ComparisonInconclusive
			comparison.Detail = LocalizedText{FR: "Scénario modifié par l’utilisateur.", EN: "Scenario modified by the user."}
			out = append(out, comparison)
			continue
		}
		for _, value := range observed {
			if value.Scope != property.Scope {
				continue
			}
			copy := value
			comparison.Observed = &copy
			if propertyMatches(property, value.Value) {
				comparison.Status = ComparisonObserved
				comparison.Detail = LocalizedText{FR: "Le résultat réel satisfait la propriété attendue.", EN: "The real result satisfies the expected property."}
			} else {
				comparison.Status = ComparisonNotObserved
				comparison.Detail = LocalizedText{FR: "Le moteur a produit un résultat différent de l’hypothèse du scénario.", EN: "The engine produced a result different from the scenario hypothesis."}
			}
			break
		}
		out = append(out, comparison)
	}
	return out
}
