package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/hypotheses"
)

// FromJournal reconstructs both the set contents and their local histories
// from a complete, cryptographically validated journal snapshot. No partial
// registry is ever returned.
func FromJournal(ctx context.Context, source journal.JournalSnapshot) (*hypotheses.Registry, ReplayMetadata, error) {
	if err := contextError(ctx); err != nil {
		return nil, ReplayMetadata{}, err
	}
	if err := validateSource(ctx, source); err != nil {
		return nil, ReplayMetadata{}, err
	}
	metadata := ReplayMetadata{JournalID: source.JournalID, RecordsExamined: uint64(len(source.Records)), FinalHeadSequence: source.HeadSequence, FinalHeadHash: source.HeadHash}
	target := hypotheses.NewRegistry()
	for _, record := range source.Records {
		if err := contextError(ctx); err != nil {
			return nil, ReplayMetadata{}, err
		}
		var setID hypotheses.SetID
		var err error
		switch record.Kind {
		case journal.RecordKindHypothesisOpened:
			var payload journal.HypothesisOpenedPayload
			payload, err = decode[journal.HypothesisOpenedPayload](record.Payload)
			setID = payload.Hypothesis.ID
			if err == nil {
				err = applyOpened(target, record, payload)
			}
			if err == nil {
				metadata.SetsOpened++
				metadata.RecordsApplied++
			}
		case journal.RecordKindHypothesisStatusChanged:
			var payload journal.HypothesisStatusChangedPayload
			payload, err = decode[journal.HypothesisStatusChangedPayload](record.Payload)
			setID = payload.SetID
			if err == nil {
				err = applyStatusChanged(target, record, payload)
			}
			if err == nil {
				metadata.StatusChangesApplied++
				metadata.RecordsApplied++
			}
		case journal.RecordKindHypothesisRebased:
			var payload journal.HypothesisRebasedPayload
			payload, err = decode[journal.HypothesisRebasedPayload](record.Payload)
			setID = payload.SetID
			if err == nil {
				err = applyRebased(target, record, payload)
			}
			if err == nil {
				metadata.RebasesApplied++
				metadata.RecordsApplied++
			}
		case journal.RecordKindHypothesisSuperseded:
			var payload journal.HypothesisSupersededPayload
			payload, err = decode[journal.HypothesisSupersededPayload](record.Payload)
			setID = payload.PreviousSetID
			if err == nil {
				err = applySuperseded(target, record, payload)
			}
			if err == nil {
				metadata.SupersessionsApplied++
				metadata.RecordsApplied++
			}
		case journal.RecordKindHypothesisResolved:
			var payload journal.HypothesisResolvedPayload
			payload, err = decode[journal.HypothesisResolvedPayload](record.Payload)
			setID = payload.SetID
			metadata.ResolutionsExamined++
			if err == nil {
				err = applyResolved(target, record, payload)
			}
			if err == nil {
				metadata.ResolutionsApplied++
				metadata.RecordsApplied++
			}
		case journal.RecordKindGenesis, journal.RecordKindChainAdded, journal.RecordKindLifecycleTransitioned,
			journal.RecordKindObservationAdded, journal.RecordKindContributionAdded, journal.RecordKindSnapshotCheckpointed,
			journal.RecordKindRoutineCreated, journal.RecordKindRoutineOccurrenceAdded, journal.RecordKindRoutineStatusChanged:
			metadata.RecordsSkipped++
		default:
			err = ErrUnsupportedRecord
		}
		if err != nil {
			return nil, ReplayMetadata{}, ReplayError{Sequence: record.Sequence, Kind: record.Kind, SetID: setID, Err: errors.Join(ErrHypothesisReplayFailed, err)}
		}
	}
	metadata.FinalSetCount = target.Count()
	for _, snapshot := range target.List() {
		if err := contextError(ctx); err != nil {
			return nil, ReplayMetadata{}, err
		}
		set, err := hypotheses.Restore(snapshot)
		if err != nil || set == nil {
			if err == nil {
				err = ErrFinalRegistryInvalid
			}
			return nil, ReplayMetadata{}, fmt.Errorf("%w: set=%s: %v", ErrFinalRegistryInvalid, snapshot.ID, err)
		}
	}
	return target, metadata, nil
}

func applyResolved(target *hypotheses.Registry, record journal.Record, payload journal.HypothesisResolvedPayload) error {
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.PreviousStatus != hypotheses.StatusOpen && payload.PreviousStatus != hypotheses.StatusUnderReview || payload.NewStatus != hypotheses.StatusResolved || payload.HypothesisRevision.SetID != payload.SetID || payload.HypothesisRevision.Operation != hypotheses.OperationHypothesisResolved || payload.HypothesisRevision.PreviousRevision != payload.PreviousRevision || payload.HypothesisRevision.NewRevision != payload.NewRevision || payload.HypothesisRevision.PreviousStatus != payload.PreviousStatus || payload.HypothesisRevision.NewStatus != payload.NewStatus || payload.HypothesisRevision.Actor != record.Actor || payload.HypothesisRevision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.HypothesisRevision.At) {
		return ErrRevisionRecordMismatch
	}
	fingerprint, err := payload.Effect.Fingerprint()
	if err != nil || fingerprint != payload.EffectFingerprint {
		return hypotheses.ErrResolutionEffectMismatch
	}
	if err := payload.Outcome.Validate(); err != nil || payload.Outcome.Kind != payload.Effect.Kind {
		return hypotheses.ErrResolutionOutcomeMismatch
	}
	if err := validateResolutionChainDelta(payload); err != nil {
		return err
	}
	before, err := target.Get(payload.SetID)
	if err != nil {
		return err
	}
	if before.Revision != payload.PreviousRevision || before.Status != payload.PreviousStatus {
		return ErrRevisionMismatch
	}
	if len(before.Assessments) == 0 || before.CurrentAssessmentVersion != payload.AssessmentVersion {
		return ErrRevisionMismatch
	}
	assessment := before.Assessments[len(before.Assessments)-1]
	if assessment.ID != payload.AssessmentID || assessment.Fingerprint != payload.AssessmentFingerprint || assessment.ResolutionSchemaVersion != hypotheses.ResolutionSchemaV1 {
		return ErrRevisionMismatch
	}
	mutation := chains.MutationContext{At: payload.HypothesisRevision.At, Actor: payload.HypothesisRevision.Actor, Reason: payload.HypothesisRevision.Reason, CorrelationID: payload.HypothesisRevision.CorrelationID}
	command := hypotheses.ResolveCommand{SetID: payload.SetID, SourceRevision: payload.PreviousRevision, AssessmentVersion: payload.AssessmentVersion, AssessmentID: payload.AssessmentID, AssessmentFingerprint: payload.AssessmentFingerprint, AlternativeID: payload.AlternativeID, AlternativeKind: payload.AlternativeKind, Effect: payload.Effect.Clone(), Mutation: mutation}
	after, err := target.Resolve(command, payload.Outcome)
	if err != nil {
		return err
	}
	if after.Revision != payload.NewRevision || after.Status != hypotheses.StatusResolved || after.Resolution == nil || after.Resolution.AlternativeID != payload.AlternativeID || after.Resolution.EffectFingerprint != payload.EffectFingerprint || !reflect.DeepEqual(after.Resolution.Outcome, payload.Outcome) || !reflect.DeepEqual(after.History[len(after.History)-1], payload.HypothesisRevision) {
		return ErrRevisionRecordMismatch
	}
	if _, err := hypotheses.Restore(after); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func validateResolutionChainDelta(payload journal.HypothesisResolvedPayload) error {
	if payload.ChainDelta.Kind != payload.Effect.Kind {
		return ErrRevisionRecordMismatch
	}
	count := 0
	if payload.ChainDelta.ObservationAdded != nil {
		count++
	}
	if payload.ChainDelta.ChainAdded != nil {
		count++
	}
	if payload.ChainDelta.ContributionAdded != nil {
		count++
	}
	if payload.ChainDelta.NoChainEffect != nil {
		count++
	}
	if count != 1 {
		return ErrRevisionRecordMismatch
	}
	switch payload.Effect.Kind {
	case hypotheses.ResolutionEffectAttachObservation:
		p, e, o := payload.ChainDelta.ObservationAdded, payload.Effect.AttachObservation, payload.Outcome.AttachObservation
		if p == nil || e == nil || o == nil || p.ChainID != e.ChainID || p.PreviousRevision != e.SourceRevision || p.Observation != e.Observation || p.NewRevision != o.NewRevision || p.Revision.NewRevision != o.NewRevision {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectCreateCandidate:
		p, e, o := payload.ChainDelta.ChainAdded, payload.Effect.CreateCandidate, payload.Outcome.CreateCandidate
		if p == nil || e == nil || o == nil || p.Chain.ID != e.ChainID || p.Chain.Status != e.InitialStatus || p.Chain.CurrentConfidence != e.InitialConfidence || p.Chain.Revision != o.NewRevision || len(p.Chain.Observations) != 1 || p.Chain.Observations[0] != e.Observation || len(p.Chain.Contributions) != 0 {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectAddContribution:
		p, e, o := payload.ChainDelta.ContributionAdded, payload.Effect.AddContribution, payload.Outcome.AddContribution
		if p == nil || e == nil || o == nil {
			return ErrRevisionRecordMismatch
		}
		t := e.ContributionTemplate
		if p.ChainID != e.ChainID || p.PreviousRevision != e.SourceRevision || p.Contribution.ID != t.ID || p.Contribution.Source != t.Source || p.Contribution.Kind != t.Kind || p.Contribution.Value != t.Value || p.Contribution.Reason != t.ReasonCode || p.Contribution.CreatedAt != payload.HypothesisRevision.At || !equalResolutionStrings(p.Contribution.ObservationIDs, t.ObservationIDs) || p.NewRevision != o.NewRevision || p.PreviousConfidence != o.PreviousConfidence || p.NewConfidence != o.NewConfidence {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectNoChain:
		if payload.ChainDelta.NoChainEffect == nil || payload.Outcome.NoChainEffect == nil || payload.Effect.NoChainEffect == nil || payload.ChainDelta.NoChainEffect.ReasonCode != payload.Effect.NoChainEffect.ReasonCode || payload.ChainDelta.NoChainEffect.ReasonCode != payload.Outcome.NoChainEffect.ReasonCode {
			return ErrRevisionRecordMismatch
		}
	}
	return nil
}

func equalResolutionStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", ErrInvalidReplayInput)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReplayInput, err)
	}
	return nil
}

func validateSource(ctx context.Context, source journal.JournalSnapshot) error {
	if source.SchemaVersion != journal.CurrentSchemaVersion || source.RecordCount == 0 || source.RecordCount != uint64(len(source.Records)) || source.JournalID == "" || source.HeadSequence == 0 || source.HeadHash == "" {
		return fmt.Errorf("%w: journal metadata is incomplete", ErrInvalidReplayInput)
	}
	var previousHash string
	for index, record := range source.Records {
		if err := contextError(ctx); err != nil {
			return err
		}
		if record.Sequence != uint64(index+1) || record.RecordHash == "" || record.PreviousHash == "" {
			return fmt.Errorf("%w: journal sequence is inconsistent", ErrInvalidReplayInput)
		}
		if index == 0 {
			if record.Kind != journal.RecordKindGenesis || record.PreviousHash != journal.GenesisPreviousHash {
				return fmt.Errorf("%w: genesis is invalid", ErrInvalidReplayInput)
			}
		} else if record.PreviousHash != previousHash {
			return fmt.Errorf("%w: journal hash chain is inconsistent", ErrInvalidReplayInput)
		}
		if err := record.Validate(); err != nil {
			return fmt.Errorf("%w: sequence=%d: %v", ErrInvalidReplayInput, record.Sequence, err)
		}
		previousHash = record.RecordHash
	}
	if source.Records[len(source.Records)-1].RecordHash != source.HeadHash {
		return fmt.Errorf("%w: journal head is inconsistent", ErrInvalidReplayInput)
	}
	return nil
}

func decode[T any](data []byte) (T, error) {
	var value T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("%w: payload: %v", ErrInvalidReplayInput, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return value, fmt.Errorf("%w: multiple JSON values", ErrInvalidReplayInput)
		}
		return value, fmt.Errorf("%w: trailing payload: %v", ErrInvalidReplayInput, err)
	}
	return value, nil
}

func applyOpened(target *hypotheses.Registry, record journal.Record, payload journal.HypothesisOpenedPayload) error {
	if payload.Hypothesis.Status != hypotheses.StatusOpen || payload.Hypothesis.Revision != 1 || len(payload.Hypothesis.History) != 1 {
		return ErrRevisionMismatch
	}
	opening := payload.Hypothesis.History[0]
	if opening.Operation != hypotheses.OperationHypothesisOpened || opening.SetID != payload.Hypothesis.ID || opening.PreviousRevision != 0 || opening.NewRevision != 1 || opening.Actor != record.Actor || opening.CorrelationID != record.CorrelationID || record.RecordedAt.Before(opening.At) {
		return ErrRevisionRecordMismatch
	}
	set, err := hypotheses.Restore(payload.Hypothesis)
	if err != nil {
		return err
	}
	if _, err := target.Get(set.ID()); err == nil {
		return hypotheses.ErrHypothesisAlreadyExists
	} else if !errors.Is(err, hypotheses.ErrHypothesisNotFound) {
		return err
	}
	return target.Add(set)
}

func applyStatusChanged(target *hypotheses.Registry, record journal.Record, payload journal.HypothesisStatusChangedPayload) error {
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.Revision.SetID != payload.SetID || payload.Revision.Operation != hypotheses.OperationHypothesisStatusChanged || payload.Revision.PreviousRevision != payload.PreviousRevision || payload.Revision.NewRevision != payload.NewRevision || payload.Revision.PreviousStatus != payload.PreviousStatus || payload.Revision.NewStatus != payload.NewStatus || payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
		return ErrRevisionRecordMismatch
	}
	before, err := target.Get(payload.SetID)
	if err != nil {
		return err
	}
	if before.Revision != payload.PreviousRevision {
		return ErrRevisionMismatch
	}
	if before.Status != payload.PreviousStatus {
		return ErrStatusMismatch
	}
	mutation := chains.MutationContext{At: payload.Revision.At, Actor: payload.Revision.Actor, Reason: payload.Revision.Reason, CorrelationID: payload.Revision.CorrelationID}
	after, err := target.SetStatus(payload.SetID, payload.PreviousRevision, payload.NewStatus, mutation)
	if err != nil {
		return err
	}
	if after.Revision != payload.NewRevision || after.Status != payload.NewStatus || !reflect.DeepEqual(before.Alternatives, after.Alternatives) || !reflect.DeepEqual(before.Subject, after.Subject) || !reflect.DeepEqual(before.Provenance, after.Provenance) || !reflect.DeepEqual(after.History[len(after.History)-1], payload.Revision) {
		return ErrRevisionRecordMismatch
	}
	if _, err := hypotheses.Restore(after); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func applyRebased(target *hypotheses.Registry, record journal.Record, payload journal.HypothesisRebasedPayload) error {
	before, err := target.Get(payload.SetID)
	if err != nil {
		return err
	}
	if before.Revision != payload.PreviousRevision {
		return ErrRevisionMismatch
	}
	if len(before.Assessments) == 0 {
		return ErrRevisionMismatch
	}
	current := before.Assessments[len(before.Assessments)-1]
	if current.Version != payload.PreviousAssessmentVersion || current.ID != payload.PreviousAssessmentID || current.Fingerprint != payload.PreviousFingerprint {
		return ErrRevisionMismatch
	}
	if payload.Assessment.Version != payload.NewAssessmentVersion || payload.Assessment.ID != payload.NewAssessmentID || payload.Assessment.Fingerprint != payload.NewFingerprint {
		return ErrRevisionRecordMismatch
	}
	if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.Revision.At) {
		return ErrRevisionRecordMismatch
	}
	mutation := chains.MutationContext{At: payload.Revision.At, Actor: payload.Revision.Actor, Reason: payload.Revision.Reason, CorrelationID: payload.Revision.CorrelationID}
	command := hypotheses.RebaseCommand{SetID: payload.SetID, SourceRevision: payload.PreviousRevision, PreviousAssessmentVersion: payload.PreviousAssessmentVersion, PreviousAssessmentID: payload.PreviousAssessmentID, Assessment: payload.Assessment, Family: before.Family, Subject: before.Subject, Mutation: mutation}
	after, err := target.Rebase(command)
	if err != nil {
		return err
	}
	if after.Revision != payload.NewRevision || len(after.Assessments) == 0 || !reflect.DeepEqual(after.Assessments[len(after.Assessments)-1], payload.Assessment) || !reflect.DeepEqual(after.History[len(after.History)-1], payload.Revision) || after.Status != before.Status {
		return ErrRevisionRecordMismatch
	}
	if _, err := hypotheses.Restore(after); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func applySuperseded(target *hypotheses.Registry, record journal.Record, payload journal.HypothesisSupersededPayload) error {
	if payload.PreviousSetID == "" || payload.NewSetID == "" || payload.PreviousSetID == payload.NewSetID || payload.PreviousRevision == 0 || payload.NewPreviousRevision != 1 || payload.PreviousStatus == hypotheses.StatusSuperseded || payload.NewStatus != hypotheses.StatusSuperseded || payload.PreviousSuccessorSetID != "" || payload.NewSuccessorSetID != payload.NewSetID {
		return ErrRevisionMismatch
	}
	if payload.PreviousSetRevision.SetID != payload.PreviousSetID || payload.PreviousSetRevision.Operation != hypotheses.OperationHypothesisSuperseded || payload.PreviousSetRevision.PreviousRevision != payload.PreviousRevision || payload.PreviousSetRevision.NewRevision != payload.PreviousRevision+1 || payload.PreviousSetRevision.PreviousStatus != payload.PreviousStatus || payload.PreviousSetRevision.NewStatus != hypotheses.StatusSuperseded || payload.PreviousSetRevision.PreviousSuccessorSetID != "" || payload.PreviousSetRevision.NewSuccessorSetID != payload.NewSetID {
		return ErrRevisionRecordMismatch
	}
	if payload.NewHypothesis.ID != payload.NewSetID || payload.NewHypothesis.Family != hypotheses.FamilyEvidence || payload.NewHypothesis.Status != hypotheses.StatusOpen || payload.NewHypothesis.Revision != 1 || len(payload.NewHypothesis.History) != 1 || payload.NewHypothesis.History[0].Operation != hypotheses.OperationHypothesisOpened || payload.NewHypothesis.History[0].PreviousRevision != 0 || payload.NewHypothesis.History[0].NewRevision != 1 || payload.NewHypothesis.Lineage.PredecessorSetID != payload.PreviousSetID || payload.NewHypothesis.Lineage.SuccessorSetID != "" || payload.NewHypothesis.Subject.ChainID == "" || payload.NewHypothesis.Subject.ObservationID == "" || payload.NewHypothesis.Subject.EvidenceFingerprint == "" {
		return ErrRevisionMismatch
	}
	if _, err := hypotheses.Restore(payload.NewHypothesis); err != nil {
		return fmt.Errorf("%w: successor: %v", ErrFinalRegistryInvalid, err)
	}
	before, err := target.Get(payload.PreviousSetID)
	if err != nil {
		return err
	}
	if before.Family != hypotheses.FamilyEvidence || before.Revision != payload.PreviousRevision || before.Status != payload.PreviousStatus || before.Lineage.SuccessorSetID != "" || before.Subject.ChainID != payload.NewHypothesis.Subject.ChainID || before.Subject.ObservationID != payload.NewHypothesis.Subject.ObservationID || before.Subject.EvidenceFingerprint == payload.NewHypothesis.Subject.EvidenceFingerprint || payload.NewHypothesis.Lineage.RootSetID != before.Lineage.RootSetID || payload.NewHypothesis.Lineage.Generation != before.Lineage.Generation+1 {
		return ErrRevisionMismatch
	}
	if len(before.Assessments) == 0 {
		return ErrRevisionMismatch
	}
	current := before.Assessments[len(before.Assessments)-1]
	if payload.PreviousSetRevision.PreviousAssessmentVersion != 0 && (payload.PreviousSetRevision.PreviousAssessmentVersion != current.Version || payload.PreviousSetRevision.PreviousAssessmentID != current.ID) {
		return ErrRevisionMismatch
	}
	if _, err := target.Get(payload.NewSetID); err == nil {
		return hypotheses.ErrHypothesisAlreadyExists
	} else if !errors.Is(err, hypotheses.ErrHypothesisNotFound) {
		return err
	}
	if payload.PreviousSetRevision.Actor != record.Actor || payload.PreviousSetRevision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.PreviousSetRevision.At) {
		return ErrRevisionRecordMismatch
	}
	mutation := chains.MutationContext{At: payload.PreviousSetRevision.At, Actor: payload.PreviousSetRevision.Actor, Reason: payload.PreviousSetRevision.Reason, CorrelationID: payload.PreviousSetRevision.CorrelationID}
	command := hypotheses.SupersedeCommand{PreviousSetID: payload.PreviousSetID, NewSetID: payload.NewSetID, PreviousSourceRevision: payload.PreviousRevision, PreviousAssessmentVersion: current.Version, PreviousAssessmentID: current.ID, NewSet: payload.NewHypothesis, Mutation: mutation}
	result, err := target.Supersede(command)
	if err != nil {
		return err
	}
	if result.PreviousAfter.Revision != payload.PreviousRevision+1 || result.PreviousAfter.Status != hypotheses.StatusSuperseded || result.PreviousAfter.Lineage.SuccessorSetID != payload.NewSetID || !reflect.DeepEqual(result.PreviousAfter.History[len(result.PreviousAfter.History)-1], payload.PreviousSetRevision) || !reflect.DeepEqual(result.NewAfter, payload.NewHypothesis) {
		return ErrRevisionRecordMismatch
	}
	return nil
}
