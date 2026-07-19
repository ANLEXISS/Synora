package cge

import (
	"context"
	"time"

	"synora/pkg/contract"
)

// CognitiveEngine is the passive boundary between Core and the future
// Cognitive Graph Engine. Implementations observe events and expose
// read-only diagnostics; they do not own security decisions or actions.
type CognitiveEngine interface {
	Observe(context.Context, Event) (ObservationResult, error)
	Snapshot(context.Context) (Snapshot, error)
	Explain(context.Context, string) (Explanation, error)
}

// CognitiveCloser is optional so Core does not need to know whether an
// implementation owns durable resources.
type CognitiveCloser interface {
	Close() error
}

// Event is the immutable, minimal event representation exchanged across the
// Core/CGE boundary. It intentionally contains no payload map or action data.
// This keeps shadow observers from mutating the source contract.Event.
type Event struct {
	ID           string
	Type         string
	Source       string
	Timestamp    time.Time
	DeviceID     string
	NodeID       string
	Identity     string
	Confidence   float64
	Priority     int
	GroupKey     string
	TrackID      string
	ClipID       string
	ClipIndex    int
	ActivationID string
	SequenceKey  string
}

// EventFromContract creates the immutable boundary representation used by
// Core. It copies scalar values only and never mutates the source event.
func EventFromContract(event *contract.Event) Event {
	if event == nil {
		return Event{}
	}
	return Event{
		ID:           event.ID,
		Type:         event.Type,
		Source:       event.Source,
		Timestamp:    event.Timestamp,
		DeviceID:     event.DeviceID,
		NodeID:       event.NodeID,
		Identity:     event.Identity,
		Confidence:   event.Confidence,
		Priority:     event.Priority,
		GroupKey:     event.GroupKey,
		TrackID:      event.TrackID,
		ClipID:       event.ClipID,
		ClipIndex:    event.ClipIndex,
		ActivationID: event.ActivationID,
		SequenceKey:  event.SequenceKey,
	}
}

// ObservationResult is deliberately limited to integration diagnostics.
type ObservationResult struct {
	ObservedAt       time.Time
	ObservationCount uint64
	LastEventType    string
}

// Snapshot is the read-only state exposed by a CGE implementation in pass 1.
type Snapshot struct {
	ObservationCount uint64
	LastObservedAt   time.Time
	LastEventType    string

	CognitiveShadowEnabled                bool
	AutoEvidenceEnabled                   bool
	MaxEvidenceReevaluations              int
	CoordinatorState                      string
	ChainCount                            int
	HypothesisCount                       int
	RoutineLearningEnabled                bool
	RoutineTemporalBucketMinutes          int
	RoutineAllowPartialContext            bool
	RoutineMaxTransitionGap               time.Duration
	RoutineRequireSameTopologyRevision    bool
	RoutineCount                          int
	DeviationEnabled                      bool
	DeviationPolicyNamespace              string
	DeviationPolicyVersion                string
	DeviationRecentAssessmentLimit        int
	DeviationMaxAssessmentsPerObservation int
	DeviationAssessmentStoreCount         int
	ContextEnabled                        bool
	ContextAllowPartial                   bool
	ContextSchemaVersion                  string
	FieldTrialEnabled                     bool
	FieldTrialSessionOpen                 bool
	FieldTrialEventCount                  uint64
	FieldTrialSegmentCount                int
	FieldTrialBytes                       int64
	FieldTrialState                       string
	FieldTrialErrors                      uint64
	FieldTrialLastSequence                uint64
	CognitiveMetrics                      MetricsSnapshot
}

// Explanation is reserved for future explanations. Pass 1 has no decision
// or action content to explain.
type Explanation struct {
	SituationID                           string
	Available                             bool
	Text                                  string
	CognitiveShadowEnabled                bool
	AutoEvidenceEnabled                   bool
	MaxEvidenceReevaluations              int
	CoordinatorState                      string
	ChainCount                            int
	HypothesisCount                       int
	RoutineLearningEnabled                bool
	RoutineTemporalBucketMinutes          int
	RoutineAllowPartialContext            bool
	RoutineMaxTransitionGap               time.Duration
	RoutineRequireSameTopologyRevision    bool
	RoutineCount                          int
	DeviationEnabled                      bool
	DeviationPolicyNamespace              string
	DeviationPolicyVersion                string
	DeviationRecentAssessmentLimit        int
	DeviationMaxAssessmentsPerObservation int
	DeviationAssessmentStoreCount         int
	ContextEnabled                        bool
	ContextAllowPartial                   bool
	ContextSchemaVersion                  string
	FieldTrialEnabled                     bool
	FieldTrialSessionOpen                 bool
	FieldTrialEventCount                  uint64
	FieldTrialSegmentCount                int
	FieldTrialBytes                       int64
	FieldTrialState                       string
	FieldTrialErrors                      uint64
	FieldTrialLastSequence                uint64
	CognitiveMetrics                      MetricsSnapshot
}
