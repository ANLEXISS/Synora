package journal

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/hypotheses"
	"synora/internal/cge/routines"
)

// CurrentSchemaVersion is the only journal schema accepted in this pass.
const CurrentSchemaVersion = 1

// GenesisPreviousHash is the fixed head predecessor for sequence one.
const GenesisPreviousHash = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"

const (
	defaultFileMode       fs.FileMode = 0o640
	defaultMaxJournalSize int64       = 256 << 20
	defaultMaxRecordSize  int         = 4 << 20
	maxActorLength                    = 128
	maxCorrelationLength              = 256
	maxPurposeLength                  = 256
)

// RecordKind identifies an implemented global journal operation. Reserved
// values are defined for forward API vocabulary but are not accepted by v1.
type RecordKind string

const (
	RecordKindGenesis                 RecordKind = "journal.genesis"
	RecordKindChainAdded              RecordKind = "chain.added"
	RecordKindLifecycleTransitioned   RecordKind = "chain.lifecycle_transitioned"
	RecordKindSnapshotCheckpointed    RecordKind = "snapshot.checkpointed"
	RecordKindObservationAdded        RecordKind = "chain.observation_added"
	RecordKindContributionAdded       RecordKind = "chain.contribution_added"
	RecordKindHypothesisOpened        RecordKind = "hypothesis.opened"
	RecordKindHypothesisStatusChanged RecordKind = "hypothesis.status_changed"
	RecordKindHypothesisRebased       RecordKind = "hypothesis.rebased"
	RecordKindHypothesisSuperseded    RecordKind = "hypothesis.superseded"
	RecordKindHypothesisResolved      RecordKind = "hypothesis.resolved"
	RecordKindRoutineCreated          RecordKind = "routine.created"
	RecordKindRoutineOccurrenceAdded  RecordKind = "routine.occurrence_added"
	RecordKindRoutineStatusChanged    RecordKind = "routine.status_changed"
	RecordKindMerged                  RecordKind = "chain.merged"
	RecordKindSplit                   RecordKind = "chain.split"
	RecordKindReassigned              RecordKind = "chain.reassigned"
	RecordKindReactivatedFromHistory  RecordKind = "chain.reactivated_from_history"
)

// Validate accepts only record kinds implemented by journal schema v1.
func (k RecordKind) Validate() error {
	switch k {
	case RecordKindGenesis, RecordKindChainAdded, RecordKindLifecycleTransitioned, RecordKindSnapshotCheckpointed, RecordKindObservationAdded, RecordKindContributionAdded, RecordKindHypothesisOpened, RecordKindHypothesisStatusChanged, RecordKindHypothesisRebased, RecordKindHypothesisSuperseded, RecordKindHypothesisResolved, RecordKindRoutineCreated, RecordKindRoutineOccurrenceAdded, RecordKindRoutineStatusChanged:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidRecordKind, k)
	}
}

// Record is one complete NDJSON journal line. PayloadSHA256 covers the exact
// Payload bytes; RecordHash covers all fields except RecordHash itself.
type Record struct {
	SchemaVersion int             `json:"schema_version"`
	Sequence      uint64          `json:"sequence"`
	Kind          RecordKind      `json:"kind"`
	RecordedAt    time.Time       `json:"recorded_at"`
	Actor         string          `json:"actor"`
	CorrelationID string          `json:"correlation_id"`
	PreviousHash  string          `json:"previous_hash"`
	Payload       json.RawMessage `json:"payload"`
	PayloadSHA256 string          `json:"payload_sha256"`
	RecordHash    string          `json:"record_hash"`
}

// Validate verifies the self-contained integrity of one record. Chain
// position is intentionally not checked here; ReadAll performs that check
// against the complete append-only sequence.
func (r Record) Validate() error {
	return validateRecordFields(r)
}

// GenesisInput describes explicit creation of a journal.
type GenesisInput struct {
	JournalID     string
	CreatedAt     time.Time
	Purpose       string
	RecordedAt    time.Time
	Actor         string
	CorrelationID string
}

// ChainAddedInput describes the first global appearance of a chain.
type ChainAddedInput struct {
	Chain         chains.Snapshot
	RecordedAt    time.Time
	Actor         string
	CorrelationID string
}

// LifecycleTransitionInput describes a compact delta from one local chain
// revision to the next.
type LifecycleTransitionInput struct {
	ChainID          chains.ChainID
	PreviousRevision uint64
	NewRevision      uint64
	From             chains.Status
	To               chains.Status
	Revision         chains.RevisionRecord
	RecordedAt       time.Time
	Actor            string
	CorrelationID    string
}

// ObservationAddedInput describes one compact observation delta. Mutation.At
// is carried by Revision.At and may differ explicitly from RecordedAt.
type ObservationAddedInput struct {
	ChainID          chains.ChainID
	PreviousRevision uint64
	NewRevision      uint64
	Observation      chains.ObservationRef
	Revision         chains.RevisionRecord
	RecordedAt       time.Time
	Actor            string
	CorrelationID    string
}

// ContributionAddedInput describes one compact contribution delta. The local
// revision remains the source of detailed provenance.
type ContributionAddedInput struct {
	ChainID                    chains.ChainID
	PreviousRevision           uint64
	NewRevision                uint64
	Contribution               chains.ConfidenceContribution
	PreviousConfidence         float64
	NewConfidence              float64
	PreviousSupportCount       uint64
	NewSupportCount            uint64
	PreviousContradictionCount uint64
	NewContradictionCount      uint64
	Revision                   chains.RevisionRecord
	RecordedAt                 time.Time
	Actor                      string
	CorrelationID              string
}

// HypothesisOpenedInput describes the first global appearance of one
// hypothesis set. The snapshot must still be at revision one and open.
type HypothesisOpenedInput struct {
	Hypothesis    hypotheses.Snapshot
	RecordedAt    time.Time
	Actor         string
	CorrelationID string
}

// HypothesisStatusChangedInput describes one local status delta.
type HypothesisStatusChangedInput struct {
	SetID            hypotheses.SetID
	PreviousRevision uint64
	NewRevision      uint64
	PreviousStatus   hypotheses.Status
	NewStatus        hypotheses.Status
	Revision         hypotheses.RevisionRecord
	RecordedAt       time.Time
	Actor            string
	CorrelationID    string
}

type HypothesisRebasedInput struct {
	SetID                     hypotheses.SetID
	PreviousRevision          uint64
	NewRevision               uint64
	PreviousAssessmentVersion uint64
	NewAssessmentVersion      uint64
	PreviousAssessmentID      string
	NewAssessmentID           string
	PreviousFingerprint       string
	NewFingerprint            string
	Assessment                hypotheses.AssessmentVersionSnapshot
	Revision                  hypotheses.RevisionRecord
	RecordedAt                time.Time
	Actor                     string
	CorrelationID             string
}

type HypothesisSupersededInput struct {
	PreviousSetID          hypotheses.SetID
	NewSetID               hypotheses.SetID
	PreviousRevision       uint64
	NewPreviousRevision    uint64
	PreviousStatus         hypotheses.Status
	NewStatus              hypotheses.Status
	PreviousSuccessorSetID hypotheses.SetID
	NewSuccessorSetID      hypotheses.SetID
	PreviousSetRevision    hypotheses.RevisionRecord
	NewHypothesis          hypotheses.Snapshot
	RecordedAt             time.Time
	Actor                  string
	CorrelationID          string
}

type HypothesisResolvedInput struct {
	SetID                 hypotheses.SetID
	PreviousRevision      uint64
	NewRevision           uint64
	PreviousStatus        hypotheses.Status
	NewStatus             hypotheses.Status
	AssessmentVersion     uint64
	AssessmentID          string
	AssessmentFingerprint string
	AlternativeID         string
	AlternativeKind       hypotheses.AlternativeKind
	Effect                hypotheses.ResolutionEffect
	EffectFingerprint     string
	Outcome               hypotheses.ResolutionOutcome
	HypothesisRevision    hypotheses.RevisionRecord
	ChainDelta            ResolutionChainDelta
	RecordedAt            time.Time
	Actor                 string
	CorrelationID         string
}

type RoutineCreatedInput struct {
	RoutineID           routines.RoutineID
	PreviousRevision    uint64
	NewRevision         uint64
	Snapshot            routines.Snapshot
	SnapshotFingerprint string
	RecordedAt          time.Time
	Actor               string
	CorrelationID       string
}

type RoutineOccurrenceAddedInput struct {
	RoutineID                 routines.RoutineID
	PreviousRevision          uint64
	NewRevision               uint64
	Occurrence                routines.Occurrence
	Revision                  routines.RevisionRecord
	Outcome                   routines.MutationOutcome
	ResultSnapshotFingerprint string
	RecordedAt                time.Time
	Actor                     string
	CorrelationID             string
}

type RoutineStatusChangedInput struct {
	RoutineID                 routines.RoutineID
	PreviousRevision          uint64
	NewRevision               uint64
	PreviousStatus            routines.Status
	NewStatus                 routines.Status
	Revision                  routines.RevisionRecord
	ResultSnapshotFingerprint string
	RecordedAt                time.Time
	Actor                     string
	CorrelationID             string
}

// SnapshotCheckpointInput records metadata for an already-created snapshot.
// It never invokes persistence.FileStore.
type SnapshotCheckpointInput struct {
	SnapshotSchemaVersion int
	SnapshotCreatedAt     time.Time
	SnapshotChainCount    int
	SnapshotPayloadSHA256 string
	SnapshotSizeBytes     int64
	JournalSequence       uint64
	JournalHeadHash       string
	RecordedAt            time.Time
	Actor                 string
	CorrelationID         string
}

// JournalSnapshot is a defensive read model of a completely validated file.
type JournalSnapshot struct {
	SchemaVersion int
	JournalID     string

	RecordCount  uint64
	HeadSequence uint64
	HeadHash     string

	Records []Record
}

// JournalHead is a bounded integrity view used by local transaction checks.
// It deliberately does not claim that older bytes have been fully validated;
// ReadAll remains the authoritative complete validation path.
type JournalHead struct {
	Sequence uint64
	Hash     string
	Size     int64
}

// Clone returns a deep defensive copy, including all raw payload bytes.
func (s JournalSnapshot) Clone() JournalSnapshot {
	clone := s
	if s.Records != nil {
		clone.Records = make([]Record, len(s.Records))
		for i, record := range s.Records {
			clone.Records[i] = cloneRecord(record)
		}
	}
	return clone
}

func cloneRecord(record Record) Record {
	clone := record
	clone.Payload = append(json.RawMessage(nil), record.Payload...)
	return clone
}
