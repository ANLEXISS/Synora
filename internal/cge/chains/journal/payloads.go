package journal

import (
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/hypotheses"
	"synora/internal/cge/routines"
)

// GenesisPayload is the minimal stable journal creation payload.
type GenesisPayload struct {
	JournalID string    `json:"journal_id"`
	CreatedAt time.Time `json:"created_at"`
	Purpose   string    `json:"purpose"`
}

// ChainAddedPayload contains the complete first snapshot of a chain.
type ChainAddedPayload struct {
	Chain chains.Snapshot `json:"chain"`
}

// LifecycleTransitionPayload is a compact global delta. The local revision
// remains the source of detailed provenance for that chain.
type LifecycleTransitionPayload struct {
	ChainID          chains.ChainID        `json:"chain_id"`
	PreviousRevision uint64                `json:"previous_revision"`
	NewRevision      uint64                `json:"new_revision"`
	From             chains.Status         `json:"from"`
	To               chains.Status         `json:"to"`
	Revision         chains.RevisionRecord `json:"revision"`
}

// ObservationAddedPayload is a compact domain delta. It carries the detached
// observation reference and the exact local audit record, never a full chain.
type ObservationAddedPayload struct {
	ChainID          chains.ChainID        `json:"chain_id"`
	PreviousRevision uint64                `json:"previous_revision"`
	NewRevision      uint64                `json:"new_revision"`
	Observation      chains.ObservationRef `json:"observation"`
	Revision         chains.RevisionRecord `json:"revision"`
}

// ContributionAddedPayload is a compact confidence delta. It contains no
// complete chain snapshot and preserves the exact before/after projections
// produced by the domain operation.
type ContributionAddedPayload struct {
	ChainID                    chains.ChainID                `json:"chain_id"`
	PreviousRevision           uint64                        `json:"previous_revision"`
	NewRevision                uint64                        `json:"new_revision"`
	Contribution               chains.ConfidenceContribution `json:"contribution"`
	PreviousConfidence         float64                       `json:"previous_confidence"`
	NewConfidence              float64                       `json:"new_confidence"`
	PreviousSupportCount       uint64                        `json:"previous_support_count"`
	NewSupportCount            uint64                        `json:"new_support_count"`
	PreviousContradictionCount uint64                        `json:"previous_contradiction_count"`
	NewContradictionCount      uint64                        `json:"new_contradiction_count"`
	Revision                   chains.RevisionRecord         `json:"revision"`
}

// HypothesisOpenedPayload contains the complete initial local snapshot.
type HypothesisOpenedPayload struct {
	Hypothesis hypotheses.Snapshot `json:"hypothesis"`
}

// HypothesisStatusChangedPayload contains only the local status delta.
type HypothesisStatusChangedPayload struct {
	SetID            hypotheses.SetID          `json:"set_id"`
	PreviousRevision uint64                    `json:"previous_revision"`
	NewRevision      uint64                    `json:"new_revision"`
	PreviousStatus   hypotheses.Status         `json:"previous_status"`
	NewStatus        hypotheses.Status         `json:"new_status"`
	Revision         hypotheses.RevisionRecord `json:"revision"`
}

// HypothesisRebasedPayload contains only the newly appended assessment
// version and the exact local revision that installed it.
type HypothesisRebasedPayload struct {
	SetID                     hypotheses.SetID                     `json:"set_id"`
	PreviousRevision          uint64                               `json:"previous_revision"`
	NewRevision               uint64                               `json:"new_revision"`
	PreviousAssessmentVersion uint64                               `json:"previous_assessment_version"`
	NewAssessmentVersion      uint64                               `json:"new_assessment_version"`
	PreviousAssessmentID      string                               `json:"previous_assessment_id"`
	NewAssessmentID           string                               `json:"new_assessment_id"`
	PreviousFingerprint       string                               `json:"previous_fingerprint"`
	NewFingerprint            string                               `json:"new_fingerprint"`
	Assessment                hypotheses.AssessmentVersionSnapshot `json:"assessment"`
	Revision                  hypotheses.RevisionRecord            `json:"revision"`
}

type HypothesisSupersededPayload struct {
	PreviousSetID          hypotheses.SetID          `json:"previous_set_id"`
	NewSetID               hypotheses.SetID          `json:"new_set_id"`
	PreviousRevision       uint64                    `json:"previous_revision"`
	NewPreviousRevision    uint64                    `json:"new_previous_revision"`
	PreviousStatus         hypotheses.Status         `json:"previous_status"`
	NewStatus              hypotheses.Status         `json:"new_status"`
	PreviousSuccessorSetID hypotheses.SetID          `json:"previous_successor_set_id"`
	NewSuccessorSetID      hypotheses.SetID          `json:"new_successor_set_id"`
	PreviousSetRevision    hypotheses.RevisionRecord `json:"previous_set_revision"`
	NewHypothesis          hypotheses.Snapshot       `json:"new_hypothesis"`
}

// ResolutionChainDelta is the single typed union carried by
// hypothesis.resolved. It intentionally contains no independent record.
type ResolutionChainDelta struct {
	Kind              hypotheses.ResolutionEffectKind `json:"kind"`
	ObservationAdded  *ObservationAddedPayload        `json:"observation_added,omitempty"`
	ChainAdded        *ChainAddedPayload              `json:"chain_added,omitempty"`
	ContributionAdded *ContributionAddedPayload       `json:"contribution_added,omitempty"`
	NoChainEffect     *ResolutionNoChainEffectPayload `json:"no_chain_effect,omitempty"`
}

type ResolutionNoChainEffectPayload struct {
	ReasonCode string `json:"reason_code"`
}

type HypothesisResolvedPayload struct {
	SetID                 hypotheses.SetID             `json:"set_id"`
	PreviousRevision      uint64                       `json:"previous_revision"`
	NewRevision           uint64                       `json:"new_revision"`
	PreviousStatus        hypotheses.Status            `json:"previous_status"`
	NewStatus             hypotheses.Status            `json:"new_status"`
	AssessmentVersion     uint64                       `json:"assessment_version"`
	AssessmentID          string                       `json:"assessment_id"`
	AssessmentFingerprint string                       `json:"assessment_fingerprint"`
	AlternativeID         string                       `json:"alternative_id"`
	AlternativeKind       hypotheses.AlternativeKind   `json:"alternative_kind"`
	Effect                hypotheses.ResolutionEffect  `json:"effect"`
	EffectFingerprint     string                       `json:"effect_fingerprint"`
	Outcome               hypotheses.ResolutionOutcome `json:"outcome"`
	HypothesisRevision    hypotheses.RevisionRecord    `json:"hypothesis_revision"`
	ChainDelta            ResolutionChainDelta         `json:"chain_delta"`
}

type RoutineCreatedPayload struct {
	RoutineID           routines.RoutineID `json:"routine_id"`
	PreviousRevision    uint64             `json:"previous_revision"`
	NewRevision         uint64             `json:"new_revision"`
	Snapshot            routines.Snapshot  `json:"snapshot"`
	SnapshotFingerprint string             `json:"snapshot_fingerprint"`
}

type RoutineOccurrenceAddedPayload struct {
	RoutineID                 routines.RoutineID       `json:"routine_id"`
	PreviousRevision          uint64                   `json:"previous_revision"`
	NewRevision               uint64                   `json:"new_revision"`
	Occurrence                routines.Occurrence      `json:"occurrence"`
	Revision                  routines.RevisionRecord  `json:"revision"`
	Outcome                   routines.MutationOutcome `json:"outcome"`
	ResultSnapshotFingerprint string                   `json:"result_snapshot_fingerprint"`
}

type RoutineStatusChangedPayload struct {
	RoutineID                 routines.RoutineID      `json:"routine_id"`
	PreviousRevision          uint64                  `json:"previous_revision"`
	NewRevision               uint64                  `json:"new_revision"`
	PreviousStatus            routines.Status         `json:"previous_status"`
	NewStatus                 routines.Status         `json:"new_status"`
	Revision                  routines.RevisionRecord `json:"revision"`
	ResultSnapshotFingerprint string                  `json:"result_snapshot_fingerprint"`
}

// SnapshotCheckpointPayload identifies a snapshot and the journal head that
// existed immediately before this checkpoint record was appended.
type SnapshotCheckpointPayload struct {
	SnapshotSchemaVersion int       `json:"snapshot_schema_version"`
	SnapshotCreatedAt     time.Time `json:"snapshot_created_at"`
	SnapshotChainCount    int       `json:"snapshot_chain_count"`
	SnapshotPayloadSHA256 string    `json:"snapshot_payload_sha256"`
	SnapshotSizeBytes     int64     `json:"snapshot_size_bytes"`
	JournalSequence       uint64    `json:"journal_sequence"`
	JournalHeadHash       string    `json:"journal_head_hash"`
}
