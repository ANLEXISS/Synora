package demo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	cgecontext "synora/internal/cge/context"
)

const ScenarioSchemaVersion = "cge-demo-scenario-v1"

const (
	ScenarioCategoryLearning                  ScenarioCategory     = "learning"
	ScenarioCategoryAssociation               ScenarioCategory     = "association"
	ScenarioCategoryAmbiguity                 ScenarioCategory     = "ambiguity"
	ScenarioCategoryDeviation                 ScenarioCategory     = "deviation"
	ScenarioCategoryAdaptation                ScenarioCategory     = "adaptation"
	ScenarioCategoryResilience                ScenarioCategory     = "resilience"
	ScenarioCategoryContext                   ScenarioCategory     = "context"
	ScenarioCategoryIdentity                  ScenarioCategory     = "identity"
	ScenarioCategoryDurability                ScenarioCategory     = "durability"
	ScenarioCategoryMemoryFieldIsolation      ScenarioCategory     = "memory-field-isolation"
	ScenarioDifficultyIntroductory            ScenarioDifficulty   = "introductory"
	ScenarioDifficultyIntermediate            ScenarioDifficulty   = "intermediate"
	ScenarioDifficultyTechnical               ScenarioDifficulty   = "technical"
	InitialStateEmpty                         InitialStateMode     = "empty"
	InitialStateGeneratedBaseline             InitialStateMode     = "generated_baseline"
	InitialStateScenarioSpecific              InitialStateMode     = "scenario_specific"
	StepInjectEvent                           ScenarioStepKind     = "inject_event"
	StepAdvanceTime                           ScenarioStepKind     = "advance_time"
	StepRepeatEvent                           ScenarioStepKind     = "repeat_event"
	StepRestartEngine                         ScenarioStepKind     = "restart_engine"
	StepPause                                 ScenarioStepKind     = "pause_for_explanation"
	StepCheckpoint                            ScenarioStepKind     = "capture_checkpoint"
	IdentityKnown                             ScenarioIdentityKind = "known"
	IdentityUnknown                           ScenarioIdentityKind = "unknown"
	IdentityUncertain                         ScenarioIdentityKind = "uncertain"
	IdentityNone                              ScenarioIdentityKind = "none"
	TimestampAbsolute                         TimestampMode        = "absolute"
	TimestampRelative                         TimestampMode        = "relative"
	RepeatNewID                               RepeatEventIDMode    = "new_id_each_time"
	RepeatSameID                              RepeatEventIDMode    = "same_id"
	RepeatSequence                            RepeatEventIDMode    = "deterministic_sequence"
	ExpectedInformational                     ExpectedSeverity     = "informational"
	Expected                                  ExpectedSeverity     = "expected"
	ExpectedImportant                         ExpectedSeverity     = "important"
	ExpectedScopeAssociationDecision          ExpectedScope        = "association.decision"
	ExpectedScopeAssociationAmbiguous         ExpectedScope        = "association.ambiguous"
	ExpectedScopeAssociationCandidateCount    ExpectedScope        = "association.candidate_count"
	ExpectedScopeHypothesisAssociationOpened  ExpectedScope        = "hypothesis.association_opened"
	ExpectedScopeHypothesisCount              ExpectedScope        = "hypothesis.count"
	ExpectedScopeRoutineCount                 ExpectedScope        = "routine.count"
	ExpectedScopeRoutineOccurrenceCount       ExpectedScope        = "routine.occurrence_count"
	ExpectedScopeRoutineCreated               ExpectedScope        = "routine.created"
	ExpectedScopeRoutineOccurrenceAdded       ExpectedScope        = "routine.occurrence_added"
	ExpectedScopeRoutineReadiness             ExpectedScope        = "routine.readiness"
	ExpectedScopeDeviationStatus              ExpectedScope        = "deviation.status"
	ExpectedScopeDeviationBand                ExpectedScope        = "deviation.band"
	ExpectedScopeDeviationScore               ExpectedScope        = "deviation.score"
	ExpectedScopeDeviationTemporalScore       ExpectedScope        = "deviation.temporal_score"
	ExpectedScopeDeviationScorePositive       ExpectedScope        = "deviation.score_positive"
	ExpectedScopeDeviationStructuralAvailable ExpectedScope        = "deviation.structural_available"
	ExpectedScopeDeviationStructuralPositive  ExpectedScope        = "deviation.structural_positive"
	ExpectedScopeDeviationTemporalAvailable   ExpectedScope        = "deviation.temporal_available"
	ExpectedScopeDeviationTemporalPositive    ExpectedScope        = "deviation.temporal_positive"
	ExpectedScopeDeviationIntervalAvailable   ExpectedScope        = "deviation.interval_available"
	ExpectedScopeDeviationIntervalPositive    ExpectedScope        = "deviation.interval_positive"
	ExpectedScopeDeviationCoverage            ExpectedScope        = "deviation.coverage"
	ExpectedScopeWALDelta                     ExpectedScope        = "wal.sequence_delta"
	ExpectedScopeReplayEqual                  ExpectedScope        = "replay.digest_equal"
	ExpectedScopeChainCount                   ExpectedScope        = "chain.count"
	ExpectedScopeObservationCount             ExpectedScope        = "chain.observation_count"
	ExpectedScopeCoordinatorState             ExpectedScope        = "coordinator.state"
	OperatorEquals                            ExpectedOperator     = "equals"
	OperatorNotEquals                         ExpectedOperator     = "not_equals"
	OperatorGreaterThan                       ExpectedOperator     = "greater_than"
	OperatorGreaterOrEqual                    ExpectedOperator     = "greater_or_equal"
	OperatorLessThan                          ExpectedOperator     = "less_than"
	OperatorOneOf                             ExpectedOperator     = "one_of"
	OperatorExists                            ExpectedOperator     = "exists"
	OperatorNotExists                         ExpectedOperator     = "not_exists"
	StepResultCompleted                       StepResultStatus     = "completed"
	StepResultUnexpected                      StepResultStatus     = "completed_with_unexpected_result"
	StepResultInconclusive                    StepResultStatus     = "inconclusive"
	StepResultFailed                          StepResultStatus     = "failed"
	StepResultCancelled                       StepResultStatus     = "cancelled"
	ComparisonObserved                        ComparisonStatus     = "observed"
	ComparisonNotObserved                     ComparisonStatus     = "not_observed"
	ComparisonNotApplicable                   ComparisonStatus     = "not_applicable"
	ComparisonInconclusive                    ComparisonStatus     = "inconclusive"
)

type ScenarioCategory string
type ScenarioDifficulty string
type InitialStateMode string
type ScenarioStepKind string
type ScenarioIdentityKind string
type TimestampMode string
type RepeatEventIDMode string
type ExpectedSeverity string
type ExpectedScope string
type ExpectedOperator string
type StepResultStatus string
type ComparisonStatus string

type LocalizedText struct {
	FR string `json:"fr"`
	EN string `json:"en"`
}

type Scenario struct {
	SchemaVersion        string                `json:"schema_version"`
	ID                   string                `json:"id"`
	Title                LocalizedText         `json:"title"`
	Description          LocalizedText         `json:"description"`
	Category             ScenarioCategory      `json:"category"`
	Difficulty           ScenarioDifficulty    `json:"difficulty"`
	Seed                 uint64                `json:"seed"`
	InitialState         InitialState          `json:"initial_state"`
	Steps                []ScenarioStep        `json:"steps"`
	LearningObjectives   []LocalizedText       `json:"learning_objectives"`
	ExpectedProperties   []ExpectedProperty    `json:"expected_properties"`
	Tags                 []string              `json:"tags"`
	MemoryFieldIsolation *MemoryFieldIsolation `json:"memory_field_isolation,omitempty"`
	UnsupportedReason    *LocalizedText        `json:"unsupported_reason,omitempty"`
}

type MemoryFieldIsolation struct {
	BaselineDays int                  `json:"baseline_days"`
	Baseline     ScenarioEvent        `json:"baseline"`
	Variants     []MemoryFieldVariant `json:"variants"`
}

type MemoryFieldVariant struct {
	ID      string        `json:"id"`
	Label   LocalizedText `json:"label"`
	Changes []string      `json:"changes"`
	Event   ScenarioEvent `json:"event"`
}

type MemoryFieldMatrix struct {
	BaselineDays int                    `json:"baseline_days"`
	Rows         []MemoryFieldMatrixRow `json:"rows"`
}

type MemoryFieldMatrixRow struct {
	Variant    string        `json:"variant"`
	Label      LocalizedText `json:"label"`
	Changes    []string      `json:"changes"`
	Structural FactorValue   `json:"structural"`
	Temporal   FactorValue   `json:"temporal"`
	Interval   FactorValue   `json:"interval"`
	Coverage   uint16        `json:"coverage"`
	Total      uint16        `json:"total"`
	Status     string        `json:"status"`
}

type FactorValue struct {
	Available bool   `json:"available"`
	Score     uint16 `json:"score"`
}

type InitialState struct {
	Mode          InitialStateMode `json:"mode"`
	BaselineDays  int              `json:"baseline_days"`
	StartAt       time.Time        `json:"start_at"`
	Timezone      string           `json:"timezone"`
	ResidentCount int              `json:"resident_count"`
	TopologyID    string           `json:"topology_id"`
}

type Duration time.Duration

func (d Duration) Duration() time.Duration      { return time.Duration(d) }
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }
func (d *Duration) UnmarshalJSON(data []byte) error {
	var text string
	if json.Unmarshal(data, &text) == nil {
		value, err := time.ParseDuration(text)
		if err != nil {
			return err
		}
		*d = Duration(value)
		return nil
	}
	var number int64
	if err := json.Unmarshal(data, &number); err != nil {
		return fmt.Errorf("scenario duration must be a duration string: %w", err)
	}
	*d = Duration(number)
	return nil
}

type ScenarioStep struct {
	ID          string             `json:"id"`
	Title       LocalizedText      `json:"title"`
	Explanation LocalizedText      `json:"explanation"`
	Kind        ScenarioStepKind   `json:"kind"`
	Event       *ScenarioEvent     `json:"event,omitempty"`
	AdvanceTime *AdvanceTimeStep   `json:"advance_time,omitempty"`
	Repeat      *RepeatStep        `json:"repeat,omitempty"`
	Restart     *RestartStep       `json:"restart,omitempty"`
	Pause       *PauseStep         `json:"pause,omitempty"`
	Checkpoint  *CheckpointStep    `json:"checkpoint,omitempty"`
	Expected    []ExpectedProperty `json:"expected,omitempty"`
}
type AdvanceTimeStep struct {
	Minutes int        `json:"minutes,omitempty"`
	At      *time.Time `json:"at,omitempty"`
}
type RepeatStep struct {
	SourceStepID string            `json:"source_step_id"`
	Count        int               `json:"count"`
	Interval     Duration          `json:"interval"`
	EventIDMode  RepeatEventIDMode `json:"event_id_mode"`
}
type RestartStep struct {
	Reason string `json:"reason,omitempty"`
}
type PauseStep struct {
	Reason LocalizedText `json:"reason,omitempty"`
}
type CheckpointStep struct {
	Label string `json:"label,omitempty"`
}
type ScenarioEvent struct {
	EventID                    string                    `json:"event_id"`
	EventType                  string                    `json:"event_type"`
	Identity                   ScenarioIdentity          `json:"identity"`
	NodeID                     string                    `json:"node_id"`
	HouseMode                  cgecontext.HouseMode      `json:"house_mode"`
	Occupancy                  cgecontext.OccupancyState `json:"occupancy"`
	ContextQuality             cgecontext.ContextQuality `json:"context_quality"`
	IdentityConfidencePermille int                       `json:"identity_confidence_permille"`
	TimestampMode              TimestampMode             `json:"timestamp_mode"`
	AbsoluteTime               *time.Time                `json:"absolute_time,omitempty"`
	RelativeOffset             Duration                  `json:"relative_offset,omitempty"`
	PreserveEventID            bool                      `json:"preserve_event_id,omitempty"`
	PrepareAmbiguity           bool                      `json:"prepare_ambiguity,omitempty"`
	TopologyAvailable          *bool                     `json:"topology_available,omitempty"`
}
type ScenarioIdentity struct {
	Kind               ScenarioIdentityKind `json:"kind"`
	EntityID           string               `json:"entity_id,omitempty"`
	CandidateEntityIDs []string             `json:"candidate_entity_ids,omitempty"`
}
type ExpectedProperty struct {
	Code        string           `json:"code"`
	Severity    ExpectedSeverity `json:"severity"`
	Description LocalizedText    `json:"description"`
	Scope       ExpectedScope    `json:"scope"`
	Operator    ExpectedOperator `json:"operator"`
	Value       any              `json:"value,omitempty"`
}

type ObservedProperty struct {
	Code  string        `json:"code"`
	Scope ExpectedScope `json:"scope"`
	Value any           `json:"value,omitempty"`
}
type PropertyComparison struct {
	Expected ExpectedProperty  `json:"expected"`
	Observed *ObservedProperty `json:"observed,omitempty"`
	Status   ComparisonStatus  `json:"status"`
	Detail   LocalizedText     `json:"detail"`
}
type ScenarioStepResult struct {
	ScenarioID  string                `json:"scenario_id"`
	StepID      string                `json:"step_id"`
	StepIndex   int                   `json:"step_index"`
	StartedAt   time.Time             `json:"started_at"`
	CompletedAt time.Time             `json:"completed_at"`
	Injection   *LiveInjectionResult  `json:"injection,omitempty"`
	Injections  []LiveInjectionResult `json:"injections,omitempty"`
	Expected    []ExpectedProperty    `json:"expected"`
	Observed    []ObservedProperty    `json:"observed"`
	Comparison  []PropertyComparison  `json:"comparison"`
	Status      StepResultStatus      `json:"status"`
	Modified    bool                  `json:"modified,omitempty"`
	ErrorCode   string                `json:"error_code,omitempty"`
}
type ScenarioRunStatus string

const (
	ScenarioReady     ScenarioRunStatus = "ready"
	ScenarioRunning   ScenarioRunStatus = "running"
	ScenarioPaused    ScenarioRunStatus = "paused"
	ScenarioCompleted ScenarioRunStatus = "completed"
	ScenarioCancelled ScenarioRunStatus = "cancelled"
	ScenarioFailed    ScenarioRunStatus = "failed"
)

type ScenarioRunState struct {
	Scenario     Scenario             `json:"scenario"`
	CurrentStep  int                  `json:"current_step"`
	Status       ScenarioRunStatus    `json:"status"`
	Modified     bool                 `json:"modified"`
	Results      []ScenarioStepResult `json:"results"`
	MemoryMatrix *MemoryFieldMatrix   `json:"memory_matrix,omitempty"`
}
type ScenarioReport struct {
	ScenarioID             string               `json:"scenario_id"`
	Seed                   uint64               `json:"seed"`
	StartedAt              time.Time            `json:"started_at"`
	CompletedAt            time.Time            `json:"completed_at"`
	StepCount              int                  `json:"step_count"`
	CompletedSteps         int                  `json:"completed_steps"`
	ObservedProperties     int                  `json:"observed_properties"`
	UnexpectedProperties   int                  `json:"unexpected_properties"`
	InconclusiveProperties int                  `json:"inconclusive_properties"`
	FinalGlobalState       LiveGlobalState      `json:"final_global_state"`
	DurableDigest          string               `json:"durable_digest,omitempty"`
	NoSecurityAuthority    bool                 `json:"no_security_authority"`
	Results                []ScenarioStepResult `json:"results"`
}

type ScenarioComparison struct {
	LeftID      string         `json:"left_id"`
	RightID     string         `json:"right_id"`
	Left        ScenarioReport `json:"left"`
	Right       ScenarioReport `json:"right"`
	Differences []string       `json:"differences"`
}

var scenarioCategories = map[ScenarioCategory]bool{ScenarioCategoryLearning: true, ScenarioCategoryAssociation: true, ScenarioCategoryAmbiguity: true, ScenarioCategoryDeviation: true, ScenarioCategoryAdaptation: true, ScenarioCategoryResilience: true, ScenarioCategoryContext: true, ScenarioCategoryIdentity: true, ScenarioCategoryDurability: true, ScenarioCategoryMemoryFieldIsolation: true}
var scenarioKinds = map[ScenarioStepKind]bool{StepInjectEvent: true, StepAdvanceTime: true, StepRepeatEvent: true, StepRestartEngine: true, StepPause: true, StepCheckpoint: true}
var expectedScopes = map[ExpectedScope]bool{ExpectedScopeAssociationDecision: true, ExpectedScopeAssociationAmbiguous: true, ExpectedScopeAssociationCandidateCount: true, ExpectedScopeHypothesisAssociationOpened: true, ExpectedScopeHypothesisCount: true, ExpectedScopeRoutineCount: true, ExpectedScopeRoutineOccurrenceCount: true, ExpectedScopeRoutineCreated: true, ExpectedScopeRoutineOccurrenceAdded: true, ExpectedScopeRoutineReadiness: true, ExpectedScopeDeviationStatus: true, ExpectedScopeDeviationBand: true, ExpectedScopeDeviationScore: true, ExpectedScopeDeviationScorePositive: true, ExpectedScopeDeviationStructuralAvailable: true, ExpectedScopeDeviationStructuralPositive: true, ExpectedScopeDeviationTemporalAvailable: true, ExpectedScopeDeviationTemporalPositive: true, ExpectedScopeDeviationIntervalAvailable: true, ExpectedScopeDeviationIntervalPositive: true, ExpectedScopeDeviationTemporalScore: true, ExpectedScopeDeviationCoverage: true, ExpectedScopeWALDelta: true, ExpectedScopeReplayEqual: true, ExpectedScopeChainCount: true, ExpectedScopeObservationCount: true, ExpectedScopeCoordinatorState: true}
var expectedOperators = map[ExpectedOperator]bool{OperatorEquals: true, OperatorNotEquals: true, OperatorGreaterThan: true, OperatorGreaterOrEqual: true, OperatorLessThan: true, OperatorOneOf: true, OperatorExists: true, OperatorNotExists: true}

func boundedText(value string, max int) bool {
	return value != "" && len(value) <= max && !strings.ContainsAny(value, "\x00<>\r\n")
}
func safeToken(value string, max int) bool {
	if !boundedText(value, max) || strings.Contains(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "..") || strings.Contains(strings.ToLower(value), "javascript:") {
		return false
	}
	for _, char := range value {
		if !(char == '-' || char == '_' || char == '.' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}
func validLocalized(value LocalizedText) bool {
	return boundedText(value.FR, 2000) && boundedText(value.EN, 2000)
}

func (s Scenario) Validate() error {
	if s.SchemaVersion != ScenarioSchemaVersion || !safeToken(s.ID, 128) || !validLocalized(s.Title) || !validLocalized(s.Description) {
		return errors.New("invalid_scenario_identity_or_schema")
	}
	if !scenarioCategories[s.Category] || (s.Difficulty != ScenarioDifficultyIntroductory && s.Difficulty != ScenarioDifficultyIntermediate && s.Difficulty != ScenarioDifficultyTechnical) {
		return errors.New("invalid_scenario_category_or_difficulty")
	}
	if len(s.Steps) == 0 || len(s.Steps) > 100 || s.InitialState.ResidentCount < 0 || s.InitialState.ResidentCount > 100 || s.InitialState.BaselineDays < 0 || s.InitialState.BaselineDays > 365 || s.InitialState.StartAt.IsZero() || !safeToken(s.InitialState.TopologyID, 128) {
		return errors.New("invalid_scenario_limits_or_initial_state")
	}
	if _, err := time.LoadLocation(s.InitialState.Timezone); err != nil {
		return fmt.Errorf("invalid_scenario_timezone: %w", err)
	}
	if s.InitialState.Mode != InitialStateEmpty && s.InitialState.Mode != InitialStateGeneratedBaseline && s.InitialState.Mode != InitialStateScenarioSpecific {
		return errors.New("invalid_initial_state_mode")
	}
	seen := map[string]bool{}
	expanded := 0
	for _, step := range s.Steps {
		if !safeToken(step.ID, 128) || seen[step.ID] || !validLocalized(step.Title) || !validLocalized(step.Explanation) || !scenarioKinds[step.Kind] {
			return fmt.Errorf("invalid_scenario_step: %s", step.ID)
		}
		seen[step.ID] = true
		payloads := 0
		if step.Event != nil {
			payloads++
		}
		if step.AdvanceTime != nil {
			payloads++
		}
		if step.Repeat != nil {
			payloads++
		}
		if step.Restart != nil {
			payloads++
		}
		if step.Pause != nil {
			payloads++
		}
		if step.Checkpoint != nil {
			payloads++
		}
		if payloads != 1 {
			return fmt.Errorf("scenario_step_payload_count: %s", step.ID)
		}
		switch step.Kind {
		case StepInjectEvent:
			if step.Event == nil {
				return fmt.Errorf("scenario_step_event_missing: %s", step.ID)
			}
			if err := step.Event.validate(); err != nil {
				return fmt.Errorf("scenario_step_event: %s: %w", step.ID, err)
			}
			expanded++
		case StepAdvanceTime:
			if step.AdvanceTime.Minutes == 0 && step.AdvanceTime.At == nil {
				return fmt.Errorf("scenario_step_advance_missing: %s", step.ID)
			}
			if step.AdvanceTime.Minutes < -365*24*60 || step.AdvanceTime.Minutes > 365*24*60 {
				return errors.New("scenario_advance_out_of_range")
			}
		case StepRepeatEvent:
			if step.Repeat.SourceStepID == "" || !seen[step.Repeat.SourceStepID] || step.Repeat.Count < 1 || step.Repeat.Count > 100 || step.Repeat.Interval.Duration() <= 0 {
				return fmt.Errorf("invalid_repeat_step: %s", step.ID)
			}
			if step.Repeat.EventIDMode != RepeatNewID && step.Repeat.EventIDMode != RepeatSameID && step.Repeat.EventIDMode != RepeatSequence {
				return errors.New("invalid_repeat_event_id_mode")
			}
			expanded += step.Repeat.Count
		case StepPause, StepRestartEngine, StepCheckpoint:
		}
		if expanded > 1000 {
			return errors.New("scenario_event_limit_exceeded")
		}
		for _, property := range step.Expected {
			if err := property.Validate(); err != nil {
				return err
			}
		}
	}
	for _, property := range s.ExpectedProperties {
		if err := property.Validate(); err != nil {
			return err
		}
	}
	for _, text := range s.LearningObjectives {
		if !validLocalized(text) {
			return errors.New("invalid_learning_objective")
		}
	}
	if s.MemoryFieldIsolation != nil {
		if s.Category != ScenarioCategoryMemoryFieldIsolation || s.MemoryFieldIsolation.BaselineDays != 30 || len(s.MemoryFieldIsolation.Variants) != 7 {
			return errors.New("invalid_memory_field_isolation_definition")
		}
		if err := s.MemoryFieldIsolation.Baseline.validate(); err != nil {
			return fmt.Errorf("memory_baseline: %w", err)
		}
		seenVariants := map[string]bool{}
		for _, variant := range s.MemoryFieldIsolation.Variants {
			if !safeToken(variant.ID, 64) || seenVariants[variant.ID] || !validLocalized(variant.Label) || len(variant.Changes) == 0 || len(variant.Changes) > 8 {
				return errors.New("invalid_memory_field_variant")
			}
			seenVariants[variant.ID] = true
			for _, change := range variant.Changes {
				if !safeToken(change, 64) {
					return errors.New("invalid_memory_field_change")
				}
			}
			if err := variant.Event.validate(); err != nil {
				return fmt.Errorf("memory_variant_%s: %w", variant.ID, err)
			}
		}
	}
	return nil
}

func (s Scenario) ValidateAgainstTopology(topology cgecontext.TopologySnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := topology.Validate(); err != nil {
		return err
	}
	if s.InitialState.TopologyID != topology.Revision {
		return fmt.Errorf("scenario_topology_revision_mismatch: %s", s.InitialState.TopologyID)
	}
	known := make(map[string]bool, len(topology.Nodes))
	for _, node := range topology.Nodes {
		known[node.ID] = true
	}
	for _, step := range s.Steps {
		if step.Event != nil && !known[step.Event.NodeID] {
			return fmt.Errorf("scenario_node_not_in_topology: %s", step.Event.NodeID)
		}
	}
	if s.MemoryFieldIsolation != nil {
		if !known[s.MemoryFieldIsolation.Baseline.NodeID] {
			return fmt.Errorf("scenario_node_not_in_topology: memory baseline")
		}
		for _, variant := range s.MemoryFieldIsolation.Variants {
			if !known[variant.Event.NodeID] {
				return fmt.Errorf("scenario_node_not_in_topology: memory variant %s", variant.ID)
			}
		}
	}
	return nil
}
func (e ScenarioEvent) validate() error {
	if !safeToken(e.EventID, 128) || !boundedText(e.EventType, 64) || (e.EventType != "vision.identity" && e.EventType != "vision.unknown" && e.EventType != "vision.uncertain") || !safeToken(e.NodeID, 128) {
		return errors.New("invalid_scenario_event")
	}
	if e.IdentityConfidencePermille < 0 || e.IdentityConfidencePermille > 1000 {
		return errors.New("invalid_identity_confidence")
	}
	if e.TimestampMode != TimestampAbsolute && e.TimestampMode != TimestampRelative {
		return errors.New("invalid_timestamp_mode")
	}
	if e.TimestampMode == TimestampAbsolute && (e.AbsoluteTime == nil || e.AbsoluteTime.IsZero()) {
		return errors.New("absolute_timestamp_missing")
	}
	if e.TimestampMode == TimestampRelative && e.RelativeOffset.Duration() < 0 {
		return errors.New("relative_timestamp_negative")
	}
	if e.HouseMode != "" && e.HouseMode != cgecontext.HouseModeUnknown && e.HouseMode != cgecontext.HouseModeHome && e.HouseMode != cgecontext.HouseModeAway && e.HouseMode != cgecontext.HouseModeNight && e.HouseMode != cgecontext.HouseModeSleep && e.HouseMode != cgecontext.HouseModeArmed {
		return errors.New("invalid_house_mode")
	}
	if e.Occupancy != "" && e.Occupancy != cgecontext.OccupancyUnknown && e.Occupancy != cgecontext.OccupancyOccupied && e.Occupancy != cgecontext.OccupancyUnoccupied {
		return errors.New("invalid_occupancy")
	}
	if e.ContextQuality != "" && e.ContextQuality != cgecontext.QualityComplete && e.ContextQuality != cgecontext.QualityPartial && e.ContextQuality != cgecontext.QualityUnknown {
		return errors.New("invalid_context_quality")
	}
	if e.Identity.Kind != IdentityKnown && e.Identity.Kind != IdentityUnknown && e.Identity.Kind != IdentityUncertain && e.Identity.Kind != IdentityNone {
		return errors.New("invalid_identity_kind")
	}
	if e.Identity.Kind == IdentityKnown && !safeToken(e.Identity.EntityID, 128) {
		return errors.New("known_identity_missing")
	}
	for _, id := range append(append([]string{}, e.Identity.CandidateEntityIDs...), e.Identity.EntityID) {
		if id != "" && !safeToken(id, 128) {
			return errors.New("invalid_identity_id")
		}
	}
	return nil
}
func (p ExpectedProperty) Validate() error {
	if !safeToken(p.Code, 128) || !validLocalized(p.Description) || !expectedScopes[p.Scope] || !expectedOperators[p.Operator] || (p.Severity != ExpectedInformational && p.Severity != Expected && p.Severity != ExpectedImportant) || !validExpectedValue(p.Value) {
		return errors.New("invalid_expected_property")
	}
	return nil
}
func validExpectedValue(value any) bool {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 4096 {
		return false
	}
	switch typed := value.(type) {
	case nil, string, bool, float64, float32, int, int64, uint64, uint16:
		if text, ok := typed.(string); ok {
			lower := strings.ToLower(text)
			return !strings.ContainsAny(text, "<>\x00") && !strings.Contains(lower, "javascript:") && !strings.Contains(lower, "<script")
		}
		return true
	case []any:
		for _, item := range value.([]any) {
			if !validExpectedValue(item) {
				return false
			}
		}
		return true
	case []string:
		for _, item := range value.([]string) {
			if !validExpectedValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type ScenarioLibrary struct{ scenarios map[string]Scenario }

func LoadScenarioLibrary(files map[string][]byte) (*ScenarioLibrary, error) {
	library := &ScenarioLibrary{scenarios: map[string]Scenario{}}
	for name, data := range files {
		if len(data) > 1<<20 || strings.HasSuffix(name, "scenario-schema.json") {
			continue
		}
		var scenario Scenario
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&scenario); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", name, err)
		}
		if err := scenario.Validate(); err != nil {
			return nil, fmt.Errorf("scenario %s: %w", name, err)
		}
		if _, exists := library.scenarios[scenario.ID]; exists {
			return nil, fmt.Errorf("duplicate scenario id: %s", scenario.ID)
		}
		library.scenarios[scenario.ID] = scenario
	}
	return library, nil
}
func (l *ScenarioLibrary) List() []Scenario {
	if l == nil {
		return nil
	}
	values := make([]Scenario, 0, len(l.scenarios))
	for _, value := range l.scenarios {
		values = append(values, cloneScenario(value))
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values
}
func (l *ScenarioLibrary) Get(id string) (Scenario, bool) {
	if l == nil {
		return Scenario{}, false
	}
	value, ok := l.scenarios[id]
	if !ok {
		return Scenario{}, false
	}
	return cloneScenario(value), true
}
func (s Scenario) Info() ScenarioInfo {
	minutes := 1
	for _, step := range s.Steps {
		if step.AdvanceTime != nil {
			minutes += step.AdvanceTime.Minutes
		}
		if step.Repeat != nil {
			minutes += int(step.Repeat.Interval.Duration().Minutes()) * step.Repeat.Count
		}
	}
	return ScenarioInfo{ID: s.ID, Title: s.Title.FR, Description: s.Description.FR, LocalizedTitle: s.Title, LocalizedDescription: s.Description, Category: s.Category, Difficulty: s.Difficulty, Minutes: minutes, StepCount: len(s.Steps), Tags: append([]string(nil), s.Tags...), Unsupported: s.UnsupportedReason != nil}
}
func (s Scenario) Fingerprint() string {
	data, _ := json.Marshal(s)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func cloneScenario(s Scenario) Scenario {
	data, _ := json.Marshal(s)
	var copy Scenario
	_ = json.Unmarshal(data, &copy)
	return copy
}
