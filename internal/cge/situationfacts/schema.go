package situationfacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

type ConflictPolicy string

const (
	ConflictAllow       ConflictPolicy = "allow"
	ConflictSingleValue ConflictPolicy = "single_value"
)

type FactDefinition struct {
	Code           FactCode
	Scope          FactScope
	ValueKind      FactValueKind
	Description    string
	AllowsMultiple bool
	ConflictPolicy ConflictPolicy
}

type FactSchema struct {
	Version     string
	Definitions []FactDefinition
	index       map[FactCode]FactDefinition
}

func (s FactSchema) Definition(code FactCode) (FactDefinition, bool) {
	if s.index != nil {
		definition, ok := s.index[code]
		return definition, ok
	}
	for _, definition := range s.Definitions {
		if definition.Code == code {
			return definition, true
		}
	}
	return FactDefinition{}, false
}

func (s FactSchema) Validate() error {
	if s.Version == "" || len(s.Definitions) == 0 {
		return ErrUnknownFactCode
	}
	last := FactCode("")
	for _, definition := range s.Definitions {
		if definition.Code == "" || !validScope(definition.Scope) || !validValueKind(definition.ValueKind) || definition.Description == "" || definition.ConflictPolicy != ConflictAllow && definition.ConflictPolicy != ConflictSingleValue || last != "" && last >= definition.Code || containsForbiddenNeutralTerm(string(definition.Code)) || containsForbiddenNeutralTerm(definition.Description) {
			return ErrInvalidFact
		}
		last = definition.Code
	}
	return nil
}

func containsForbiddenNeutralTerm(value string) bool {
	lowered := strings.ToLower(value)
	for _, term := range []string{"intrusion", "threat", "danger", "malicious", "suspicious", "intent", "visitor_expected", "emergency", "attack", "compromise", "safe", "unsafe"} {
		if strings.Contains(lowered, term) {
			return true
		}
	}
	return false
}

func ValidateFactAgainstSchema(f Fact) error {
	return f.Validate(Schema(), DefaultPolicy())
}

const (
	CodeEpisodeStatus               FactCode = "episode.status"
	CodeEpisodeObservationCount     FactCode = "episode.observation_count"
	CodeEpisodeDurationMS           FactCode = "episode.duration_ms"
	CodeEpisodeEntityCount          FactCode = "episode.entity_count"
	CodeEpisodeNodeCount            FactCode = "episode.node_count"
	CodeEpisodeZoneCount            FactCode = "episode.zone_count"
	CodeEpisodeChainCount           FactCode = "episode.chain_count"
	CodeEpisodeRoutineCount         FactCode = "episode.routine_count"
	CodeEpisodeEventTypeSet         FactCode = "episode.event_type_set"
	CodeEpisodeContextQualitySet    FactCode = "episode.context_quality_set"
	CodeEpisodeMultipleObservations FactCode = "episode.multiple_observations"
	CodeEpisodeMultipleEntities     FactCode = "episode.multiple_entities"
	CodeEpisodeStartedAt            FactCode = "episode.started_at"
	CodeEpisodeLastObservedAt       FactCode = "episode.last_observed_at"

	CodeIdentityKnownPresent          FactCode = "identity.known_present"
	CodeIdentityUnknownPresent        FactCode = "identity.unknown_present"
	CodeIdentityUncertainPresent      FactCode = "identity.uncertain_present"
	CodeIdentityNonePresent           FactCode = "identity.none_present"
	CodeIdentityKnownEntitySet        FactCode = "identity.known_entity_set"
	CodeIdentityCandidateEntitySet    FactCode = "identity.candidate_entity_set"
	CodeIdentityMultipleKnownEntities FactCode = "identity.multiple_known_entities"
	CodeIdentityStateChanged          FactCode = "identity.state_changed"
	CodeIdentityConflict              FactCode = "identity.conflict"

	CodeSpatialStartNode                  FactCode = "spatial.start_node"
	CodeSpatialEndNode                    FactCode = "spatial.end_node"
	CodeSpatialNodeSequence               FactCode = "spatial.node_sequence"
	CodeSpatialZoneSequence               FactCode = "spatial.zone_sequence"
	CodeSpatialTransitionCount            FactCode = "spatial.transition_count"
	CodeSpatialReachableTransitionCount   FactCode = "spatial.reachable_transition_count"
	CodeSpatialUnreachableTransitionCount FactCode = "spatial.unreachable_transition_count"
	CodeSpatialUnknownTransitionCount     FactCode = "spatial.unknown_transition_count"
	CodeSpatialTopologyAvailable          FactCode = "spatial.topology_available"

	CodeTemporalDurationMS        FactCode = "temporal.duration_ms"
	CodeTemporalOutOfOrderPresent FactCode = "temporal.out_of_order_observation_present"
	CodeTemporalDaypartSet        FactCode = "temporal.daypart_set"
	CodeTemporalWeekdaySet        FactCode = "temporal.weekday_set"
	CodeTemporalMinimumGapMS      FactCode = "temporal.minimum_gap_ms"
	CodeTemporalMaximumGapMS      FactCode = "temporal.maximum_gap_ms"
	CodeTemporalAverageGapMS      FactCode = "temporal.average_gap_ms"

	CodeContextHouseModeSet      FactCode = "context.house_mode_set"
	CodeContextHouseModeChanged  FactCode = "context.house_mode_changed"
	CodeContextHouseModeConflict FactCode = "context.house_mode_conflict"
	CodeContextOccupancySet      FactCode = "context.occupancy_set"
	CodeContextOccupancyChanged  FactCode = "context.occupancy_changed"
	CodeContextOccupancyConflict FactCode = "context.occupancy_conflict"
	CodeContextCompleteCount     FactCode = "context.complete_observation_count"
	CodeContextPartialCount      FactCode = "context.partial_observation_count"
	CodeContextMissingCount      FactCode = "context.missing_observation_count"
	CodeContextPartialPresent    FactCode = "context.partial_present"
	CodeContextMissingPresent    FactCode = "context.missing_present"

	CodeContinuityActivationCount        FactCode = "continuity.activation_count"
	CodeContinuityTrackCount             FactCode = "continuity.track_count"
	CodeContinuitySequenceCount          FactCode = "continuity.sequence_count"
	CodeContinuitySharedActivation       FactCode = "continuity.shared_activation"
	CodeContinuitySharedTrack            FactCode = "continuity.shared_track"
	CodeContinuitySharedSequence         FactCode = "continuity.shared_sequence"
	CodeContinuityMultipleNodesSameTrack FactCode = "continuity.multiple_nodes_same_track"
	CodeContinuityMultipleClips          FactCode = "continuity.multiple_clips"

	CodeMemoryChainRefCount                FactCode = "memory.chain_ref_count"
	CodeMemoryRoutineRefCount              FactCode = "memory.routine_ref_count"
	CodeMemoryDeviationPresent             FactCode = "memory.deviation_present"
	CodeMemoryDeviationEvaluated           FactCode = "memory.deviation_evaluated"
	CodeMemoryDeviationStatusSet           FactCode = "memory.deviation_status_set"
	CodeMemoryDeviationBandSet             FactCode = "memory.deviation_band_set"
	CodeMemoryDeviationMaximumScore        FactCode = "memory.deviation.maximum_score"
	CodeMemoryDeviationMaximumCoverage     FactCode = "memory.deviation.maximum_coverage"
	CodeMemoryDeviationStructuralAvailable FactCode = "memory.deviation.structural_available"
	CodeMemoryDeviationTemporalAvailable   FactCode = "memory.deviation.temporal_available"
	CodeMemoryDeviationIntervalAvailable   FactCode = "memory.deviation.interval_available"
	CodeMemoryDeviationStructuralPositive  FactCode = "memory.deviation.structural_positive"
	CodeMemoryDeviationTemporalPositive    FactCode = "memory.deviation.temporal_positive"
	CodeMemoryDeviationIntervalPositive    FactCode = "memory.deviation.interval_positive"
)

func allCodes() []FactCode {
	values := []FactCode{
		CodeEpisodeStatus, CodeEpisodeObservationCount, CodeEpisodeDurationMS, CodeEpisodeEntityCount, CodeEpisodeNodeCount, CodeEpisodeZoneCount, CodeEpisodeChainCount, CodeEpisodeRoutineCount, CodeEpisodeEventTypeSet, CodeEpisodeContextQualitySet, CodeEpisodeMultipleObservations, CodeEpisodeMultipleEntities, CodeEpisodeStartedAt, CodeEpisodeLastObservedAt,
		CodeIdentityKnownPresent, CodeIdentityUnknownPresent, CodeIdentityUncertainPresent, CodeIdentityNonePresent, CodeIdentityKnownEntitySet, CodeIdentityCandidateEntitySet, CodeIdentityMultipleKnownEntities, CodeIdentityStateChanged, CodeIdentityConflict,
		CodeSpatialStartNode, CodeSpatialEndNode, CodeSpatialNodeSequence, CodeSpatialZoneSequence, CodeSpatialTransitionCount, CodeSpatialReachableTransitionCount, CodeSpatialUnreachableTransitionCount, CodeSpatialUnknownTransitionCount, CodeSpatialTopologyAvailable,
		CodeTemporalDurationMS, CodeTemporalOutOfOrderPresent, CodeTemporalDaypartSet, CodeTemporalWeekdaySet, CodeTemporalMinimumGapMS, CodeTemporalMaximumGapMS, CodeTemporalAverageGapMS,
		CodeContextHouseModeSet, CodeContextHouseModeChanged, CodeContextHouseModeConflict, CodeContextOccupancySet, CodeContextOccupancyChanged, CodeContextOccupancyConflict, CodeContextCompleteCount, CodeContextPartialCount, CodeContextMissingCount, CodeContextPartialPresent, CodeContextMissingPresent,
		CodeContinuityActivationCount, CodeContinuityTrackCount, CodeContinuitySequenceCount, CodeContinuitySharedActivation, CodeContinuitySharedTrack, CodeContinuitySharedSequence, CodeContinuityMultipleNodesSameTrack, CodeContinuityMultipleClips,
		CodeMemoryChainRefCount, CodeMemoryRoutineRefCount, CodeMemoryDeviationPresent, CodeMemoryDeviationEvaluated, CodeMemoryDeviationStatusSet, CodeMemoryDeviationBandSet, CodeMemoryDeviationMaximumScore, CodeMemoryDeviationMaximumCoverage, CodeMemoryDeviationStructuralAvailable, CodeMemoryDeviationTemporalAvailable, CodeMemoryDeviationIntervalAvailable, CodeMemoryDeviationStructuralPositive, CodeMemoryDeviationTemporalPositive, CodeMemoryDeviationIntervalPositive,
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

// AllFactCodes returns the canonical list used by the neutral schema.
func AllFactCodes() []FactCode { return append([]FactCode(nil), allCodes()...) }

func definition(code FactCode, scope FactScope, kind FactValueKind, multiple bool, conflict ConflictPolicy, description string) FactDefinition {
	return FactDefinition{Code: code, Scope: scope, ValueKind: kind, AllowsMultiple: multiple, ConflictPolicy: conflict, Description: description}
}

func buildSchema() FactSchema {
	defs := make([]FactDefinition, 0, 80)
	add := func(code FactCode, scope FactScope, kind FactValueKind, multiple bool, conflict ConflictPolicy) {
		defs = append(defs, definition(code, scope, kind, multiple, conflict, "neutral descriptive fact"))
	}
	add(CodeEpisodeStatus, ScopeEpisode, ValueString, false, ConflictSingleValue)
	add(CodeEpisodeObservationCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeDurationMS, ScopeEpisode, ValueDurationMS, false, ConflictSingleValue)
	add(CodeEpisodeEntityCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeNodeCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeZoneCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeChainCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeRoutineCount, ScopeEpisode, ValueInt, false, ConflictSingleValue)
	add(CodeEpisodeEventTypeSet, ScopeEpisode, ValueStringSet, false, ConflictSingleValue)
	add(CodeEpisodeContextQualitySet, ScopeEpisode, ValueStringSet, false, ConflictSingleValue)
	add(CodeEpisodeMultipleObservations, ScopeEpisode, ValueBool, false, ConflictSingleValue)
	add(CodeEpisodeMultipleEntities, ScopeEpisode, ValueBool, false, ConflictSingleValue)
	add(CodeEpisodeStartedAt, ScopeEpisode, ValueTimestamp, false, ConflictSingleValue)
	add(CodeEpisodeLastObservedAt, ScopeEpisode, ValueTimestamp, false, ConflictSingleValue)
	add(CodeIdentityKnownPresent, ScopeEntity, ValueBool, true, ConflictAllow)
	add(CodeIdentityUnknownPresent, ScopeEntity, ValueBool, true, ConflictAllow)
	add(CodeIdentityUncertainPresent, ScopeEntity, ValueBool, true, ConflictAllow)
	add(CodeIdentityNonePresent, ScopeEntity, ValueBool, true, ConflictAllow)
	add(CodeIdentityKnownEntitySet, ScopeEntity, ValueStringSet, false, ConflictSingleValue)
	add(CodeIdentityCandidateEntitySet, ScopeEntity, ValueStringSet, false, ConflictSingleValue)
	add(CodeIdentityMultipleKnownEntities, ScopeEntity, ValueBool, false, ConflictSingleValue)
	add(CodeIdentityStateChanged, ScopeEntity, ValueBool, false, ConflictSingleValue)
	add(CodeIdentityConflict, ScopeEntity, ValueBool, false, ConflictSingleValue)
	add(CodeSpatialStartNode, ScopeTransition, ValueString, false, ConflictSingleValue)
	add(CodeSpatialEndNode, ScopeTransition, ValueString, false, ConflictSingleValue)
	add(CodeSpatialNodeSequence, ScopeTransition, ValueStringList, false, ConflictSingleValue)
	add(CodeSpatialZoneSequence, ScopeTransition, ValueStringList, false, ConflictSingleValue)
	add(CodeSpatialTransitionCount, ScopeTransition, ValueInt, false, ConflictSingleValue)
	add(CodeSpatialReachableTransitionCount, ScopeTransition, ValueInt, false, ConflictSingleValue)
	add(CodeSpatialUnreachableTransitionCount, ScopeTransition, ValueInt, false, ConflictSingleValue)
	add(CodeSpatialUnknownTransitionCount, ScopeTransition, ValueInt, false, ConflictSingleValue)
	add(CodeSpatialTopologyAvailable, ScopeTransition, ValueBool, false, ConflictSingleValue)
	add(CodeTemporalDurationMS, ScopeEpisode, ValueDurationMS, false, ConflictSingleValue)
	add(CodeTemporalOutOfOrderPresent, ScopeEpisode, ValueBool, false, ConflictSingleValue)
	add(CodeTemporalDaypartSet, ScopeEpisode, ValueStringSet, false, ConflictSingleValue)
	add(CodeTemporalWeekdaySet, ScopeEpisode, ValueStringSet, false, ConflictSingleValue)
	add(CodeTemporalMinimumGapMS, ScopeEpisode, ValueDurationMS, false, ConflictSingleValue)
	add(CodeTemporalMaximumGapMS, ScopeEpisode, ValueDurationMS, false, ConflictSingleValue)
	add(CodeTemporalAverageGapMS, ScopeEpisode, ValueDurationMS, false, ConflictSingleValue)
	add(CodeContextHouseModeSet, ScopeContext, ValueStringSet, false, ConflictSingleValue)
	add(CodeContextHouseModeChanged, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContextHouseModeConflict, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContextOccupancySet, ScopeContext, ValueStringSet, false, ConflictSingleValue)
	add(CodeContextOccupancyChanged, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContextOccupancyConflict, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContextCompleteCount, ScopeContext, ValueInt, false, ConflictSingleValue)
	add(CodeContextPartialCount, ScopeContext, ValueInt, false, ConflictSingleValue)
	add(CodeContextMissingCount, ScopeContext, ValueInt, false, ConflictSingleValue)
	add(CodeContextPartialPresent, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContextMissingPresent, ScopeContext, ValueBool, false, ConflictSingleValue)
	add(CodeContinuityActivationCount, ScopeObservation, ValueInt, false, ConflictSingleValue)
	add(CodeContinuityTrackCount, ScopeObservation, ValueInt, false, ConflictSingleValue)
	add(CodeContinuitySequenceCount, ScopeObservation, ValueInt, false, ConflictSingleValue)
	add(CodeContinuitySharedActivation, ScopeObservation, ValueBool, false, ConflictSingleValue)
	add(CodeContinuitySharedTrack, ScopeObservation, ValueBool, false, ConflictSingleValue)
	add(CodeContinuitySharedSequence, ScopeObservation, ValueBool, false, ConflictSingleValue)
	add(CodeContinuityMultipleNodesSameTrack, ScopeObservation, ValueBool, false, ConflictSingleValue)
	add(CodeContinuityMultipleClips, ScopeObservation, ValueInt, false, ConflictSingleValue)
	add(CodeMemoryChainRefCount, ScopeMemory, ValueInt, false, ConflictSingleValue)
	add(CodeMemoryRoutineRefCount, ScopeMemory, ValueInt, false, ConflictSingleValue)
	add(CodeMemoryDeviationPresent, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationEvaluated, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationStatusSet, ScopeMemory, ValueStringSet, false, ConflictSingleValue)
	add(CodeMemoryDeviationBandSet, ScopeMemory, ValueStringSet, false, ConflictSingleValue)
	add(CodeMemoryDeviationMaximumScore, ScopeMemory, ValuePermille, false, ConflictSingleValue)
	add(CodeMemoryDeviationMaximumCoverage, ScopeMemory, ValuePermille, false, ConflictSingleValue)
	add(CodeMemoryDeviationStructuralAvailable, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationTemporalAvailable, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationIntervalAvailable, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationStructuralPositive, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationTemporalPositive, ScopeMemory, ValueBool, false, ConflictSingleValue)
	add(CodeMemoryDeviationIntervalPositive, ScopeMemory, ValueBool, false, ConflictSingleValue)
	sort.Slice(defs, func(i, j int) bool { return defs[i].Code < defs[j].Code })
	index := make(map[FactCode]FactDefinition, len(defs))
	for _, value := range defs {
		index[value.Code] = value
	}
	return FactSchema{Version: "situation-facts-schema-v1", Definitions: defs, index: index}
}

var schemaCache struct {
	sync.Once
	schema      FactSchema
	fingerprint string
}

func compiledSchema() FactSchema {
	schemaCache.Do(func() {
		schemaCache.schema = buildSchema()
		payload, _ := json.Marshal(schemaCache.schema)
		digest := sha256.Sum256(payload)
		schemaCache.fingerprint = "situation-facts-schema-v1:" + hex.EncodeToString(digest[:])
	})
	return schemaCache.schema
}

// Schema returns a defensive public copy of the immutable built-in schema.
func Schema() FactSchema {
	compiled := compiledSchema()
	index := make(map[FactCode]FactDefinition, len(compiled.index))
	for code, definition := range compiled.index {
		index[code] = definition
	}
	return FactSchema{Version: compiled.Version, Definitions: append([]FactDefinition(nil), compiled.Definitions...), index: index}
}

func SchemaFingerprint() string {
	compiledSchema()
	return schemaCache.fingerprint
}

type Policy struct {
	MaxFactsPerEpisode   int
	MaxProvenancePerFact int
	MaxStringLength      int
	MaxSetValues         int
	MaxSequenceValues    int

	IncludeUnknownFacts    bool
	IncludeRetractedInDiff bool
}

func DefaultPolicy() Policy {
	return Policy{MaxFactsPerEpisode: 256, MaxProvenancePerFact: 64, MaxStringLength: 256, MaxSetValues: 64, MaxSequenceValues: 256, IncludeUnknownFacts: true, IncludeRetractedInDiff: false}
}

func (p Policy) Validate() error {
	if p.MaxFactsPerEpisode <= 0 || p.MaxProvenancePerFact <= 0 || p.MaxStringLength <= 0 || p.MaxSetValues <= 0 || p.MaxSequenceValues <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p Policy) Fingerprint() string {
	if p.Validate() != nil {
		return "situation-facts-policy-v1:invalid"
	}
	payload, _ := json.Marshal(struct {
		Facts, Provenance, Strings, Sets, Sequences int
		Unknown, Retracted                          bool
	}{p.MaxFactsPerEpisode, p.MaxProvenancePerFact, p.MaxStringLength, p.MaxSetValues, p.MaxSequenceValues, p.IncludeUnknownFacts, p.IncludeRetractedInDiff})
	digest := sha256.Sum256(payload)
	return "situation-facts-policy-v1:" + hex.EncodeToString(digest[:])
}

func zeroValue(kind FactValueKind) FactValue {
	switch kind {
	case ValueBool:
		return BoolFactValue(false)
	case ValueInt:
		return IntFactValue(0)
	case ValuePermille:
		return PermilleFactValue(0)
	case ValueString:
		return StringFactValue("unknown")
	case ValueTimestamp:
		return TimestampFactValue(time.Unix(0, 0))
	case ValueDurationMS:
		return DurationMSFactValue(0)
	case ValueStringSet:
		return StringSetFactValue(nil)
	case ValueStringList:
		return StringListFactValue(nil)
	case ValueRef:
		return RefFactValue("unknown")
	}
	return FactValue{}
}
