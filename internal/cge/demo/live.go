package demo

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cge "synora/internal/cge"
	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/chains/journal"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
	"synora/internal/cge/routines"
)

type LiveSession struct {
	mu          sync.Mutex
	ctx         context.Context
	seed        uint64
	root        string
	journalPath string
	clock       *demoClock
	provider    *demoProvider
	topology    cgecontext.TopologySnapshot
	engine      *cge.ShadowEngine
	sequence    uint64
	eventNumber uint64
	events      []LiveInjectionResult
	trace       []LiveTraceStep
	subs        map[chan []byte]struct{}
	closed      bool
	scenarioMu  sync.RWMutex
	scenarios   *ScenarioLibrary
	scenarioRun *ScenarioRunner
}

func NewLiveSession(ctx context.Context, options LiveOptions) (*LiveSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.Seed == 0 {
		options.Seed = 3501
	}
	root, err := os.MkdirTemp("", "synora-cge-live-")
	if err != nil {
		return nil, err
	}
	start := time.Date(2026, 1, 5, 17, 0, 0, 0, time.UTC)
	s := &LiveSession{ctx: ctx, seed: options.Seed, root: root, journalPath: filepath.Join(root, "journal.ndjson"), clock: &demoClock{now: start}, topology: topology(start), subs: map[chan []byte]struct{}{}}
	s.provider = &demoProvider{topology: s.topology, timezone: "Europe/Paris", available: true}
	if err := s.openLocked(true); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return s, nil
}

func (s *LiveSession) openLocked(initialize bool) error {
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = s.root
	config.JournalPath = s.journalPath
	config.InitializeIfMissing = initialize
	config.JournalID = fmt.Sprintf("cge-live-%d", s.seed)
	config.Context.Enabled = true
	config.Context.Timezone = "Europe/Paris"
	config.Context.AllowPartial = true
	config.Cognitive.Enabled = true
	config.Cognitive.AutoApplyDecisiveEvidence = false
	config.Routines.Enabled = true
	config.Routines.AllowPartialContext = true
	config.Deviation.Enabled = true
	config.Deviation.RecentAssessmentLimit = 256
	config.Deviation.MaxAssessmentsPerObservation = 2
	engine, err := cge.NewShadowEngineWithConfig(s.ctx, config, s.clock, quietLogger{})
	if err != nil {
		return err
	}
	engine.SetContextProvider(s.provider)
	engine.SetRoutineTopologyProvider(s.provider)
	s.engine = engine
	return nil
}

func (s *LiveSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.engine != nil {
		_ = s.engine.Close()
	}
	return os.RemoveAll(s.root)
}

func (s *LiveSession) State() LiveState {
	s.mu.Lock()
	state := s.stateLocked()
	s.mu.Unlock()
	state.Scenario = s.ScenarioState()
	return state
}
func (s *LiveSession) stateForScenario() LiveState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}
func (s *LiveSession) topologyForScenario() cgecontext.TopologySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.topology.Clone()
}

func (s *LiveSession) SetScenarioLibrary(library *ScenarioLibrary) {
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	s.scenarios = library
}
func (s *LiveSession) ListScenarios() []ScenarioInfo {
	s.scenarioMu.RLock()
	defer s.scenarioMu.RUnlock()
	if s.scenarios == nil {
		return nil
	}
	values := s.scenarios.List()
	out := make([]ScenarioInfo, 0, len(values))
	for _, value := range values {
		out = append(out, value.Info())
	}
	return out
}
func (s *LiveSession) GetScenario(id string) (Scenario, bool) {
	s.scenarioMu.RLock()
	defer s.scenarioMu.RUnlock()
	if s.scenarios == nil {
		return Scenario{}, false
	}
	return s.scenarios.Get(id)
}
func (s *LiveSession) LoadScenario(id string) (ScenarioRunState, error) {
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	if s.scenarios == nil {
		return ScenarioRunState{}, errors.New("scenario_library_not_loaded")
	}
	scenario, ok := s.scenarios.Get(id)
	if !ok {
		return ScenarioRunState{}, errors.New("scenario_not_found")
	}
	runner, err := NewScenarioRunner(s, scenario)
	if err != nil {
		return ScenarioRunState{}, err
	}
	s.scenarioRun = runner
	return runner.State(), nil
}
func (s *LiveSession) scenarioRunner() (*ScenarioRunner, error) {
	s.scenarioMu.RLock()
	defer s.scenarioMu.RUnlock()
	if s.scenarioRun == nil {
		return nil, errors.New("scenario_not_loaded")
	}
	return s.scenarioRun, nil
}
func (s *LiveSession) StartScenario(ctx context.Context, id string) (ScenarioRunState, error) {
	s.scenarioMu.Lock()
	if id == "" && s.scenarioRun != nil {
		id = s.scenarioRun.scenario.ID
	}
	if s.scenarioRun == nil || s.scenarioRun.scenario.ID != id {
		if s.scenarios == nil {
			s.scenarioMu.Unlock()
			return ScenarioRunState{}, errors.New("scenario_library_not_loaded")
		}
		scenario, ok := s.scenarios.Get(id)
		if !ok {
			s.scenarioMu.Unlock()
			return ScenarioRunState{}, errors.New("scenario_not_found")
		}
		runner, err := NewScenarioRunner(s, scenario)
		if err != nil {
			s.scenarioMu.Unlock()
			return ScenarioRunState{}, err
		}
		s.scenarioRun = runner
	}
	runner := s.scenarioRun
	s.scenarioMu.Unlock()
	if err := runner.Start(ctx); err != nil {
		return runner.State(), err
	}
	return runner.State(), nil
}
func (s *LiveSession) ScenarioState() *ScenarioRunState {
	s.scenarioMu.RLock()
	defer s.scenarioMu.RUnlock()
	if s.scenarioRun == nil {
		return nil
	}
	value := s.scenarioRun.State()
	return &value
}
func (s *LiveSession) ScenarioStateAfter(err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return s.ScenarioState(), nil
}
func (s *LiveSession) ImportScenario(data []byte) error {
	loaded, err := LoadScenarioLibrary(map[string][]byte{"import.json": data})
	if err != nil {
		return err
	}
	s.scenarioMu.Lock()
	defer s.scenarioMu.Unlock()
	if s.scenarios == nil {
		s.scenarios = &ScenarioLibrary{scenarios: map[string]Scenario{}}
	}
	for _, scenario := range loaded.List() {
		s.scenarios.scenarios[scenario.ID] = scenario
	}
	return nil
}
func (s *LiveSession) ExportScenario() ([]byte, error) {
	s.scenarioMu.RLock()
	defer s.scenarioMu.RUnlock()
	if s.scenarioRun == nil {
		return nil, errors.New("scenario_not_loaded")
	}
	state := s.scenarioRun.State()
	return json.MarshalIndent(state.Scenario, "", "  ")
}
func (s *LiveSession) ScenarioNext(ctx context.Context) (ScenarioStepResult, error) {
	runner, err := s.scenarioRunner()
	if err != nil {
		return ScenarioStepResult{}, err
	}
	result, err := runner.Next(ctx)
	if err == nil {
		result.Comparison = comparisons(result.Expected, result.Observed, result.Modified)
	}
	return result, err
}
func (s *LiveSession) ScenarioPreviousView() (ScenarioStepResult, error) {
	runner, err := s.scenarioRunner()
	if err != nil {
		return ScenarioStepResult{}, err
	}
	return runner.PreviousView(), nil
}
func (s *LiveSession) ScenarioPause() error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	runner.Pause()
	return nil
}
func (s *LiveSession) ScenarioResume() error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	runner.Resume()
	return nil
}
func (s *LiveSession) ScenarioCancel() error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	runner.Cancel()
	return nil
}
func (s *LiveSession) ScenarioReset(ctx context.Context) error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	return runner.Reset(ctx)
}
func (s *LiveSession) ScenarioRunToEnd(ctx context.Context) error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	return runner.RunToEnd(ctx)
}
func (s *LiveSession) ModifyScenarioEvent(stepID string, event ScenarioEvent) error {
	runner, err := s.scenarioRunner()
	if err != nil {
		return err
	}
	return runner.ModifyEvent(stepID, event)
}
func (s *LiveSession) ScenarioReport() (ScenarioReport, error) {
	runner, err := s.scenarioRunner()
	if err != nil {
		return ScenarioReport{}, err
	}
	return runner.Report(), nil
}
func (s *LiveSession) DurableDigest() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return "", errors.New("live_engine_missing")
	}
	return s.engine.DurableStateDigest(s.ctx)
}
func (s *LiveSession) SetTopologyAvailable(available bool) {
	s.provider.mu.Lock()
	s.provider.available = available
	s.provider.mu.Unlock()
}
func (s *LiveSession) Checkpoint(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.engine == nil {
		return nil, errors.New("live_engine_missing")
	}
	value, err := s.engine.CreateCheckpoint(ctx, s.clock.Now())
	if err != nil {
		return nil, err
	}
	return map[string]any{"generation_id": value.Generation.GenerationID, "journal_sequence": value.Generation.IncludedJournalSequence, "checkpoint_sequence": value.Generation.CheckpointRecordSequence, "snapshot_written": value.SnapshotWritten, "checkpoint_appended": value.CheckpointAppended, "manifest_published": value.ManifestPublished}, nil
}

func (s *LiveSession) Subscribe() (<-chan []byte, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan []byte, 32)
	s.subs[ch] = struct{}{}
	initial, _ := json.Marshal(struct {
		Type  string    `json:"type"`
		State LiveState `json:"state"`
	}{"live.state", s.stateLocked()})
	ch <- initial
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
	}
}

func (s *LiveSession) broadcastLocked(kind string, payload any) {
	data, err := json.Marshal(struct {
		Type    string `json:"type"`
		Payload any    `json:"payload"`
	}{kind, payload})
	if err != nil {
		return
	}
	for ch := range s.subs {
		select {
		case ch <- data:
		default:
		}
	}
}

func (s *LiveSession) Advance(req LiveAdvanceRequest) (LiveState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return LiveState{}, errors.New("live_session_closed")
	}
	if !req.At.IsZero() {
		s.clock.Set(req.At)
	} else if req.Minutes != 0 {
		s.clock.Set(s.clock.Now().Add(time.Duration(req.Minutes) * time.Minute))
	}
	state := s.stateLocked()
	s.broadcastLocked("live.clock", state)
	return state, nil
}

func (s *LiveSession) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("live_session_closed")
	}
	if s.engine != nil {
		_ = s.engine.Close()
	}
	oldRoot := s.root
	root, err := os.MkdirTemp("", "synora-cge-live-")
	if err != nil {
		return err
	}
	s.root, s.journalPath = root, filepath.Join(root, "journal.ndjson")
	start := time.Date(2026, 1, 5, 17, 0, 0, 0, time.UTC)
	s.clock.Set(start)
	s.topology = topology(start)
	s.provider.mu.Lock()
	s.provider.topology = s.topology
	s.provider.current = syntheticEvent{}
	s.provider.available = true
	s.provider.mu.Unlock()
	s.events = nil
	s.trace = nil
	s.sequence = 0
	s.eventNumber = 0
	if err := s.openLocked(true); err != nil {
		_ = os.RemoveAll(root)
		return err
	}
	_ = os.RemoveAll(oldRoot)
	s.broadcastLocked("live.reset", s.stateLocked())
	return nil
}

func (s *LiveSession) Restart() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("live_session_closed")
	}
	before, err := s.engine.DurableStateDigest(s.ctx)
	if err != nil {
		return nil, err
	}
	if err := s.engine.Close(); err != nil {
		return nil, err
	}
	if err := s.openLocked(false); err != nil {
		return nil, err
	}
	after, err := s.engine.DurableStateDigest(s.ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"before": before, "after": after, "equal": before == after, "chains": s.engine.Status().ChainCount, "hypotheses": s.engine.Status().HypothesisCount, "routines": s.engine.Status().RoutineCount, "deviation_store": s.engine.SnapshotCount()}
	s.broadcastLocked("live.replay", result)
	return result, nil
}

func (s *LiveSession) LoadBaseline(days int) error {
	if days != 7 && days != 30 {
		return fmt.Errorf("baseline days must be 7 or 30")
	}
	if err := s.Reset(); err != nil {
		return err
	}
	location, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		return fmt.Errorf("load demo timezone: %w", err)
	}
	for day := 0; day < days; day++ {
		at := time.Date(2026, 1, 5+day, 18, 15, 0, 0, location).UTC()
		input := LiveEventInput{EventType: "vision.identity", Identity: "subject-a", IdentityLabel: "Résident A", NodeID: "entrance", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", SimulatedAt: at, SequenceKey: "resident-a-evening", DeviceID: "synthetic-camera", TrackID: "track-a"}
		if _, err := s.Submit(input); err != nil {
			return err
		}
	}
	return nil
}

// RunMemoryFieldIsolation executes every declared variant in a fresh session
// with the same deterministic baseline. It only reads the actual assessment
// returned by the engine; it does not alter the current Live Lab session.
func (s *LiveSession) RunMemoryFieldIsolation(ctx context.Context, definition MemoryFieldIsolation) (MemoryFieldMatrix, error) {
	if definition.BaselineDays != 30 || len(definition.Variants) == 0 {
		return MemoryFieldMatrix{}, errors.New("invalid_memory_field_isolation_definition")
	}
	matrix := MemoryFieldMatrix{BaselineDays: definition.BaselineDays, Rows: make([]MemoryFieldMatrixRow, 0, len(definition.Variants))}
	for _, variant := range definition.Variants {
		if err := ctx.Err(); err != nil {
			return matrix, err
		}
		// Reuse the same seed as well as the same baseline recipe. The sessions
		// are isolated by their temporary roots, not by changing the baseline.
		isolated, err := NewLiveSession(ctx, LiveOptions{Seed: s.seed})
		if err != nil {
			return matrix, err
		}
		if err := isolated.LoadBaseline(definition.BaselineDays); err != nil {
			_ = isolated.Close()
			return matrix, err
		}
		event := variant.Event
		at := event.AbsoluteTime
		if at == nil {
			_ = isolated.Close()
			return matrix, errors.New("memory_variant_absolute_timestamp_required")
		}
		input := LiveEventInput{EventID: event.EventID, EventType: event.EventType, Identity: event.Identity.EntityID, NodeID: event.NodeID, HouseMode: string(event.HouseMode), Occupancy: string(event.Occupancy), ContextQuality: string(event.ContextQuality), SimulatedAt: at.UTC(), PrepareAmbiguity: event.PrepareAmbiguity, SequenceKey: "memory-field-isolation", DeviceID: "scenario-camera", TrackID: "scenario-track"}
		result, submitErr := isolated.Submit(input)
		closeErr := isolated.Close()
		if submitErr != nil {
			return matrix, submitErr
		}
		if closeErr != nil {
			return matrix, closeErr
		}
		row := MemoryFieldMatrixRow{Variant: variant.ID, Label: variant.Label, Changes: append([]string(nil), variant.Changes...), Coverage: result.Deviation.Coverage, Total: result.Deviation.Score, Status: result.Deviation.Status}
		for _, factor := range result.Deviation.Factors {
			value := FactorValue{Available: factor.Available, Score: factor.Score}
			switch factor.Kind {
			case "structural":
				row.Structural = value
			case "temporal":
				row.Temporal = value
			case "interval":
				row.Interval = value
			}
		}
		matrix.Rows = append(matrix.Rows, row)
	}
	return matrix, nil
}

func (s *LiveSession) Submit(input LiveEventInput) (LiveInjectionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return LiveInjectionResult{}, errors.New("live_session_closed")
	}
	result, err := s.submitLocked(input)
	if err == nil {
		s.events = append(s.events, result)
		if len(s.events) > 500 {
			s.events = s.events[len(s.events)-500:]
		}
		s.broadcastLocked("live.event", result)
		s.broadcastLocked("live.state", s.stateLocked())
	}
	return result, err
}

func (s *LiveSession) RunBatch(ctx context.Context, request LiveBatchRequest, progress func(LiveInjectionResult)) ([]LiveInjectionResult, error) {
	if request.Count <= 0 || request.Count > 30 {
		return nil, errors.New("batch_count_out_of_range")
	}
	step := 24 * time.Hour
	if request.Step != "" {
		parsed, err := time.ParseDuration(request.Step)
		if err != nil || parsed <= 0 {
			return nil, errors.New("invalid_batch_step")
		}
		step = parsed
	}
	results := make([]LiveInjectionResult, 0, request.Count)
	for i := 0; i < request.Count; i++ {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		input := request.Input
		input.EventID = ""
		at := input.SimulatedAt
		if at.IsZero() {
			at = s.State().SimulatedAt
		}
		input.SimulatedAt = at.Add(time.Duration(i) * step)
		result, err := s.Submit(input)
		if err != nil {
			return results, err
		}
		results = append(results, result)
		if progress != nil {
			progress(result)
		}
		if request.Delay > 0 {
			timer := time.NewTimer(time.Duration(request.Delay) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return results, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return results, nil
}

func (s *LiveSession) submitLocked(input LiveEventInput) (LiveInjectionResult, error) {
	started := time.Now()
	if input.EventType == "" {
		input.EventType = "vision.identity"
	}
	if input.EventType != "vision.identity" && input.EventType != "vision.unknown" && input.EventType != "vision.uncertain" {
		return LiveInjectionResult{}, errors.New("unsupported_live_event_type")
	}
	if input.NodeID == "" {
		return LiveInjectionResult{}, errors.New("node_id_required")
	}
	at := input.SimulatedAt
	if at.IsZero() {
		at = s.clock.Now()
	}
	at = at.UTC()
	if input.EventID == "" {
		s.eventNumber++
		input.EventID = fmt.Sprintf("live-%d-%04d", s.seed, s.eventNumber)
	}
	if input.DeviceID == "" {
		input.DeviceID = "synthetic-camera"
	}
	if input.TrackID == "" {
		input.TrackID = "track-live"
	}
	if input.SequenceKey == "" {
		input.SequenceKey = "live-sequence"
	}
	mode, err := parseHouseMode(input.HouseMode)
	if err != nil {
		return LiveInjectionResult{}, err
	}
	occupancy, err := parseOccupancy(input.Occupancy)
	if err != nil {
		return LiveInjectionResult{}, err
	}
	quality := strings.ToLower(strings.TrimSpace(input.ContextQuality))
	if quality == "" {
		quality = "complete"
	}
	if quality != "complete" && quality != "partial" && quality != "missing" {
		return LiveInjectionResult{}, errors.New("invalid_context_quality")
	}
	item := syntheticEvent{ID: input.EventID, At: at, NodeID: input.NodeID, Identity: input.Identity, DeviceID: input.DeviceID, SequenceKey: input.SequenceKey, TrackID: input.TrackID, Mode: mode, Occupancy: occupancy, Partial: quality == "partial", Missing: quality == "missing"}
	s.provider.Set(item)
	s.clock.Set(at)
	event := cge.Event{ID: input.EventID, Type: input.EventType, Source: "synthetic-live-lab", Timestamp: at, DeviceID: input.DeviceID, NodeID: input.NodeID, Identity: input.Identity, TrackID: input.TrackID, SequenceKey: input.SequenceKey}
	if input.PrepareAmbiguity {
		if err := s.engine.SeedAssociationAmbiguityFixture(s.ctx, at); err != nil {
			return LiveInjectionResult{}, err
		}
	}
	plan, planErr := s.engine.PlanAssociationForEvent(s.ctx, event)
	beforeChains := s.engine.ListChains()
	beforeRoutines := s.engine.ListRoutines()
	beforeRoutineTotal := routineOccurrences(beforeRoutines)
	trace := []LiveTraceStep{{Sequence: 1, Kind: "observation.received", At: at, Payload: map[string]any{"event_id": input.EventID, "event_type": input.EventType}}}
	contextResult := s.contextResult(item)
	trace = append(trace, LiveTraceStep{Sequence: 2, Kind: "context.resolved", At: at, Payload: contextResult})
	associationResult := LiveAssociationResult{}
	if planErr == nil {
		associationResult = associationPlanResult(plan)
		trace = append(trace, LiveTraceStep{Sequence: 3, Kind: "association.planned", At: at, Payload: associationResult})
	}
	if _, err := s.engine.Observe(s.ctx, event); err != nil {
		return LiveInjectionResult{}, fmt.Errorf("live_observe: %w", err)
	}
	orches := s.engine.LastOrchestrationResult()
	if planErr != nil {
		associationResult.Decision = string(orches.AssociationDecision)
	}
	if associationResult.Decision == "" {
		associationResult.Decision = string(orches.AssociationDecision)
	}
	if associationResult.ReasonCode == "" {
		associationResult.ReasonCode = "runtime_result"
	}
	trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "association.applied", At: at, Payload: map[string]any{"decision": orches.AssociationDecision, "chain_id": orches.ChainID, "applied": orches.AssociationApplied}})
	chainsAfter := s.engine.ListChains()
	chainBefore, chainAfter := chainForPlan(beforeChains, plan, ""), chainByID(chainsAfter, string(orches.ChainID))
	evidenceResult := LiveEvidenceResult{Decision: string(orches.EvidenceDecision)}
	if orchestrationChain := string(orches.ChainID); orchestrationChain != "" {
		if evaluation, evalErr := s.engine.EvaluateEvidenceForObservation(s.ctx, chains.ChainID(orchestrationChain), input.EventID, at); evalErr == nil {
			evidenceResult = evidenceResultFrom(evaluation)
		}
	}
	trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "evidence.evaluated", At: at, Payload: evidenceResult})
	hypothesesAfter := s.engine.ListHypotheses()
	hypothesisResult := LiveHypothesisResult{Action: string(orches.HypothesisAction), Count: len(hypothesesAfter)}
	if hypothesisResult.Action != "" && hypothesisResult.Action != "none" {
		trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "hypothesis.updated", At: at, Payload: hypothesisResult})
	}
	trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "deviation.baseline_read", At: at, Payload: map[string]any{"routine_count": len(beforeRoutines), "occurrence_count": beforeRoutineTotal}})
	deviationResult := LiveDeviationResult{}
	if last := s.engine.LastDeviationResult(); last.Attempted {
		deviationResult.Attempted = true
		deviationResult.Status = ""
		if assessment, ok := s.engine.LastDeviationAssessmentDetailed(); ok {
			deviationResult = deviationResultFrom(assessment, beforeRoutines, contextResult)
		}
		trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "deviation.evaluated", At: at, Payload: deviationResult})
	}
	afterRoutines := s.engine.ListRoutines()
	learning := learningResult(beforeRoutines, afterRoutines, beforeRoutineTotal, routineOccurrences(afterRoutines))
	trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "routine.learned", At: at, Payload: learning})
	wal := s.readWAL(20)
	if learning.OccurrenceCountAfter > learning.OccurrenceCountBefore {
		trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "routine.occurrence_added", At: at, Payload: map[string]any{"before": learning.OccurrenceCountBefore, "after": learning.OccurrenceCountAfter, "wal_sequences": walSequences(wal)}})
	}
	status := s.engine.Status()
	trace = append(trace, LiveTraceStep{Sequence: uint64(len(trace) + 1), Kind: "wal.confirmed", At: at, Payload: map[string]any{"sequence": status.JournalSequence, "head_hash": status.JournalHeadHash}})
	state := s.stateLocked()
	state.SimulatedAt = at
	s.trace = append(s.trace, trace...)
	if len(s.trace) > 240 {
		s.trace = s.trace[len(s.trace)-240:]
	}
	return LiveInjectionResult{EventID: input.EventID, SimulatedAt: at, Context: contextResult, Association: associationResult, Evidence: evidenceResult, Hypothesis: hypothesisResult, Deviation: deviationResult, Learning: learning, ChainBefore: chainBefore, ChainAfter: chainAfter, WALRecords: wal, GlobalState: state.Global, TotalDurationMicros: uint64(time.Since(started) / time.Microsecond), Trace: trace}, nil
}

func (s *LiveSession) contextResult(item syntheticEvent) LiveContextResult {
	frame, err := s.provider.Resolve(s.ctx, item.ID, item.At, item.NodeID)
	if err != nil {
		return LiveContextResult{ObservationID: item.ID, NodeID: item.NodeID, Quality: "missing"}
	}
	return LiveContextResult{ObservationID: item.ID, ObservedAt: frame.ObservedAt, NodeID: frame.NodeID, ZoneID: frame.ZoneID, NodeKind: string(frame.NodeKind), HouseMode: string(frame.HouseMode), Occupancy: string(frame.Occupancy), Quality: string(frame.Quality), Timezone: frame.Time.Timezone, Weekday: frame.Time.Weekday.String(), MinuteOfDay: frame.Time.MinuteOfDay, TimeBucket: frame.Time.MinuteOfDay / 15, DayPart: string(frame.Time.DayPart), Fingerprint: frame.Fingerprint}
}

func (s *LiveSession) stateLocked() LiveState {
	status := s.engine.Status()
	chainsValue := s.engine.ListChains()
	hypothesesValue := s.engine.ListHypotheses()
	routinesValue := s.engine.ListRoutines()
	wal := s.readWAL(24)
	chainSummaries := make([]LiveChainSummary, 0, len(chainsValue))
	for _, value := range chainsValue {
		chainSummaries = append(chainSummaries, chainSummary(value))
	}
	openCount := 0
	hypotheses := make([]any, len(hypothesesValue))
	for i, value := range hypothesesValue {
		hypotheses[i] = value
		if string(value.Status) == "open" {
			openCount++
		}
	}
	routines := make([]any, len(routinesValue))
	for i, value := range routinesValue {
		routines[i] = value
	}
	snapshot, _ := s.engine.Snapshot(s.ctx)
	global := LiveGlobalState{SimulatedAt: s.clock.Now(), ChainCount: status.ChainCount, OpenHypothesisCount: openCount, HypothesisCount: status.HypothesisCount, RoutineCount: status.RoutineCount, ObservationCount: snapshot.ObservationCount, JournalSequence: status.JournalSequence, JournalHeadHash: status.JournalHeadHash, CoordinatorState: string(status.State), DeviationStoreCount: s.engine.SnapshotCount(), ActionsEnabled: false, Synthetic: true}
	var lastResult *LiveInjectionResult
	if len(s.events) > 0 {
		last := s.events[len(s.events)-1]
		lastResult = &last
	}
	return LiveState{SessionID: fmt.Sprintf("live-%d", s.seed), Seed: s.seed, SimulatedAt: s.clock.Now(), Topology: s.topology, Global: global, Chains: chainSummaries, Hypotheses: hypotheses, Routines: routines, WAL: wal, Trace: append([]LiveTraceStep(nil), s.trace...), Events: append([]LiveInjectionResult(nil), s.events...), LastResult: lastResult, Mode: "live", SyntheticNotice: "Événements synthétiques — traitement réel du CGE", Qualification: "Qualification non chargée", Capabilities: CurrentCapabilities(), InterpretationNotice: "Le CGE mesure une divergence avec sa mémoire. Il n’en interprète pas encore la cause."}
}

func (s *LiveSession) readWAL(limit int) []LiveWALRecord {
	file, err := os.Open(s.journalPath)
	if err != nil {
		return nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > limit {
			lines = lines[1:]
		}
	}
	out := make([]LiveWALRecord, 0, len(lines))
	for _, line := range lines {
		var value journal.Record
		if json.Unmarshal([]byte(line), &value) == nil {
			out = append(out, LiveWALRecord{Sequence: value.Sequence, Kind: string(value.Kind), RecordedAt: value.RecordedAt, Actor: value.Actor, CorrelationID: value.CorrelationID, PreviousHash: value.PreviousHash, RecordHash: value.RecordHash})
		}
	}
	return out
}

func (s *LiveSession) Trace() []LiveTraceStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LiveTraceStep(nil), s.trace...)
}

func associationPlanResult(plan association.Plan) LiveAssociationResult {
	out := LiveAssociationResult{Decision: string(plan.Decision), PolicyVersion: plan.PolicyVersion, BestScore: plan.BestScore, ScoreMargin: plan.ScoreMargin, SelectedChainID: string(plan.SelectedChainID), NewChainID: string(plan.NewChainID), ReasonCode: plan.ReasonCode, Reason: plan.Reason}
	out.Candidates = make([]LiveCandidate, 0, len(plan.RankedCandidates))
	for _, c := range plan.RankedCandidates {
		facts := make([]LiveScoreFact, 0, len(c.Facts))
		for _, f := range c.Facts {
			facts = append(facts, LiveScoreFact{Code: f.Code, Score: f.Score, Detail: f.Detail})
		}
		out.Candidates = append(out.Candidates, LiveCandidate{ChainID: string(c.ChainID), SourceRevision: c.SourceRevision, Status: string(c.Status), Eligible: c.Eligible, Score: c.Score, RejectionCode: c.RejectionCode, Facts: facts})
	}
	return out
}
func evidenceResultFrom(value evidence.EvidenceEvaluation) LiveEvidenceResult {
	out := LiveEvidenceResult{Decision: string(value.Decision), ChainID: string(value.ChainID), SourceRevision: value.SourceRevision, SupportScore: value.SupportScore, ContradictionScore: value.ContradictionScore, DecisionMargin: value.DecisionMargin, ReasonCode: value.ReasonCode, Reason: value.Reason, Fingerprint: value.EvidenceFingerprint}
	for _, f := range value.Facts {
		out.Facts = append(out.Facts, LiveEvidenceFact{Code: f.Code, Side: string(f.Side), Score: f.Score, Detail: f.Detail, ObservationIDs: f.ObservationIDs})
	}
	return out
}
func deviationResultFrom(value deviation.Assessment, baseline []routines.Snapshot, observed LiveContextResult) LiveDeviationResult {
	out := LiveDeviationResult{Attempted: true, Status: string(value.Status), Band: string(value.Band), Score: uint16(value.Score), Coverage: uint16(value.Coverage), CandidateCount: len(value.Candidates), BaselineCount: len(value.Baseline), ReasonCodes: value.ReasonCodes, Fingerprint: value.Fingerprint, Observed: observed, RoutineReady: value.BestMatch != nil}
	if value.BestMatch != nil {
		out.RoutineID = string(value.BestMatch.Routine.RoutineID)
		for _, f := range []deviation.Factor{value.BestMatch.Structural, value.BestMatch.Temporal, value.BestMatch.Interval} {
			out.Factors = append(out.Factors, LiveFactor{Kind: string(f.Kind), Available: f.Available, Score: uint16(f.Score), Weight: uint16(f.Weight), Contribution: uint16(f.EffectiveWeight), ReasonCodes: f.ReasonCodes})
		}
		for _, candidate := range baseline {
			if candidate.ID != value.BestMatch.Routine.RoutineID {
				continue
			}
			diagnostic := routineDiagnostic(candidate)
			out.Routine = &diagnostic
			break
		}
	}
	return out
}

func routineDiagnostic(value routines.Snapshot) LiveRoutineDiagnostic {
	diagnostic := LiveRoutineDiagnostic{RoutineID: string(value.ID), Revision: value.Revision, OccurrenceCount: value.OccurrenceCount, LastSeenAt: value.LastSeenAt, PatternKind: string(value.Pattern.Kind), TemporalBins: make([]LiveTemporalBin, 0, len(value.TemporalBins)), IntervalStatistics: LiveIntervalStatistics{Count: value.IntervalStatistics.Count, Minimum: value.IntervalStatistics.Minimum, Maximum: value.IntervalStatistics.Maximum, Total: value.IntervalStatistics.Total, Mean: value.IntervalStatistics.Mean}}
	for _, bin := range value.TemporalBins {
		diagnostic.TemporalBins = append(diagnostic.TemporalBins, LiveTemporalBin{Weekday: bin.Weekday.String(), TimeBucket: bin.TimeBucket, Count: bin.Count})
	}
	if value.Pattern.Presence != nil {
		diagnostic.PatternNodeID = value.Pattern.Presence.NodeID
		diagnostic.PatternHouseMode = string(value.Pattern.Presence.HouseMode)
		diagnostic.PatternOccupancy = string(value.Pattern.Presence.Occupancy)
	}
	return diagnostic
}

func walSequences(records []LiveWALRecord) []uint64 {
	sequences := make([]uint64, 0, len(records))
	for _, record := range records {
		if record.Kind == "routine.created" || record.Kind == "routine.occurrence_added" {
			sequences = append(sequences, record.Sequence)
		}
	}
	return sequences
}
func chainSummary(value chains.Snapshot) LiveChainSummary {
	return LiveChainSummary{ID: string(value.ID), EntityID: value.EntityID, Status: string(value.Status), Revision: value.Revision, ObservationCount: len(value.Observations), Confidence: value.CurrentConfidence, FirstSeenAt: value.FirstSeenAt, LastSeenAt: value.LastSeenAt}
}
func chainByID(values []chains.Snapshot, id string) *LiveChainSummary {
	for _, value := range values {
		if string(value.ID) == id {
			out := chainSummary(value)
			return &out
		}
	}
	return nil
}
func chainForPlan(values []chains.Snapshot, plan association.Plan, fallback string) *LiveChainSummary {
	id := string(plan.SelectedChainID)
	if id == "" {
		id = fallback
	}
	return chainByID(values, id)
}
func routineOccurrences(values []routines.Snapshot) uint64 {
	var total uint64
	for _, value := range values {
		total += value.OccurrenceCount
	}
	return total
}
func learningResult(before, after []routines.Snapshot, beforeTotal, afterTotal uint64) LiveLearningResult {
	beforeIDs := map[string]bool{}
	for _, value := range before {
		beforeIDs[string(value.ID)] = true
	}
	out := LiveLearningResult{RoutineCountBefore: len(before), RoutineCountAfter: len(after), OccurrenceCountBefore: beforeTotal, OccurrenceCountAfter: afterTotal}
	for _, value := range after {
		if !beforeIDs[string(value.ID)] {
			if value.Kind == routines.KindPresence {
				out.PresenceCreated++
			} else {
				out.TransitionCreated++
			}
		}
	}
	if afterTotal > beforeTotal {
		if len(after) > 0 {
			out.PresenceAdded = int(afterTotal - beforeTotal)
		}
	}
	return out
}
func parseHouseMode(value string) (cgecontext.HouseMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "home":
		return cgecontext.HouseModeHome, nil
	case "away":
		return cgecontext.HouseModeAway, nil
	case "night":
		return cgecontext.HouseModeNight, nil
	case "sleep":
		return cgecontext.HouseModeSleep, nil
	case "armed":
		return cgecontext.HouseModeArmed, nil
	case "", "unknown":
		return cgecontext.HouseModeUnknown, nil
	}
	return "", errors.New("invalid_house_mode")
}
func parseOccupancy(value string) (cgecontext.OccupancyState, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "occupied":
		return cgecontext.OccupancyOccupied, nil
	case "unoccupied":
		return cgecontext.OccupancyUnoccupied, nil
	case "", "unknown":
		return cgecontext.OccupancyUnknown, nil
	}
	return "", errors.New("invalid_occupancy")
}
