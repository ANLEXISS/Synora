package fieldtrial

import (
	"sort"
	"strings"
	"time"
)

type TrialEvent struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`
	Sequence      uint64 `json:"sequence"`

	RecordedAt time.Time `json:"recorded_at"`
	ObservedAt time.Time `json:"observed_at"`

	EventRef   string `json:"event_ref"`
	SubjectRef string `json:"subject_ref,omitempty"`
	ChainRef   string `json:"chain_ref,omitempty"`
	NodeRef    string `json:"node_ref,omitempty"`
	ZoneRef    string `json:"zone_ref,omitempty"`

	ContextQuality string `json:"context_quality,omitempty"`
	NodeKind       string `json:"node_kind,omitempty"`
	EntryPoint     bool   `json:"entry_point,omitempty"`
	Exterior       bool   `json:"exterior,omitempty"`

	AssociationDecision string `json:"association_decision,omitempty"`
	HypothesisAction    string `json:"hypothesis_action,omitempty"`
	EvidenceDecision    string `json:"evidence_decision,omitempty"`
	EvidenceApplied     bool   `json:"evidence_applied,omitempty"`

	DeviationAttempted   bool   `json:"deviation_attempted,omitempty"`
	DeviationStatus      string `json:"deviation_status,omitempty"`
	DeviationBand        string `json:"deviation_band,omitempty"`
	DeviationScore       uint16 `json:"deviation_score,omitempty"`
	DeviationCoverage    uint16 `json:"deviation_coverage,omitempty"`
	DeviationFingerprint string `json:"deviation_fingerprint,omitempty"`

	BaselineRoutineCount  int    `json:"baseline_routine_count,omitempty"`
	BestMatchExactRoutine bool   `json:"best_match_exact_routine,omitempty"`
	BestMatchRevision     uint64 `json:"best_match_revision,omitempty"`
	BestMatchOccurrences  uint64 `json:"best_match_occurrences,omitempty"`
	BestMatchDistinctDays uint64 `json:"best_match_distinct_days,omitempty"`

	PresencePlanned      bool `json:"presence_planned,omitempty"`
	TransitionPlanned    bool `json:"transition_planned,omitempty"`
	PresenceApplied      bool `json:"presence_applied,omitempty"`
	TransitionApplied    bool `json:"transition_applied,omitempty"`
	PresenceIdempotent   bool `json:"presence_idempotent,omitempty"`
	TransitionIdempotent bool `json:"transition_idempotent,omitempty"`

	CoordinatorState       string `json:"coordinator_state,omitempty"`
	CognitiveWALSequence   uint64 `json:"cognitive_wal_sequence,omitempty"`
	CognitiveWALHashPrefix string `json:"cognitive_wal_hash_prefix,omitempty"`

	TotalLatencyMicros           uint64 `json:"total_latency_micros,omitempty"`
	AssociationLatencyMicros     uint64 `json:"association_latency_micros,omitempty"`
	EvidenceLatencyMicros        uint64 `json:"evidence_latency_micros,omitempty"`
	DeviationLatencyMicros       uint64 `json:"deviation_latency_micros,omitempty"`
	RoutineLearningLatencyMicros uint64 `json:"routine_learning_latency_micros,omitempty"`
	LatencyBreakdownAvailable    bool   `json:"latency_breakdown_available,omitempty"`

	ErrorCodes    []string `json:"error_codes,omitempty"`
	AttemptNumber uint32   `json:"attempt_number,omitempty"`
}

type EventInput struct {
	ObservedAt time.Time
	RecordedAt time.Time
	EventID    string
	SubjectID  string
	ChainID    string
	NodeID     string
	ZoneID     string

	ContextQuality string
	NodeKind       string
	EntryPoint     bool
	Exterior       bool

	AssociationDecision string
	HypothesisAction    string
	EvidenceDecision    string
	EvidenceApplied     bool

	DeviationAttempted    bool
	DeviationStatus       string
	DeviationBand         string
	DeviationScore        uint16
	DeviationCoverage     uint16
	DeviationFingerprint  string
	BaselineRoutineCount  int
	BestMatchExactRoutine bool
	BestMatchRevision     uint64
	BestMatchOccurrences  uint64
	BestMatchDistinctDays uint64

	PresencePlanned           bool
	TransitionPlanned         bool
	PresenceApplied           bool
	TransitionApplied         bool
	PresenceIdempotent        bool
	TransitionIdempotent      bool
	CoordinatorState          string
	CognitiveWALSequence      uint64
	CognitiveWALHash          string
	TotalLatency              time.Duration
	AssociationLatency        time.Duration
	EvidenceLatency           time.Duration
	DeviationLatency          time.Duration
	RoutineLearningLatency    time.Duration
	LatencyBreakdownAvailable bool
	ErrorCodes                []string
	AttemptNumber             uint32
}

func buildTrialEvent(input EventInput, sessionID string, sequence uint64, pseudo *Pseudonymizer, includeContext, includeLatencies bool) TrialEvent {
	event := TrialEvent{SchemaVersion: SchemaVersion, SessionID: sessionID, Sequence: sequence, RecordedAt: input.RecordedAt.UTC(), ObservedAt: input.ObservedAt.UTC(), EventRef: pseudo.Ref("event", input.EventID), SubjectRef: pseudo.Ref("subject", input.SubjectID), ChainRef: pseudo.Ref("chain", input.ChainID), NodeRef: pseudo.Ref("node", input.NodeID), ZoneRef: pseudo.Ref("zone", input.ZoneID), AssociationDecision: boundedCode(input.AssociationDecision), HypothesisAction: boundedCode(input.HypothesisAction), EvidenceDecision: boundedCode(input.EvidenceDecision), EvidenceApplied: input.EvidenceApplied, DeviationAttempted: input.DeviationAttempted, DeviationStatus: boundedCode(input.DeviationStatus), DeviationBand: boundedCode(input.DeviationBand), DeviationScore: input.DeviationScore, DeviationCoverage: input.DeviationCoverage, DeviationFingerprint: boundedCode(input.DeviationFingerprint), BaselineRoutineCount: input.BaselineRoutineCount, BestMatchExactRoutine: input.BestMatchExactRoutine, BestMatchRevision: input.BestMatchRevision, BestMatchOccurrences: input.BestMatchOccurrences, BestMatchDistinctDays: input.BestMatchDistinctDays, PresencePlanned: input.PresencePlanned, TransitionPlanned: input.TransitionPlanned, PresenceApplied: input.PresenceApplied, TransitionApplied: input.TransitionApplied, PresenceIdempotent: input.PresenceIdempotent, TransitionIdempotent: input.TransitionIdempotent, CoordinatorState: boundedCode(input.CoordinatorState), CognitiveWALSequence: input.CognitiveWALSequence, CognitiveWALHashPrefix: hashPrefix(input.CognitiveWALHash), AttemptNumber: input.AttemptNumber, ErrorCodes: normalizeCodes(input.ErrorCodes)}
	if includeContext {
		event.ContextQuality, event.NodeKind = boundedCode(input.ContextQuality), boundedCode(input.NodeKind)
		event.EntryPoint, event.Exterior = input.EntryPoint, input.Exterior
	}
	if includeLatencies {
		event.TotalLatencyMicros, event.AssociationLatencyMicros, event.EvidenceLatencyMicros, event.DeviationLatencyMicros, event.RoutineLearningLatencyMicros = micros(input.TotalLatency), micros(input.AssociationLatency), micros(input.EvidenceLatency), micros(input.DeviationLatency), micros(input.RoutineLearningLatency)
		event.LatencyBreakdownAvailable = input.LatencyBreakdownAvailable
	}
	return event
}

func micros(value time.Duration) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value / time.Microsecond)
}

func hashPrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 24 {
		return value[:24]
	}
	return value
}

func boundedCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > MaxErrorCodeLength {
		return value[:MaxErrorCodeLength]
	}
	return value
}

func normalizeCodes(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = boundedCode(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) == MaxErrorCodesPerEvent {
			break
		}
	}
	sort.Strings(result)
	return result
}
