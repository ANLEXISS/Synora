package durable

import (
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/replay"
	"synora/internal/cge/hypotheses"
	hypothesisreplay "synora/internal/cge/hypotheses/replay"
	"synora/internal/cge/routines"
	routinereplay "synora/internal/cge/routines/replay"
)

// CoordinatorState describes the durability boundary's operational state.
type CoordinatorState string

const (
	StateReady    CoordinatorState = "ready"
	StateDegraded CoordinatorState = "degraded"
	StateClosed   CoordinatorState = "closed"
)

// MutationKind identifies the durable mutations coordinated by the CGE WAL.
type MutationKind string

const (
	MutationChainAdded              MutationKind = "chain_added"
	MutationLifecycleTransitioned   MutationKind = "lifecycle_transitioned"
	MutationObservationAdded        MutationKind = "observation_added"
	MutationContributionAdded       MutationKind = "contribution_added"
	MutationHypothesisOpened        MutationKind = "hypothesis_opened"
	MutationHypothesisStatusChanged MutationKind = "hypothesis_status_changed"
	MutationHypothesisResolved      MutationKind = "hypothesis_resolved"
	MutationRoutineCreated          MutationKind = "routine_created"
	MutationRoutineOccurrenceAdded  MutationKind = "routine_occurrence_added"
	MutationRoutineStatusChanged    MutationKind = "routine_status_changed"
)

// StatusSnapshot is a defensive operational view. DegradedReason is a stable
// category, never a raw filesystem or payload error.
type StatusSnapshot struct {
	State CoordinatorState

	ChainCount      int
	HypothesisCount int
	RoutineCount    int

	JournalSequence uint64
	JournalHeadHash string

	DegradedReason string
}

// MutationResult contains only detached values from one prepared and
// journaled mutation. Published is false when a durable append succeeded but
// publication was deliberately interrupted and the coordinator degraded.
type MutationResult struct {
	Kind    MutationKind
	ChainID chains.ChainID

	Before             *chains.Snapshot
	After              chains.Snapshot
	Revision           chains.RevisionRecord
	ContributionID     string
	PreviousConfidence float64
	NewConfidence      float64

	JournalSequence   uint64
	JournalRecordHash string

	Published bool
}

// RecoveryMetadata describes the validated replay used to construct or
// recover a coordinator.
type RecoveryMetadata struct {
	Replay replay.ReplayMetadata

	State            CoordinatorState
	ChainCount       int
	HypothesisCount  int
	JournalSequence  uint64
	JournalHeadHash  string
	HypothesisReplay hypothesisreplay.ReplayMetadata
	RoutineReplay    routinereplay.Metadata
}

type RoutineOccurrenceResult struct {
	RoutineID         routines.RoutineID
	OccurrenceID      routines.OccurrenceID
	Before            *routines.Snapshot
	After             routines.Snapshot
	Revision          routines.RevisionRecord
	Created           bool
	Applied           bool
	Idempotent        bool
	Published         bool
	JournalSequence   uint64
	JournalRecordHash string
}

type RoutineStatusResult struct {
	RoutineID         routines.RoutineID
	Before            *routines.Snapshot
	After             routines.Snapshot
	Revision          routines.RevisionRecord
	Applied           bool
	Published         bool
	JournalSequence   uint64
	JournalRecordHash string
}

type RoutineLearningResult struct {
	ChainID         chains.ChainID
	Results         []routines.LearningOccurrenceResult
	AppliedCount    int
	IdempotentCount int
	ErrorCount      int
}

// AppendOutcome classifies what can be established after an append error.
type AppendOutcome string

const (
	AppendRejected  AppendOutcome = "rejected"
	AppendUncertain AppendOutcome = "durability_uncertain"
)

func validRecordedAt(value time.Time) bool { return !value.IsZero() }

// HypothesisMutationKind identifies one explicit durable hypothesis mutation.
type HypothesisMutationKind string

const (
	HypothesisMutationOpened        HypothesisMutationKind = "hypothesis_opened"
	HypothesisMutationStatusChanged HypothesisMutationKind = "hypothesis_status_changed"
	HypothesisMutationRebased       HypothesisMutationKind = "hypothesis_rebased"
	HypothesisMutationSuperseded    HypothesisMutationKind = "hypothesis_superseded"
)

type HypothesisMutationResult struct {
	Kind  HypothesisMutationKind
	SetID hypotheses.SetID

	Before *hypotheses.Snapshot
	After  hypotheses.Snapshot

	Revision hypotheses.RevisionRecord

	JournalSequence   uint64
	JournalRecordHash string

	Published  bool
	Applied    bool
	Idempotent bool

	PreviousAssessmentVersion     uint64
	NewAssessmentVersion          uint64
	PreviousAssessmentID          string
	NewAssessmentID               string
	PreviousAssessmentFingerprint string
	NewAssessmentFingerprint      string
}

type HypothesisSupersessionResult struct {
	PreviousSetID      hypotheses.SetID
	NewSetID           hypotheses.SetID
	PreviousBefore     hypotheses.Snapshot
	PreviousAfter      hypotheses.Snapshot
	NewAfter           hypotheses.Snapshot
	PreviousRevision   hypotheses.RevisionRecord
	NewOpeningRevision hypotheses.RevisionRecord
	JournalSequence    uint64
	JournalRecordHash  string
	Applied            bool
	Idempotent         bool
	Published          bool
}

type HypothesisResolutionResult struct {
	SetID              hypotheses.SetID
	AlternativeID      string
	AlternativeKind    hypotheses.AlternativeKind
	EffectKind         hypotheses.ResolutionEffectKind
	HypothesisBefore   hypotheses.Snapshot
	HypothesisAfter    hypotheses.Snapshot
	ChainBefore        *chains.Snapshot
	ChainAfter         *chains.Snapshot
	Outcome            hypotheses.ResolutionOutcome
	HypothesisRevision hypotheses.RevisionRecord
	JournalSequence    uint64
	JournalRecordHash  string
	Applied            bool
	Idempotent         bool
	Published          bool
}
