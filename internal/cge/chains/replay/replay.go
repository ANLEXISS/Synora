package replay

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/hypotheses"
)

// FromJournal reconstructs a new registry from a completely validated
// JournalSnapshot. The caller normally obtains that snapshot from
// FileJournal.ReadAll; replay adds the inter-entry domain checks that a file
// hash chain cannot express.
func FromJournal(ctx context.Context, source journal.JournalSnapshot) (*registry.Registry, ReplayMetadata, error) {
	if err := contextError(ctx); err != nil {
		return nil, ReplayMetadata{}, err
	}
	if err := validateJournalSnapshot(ctx, source); err != nil {
		return nil, ReplayMetadata{}, err
	}

	state := newReplayState()
	metadata := ReplayMetadata{
		Mode:              ReplayModeJournalOnly,
		JournalID:         source.JournalID,
		RecordsExamined:   uint64(len(source.Records)),
		FinalHeadSequence: source.HeadSequence,
		FinalHeadHash:     source.HeadHash,
	}
	metadata.CheckpointsSkipped = countCheckpoints(source)
	if err := replayRecords(ctx, state, source.Records, 1, &metadata); err != nil {
		return nil, ReplayMetadata{}, err
	}
	target, err := state.registry(ctx)
	if err != nil {
		return nil, ReplayMetadata{}, err
	}
	if err := finalize(ctx, target, &metadata); err != nil {
		return nil, ReplayMetadata{}, err
	}
	return target, metadata, nil
}

// FromSnapshotAndJournal reconstructs a new registry from a defensive copy
// of snapshotRegistry and only the journal records after the most recent
// checkpoint that exactly identifies snapshotMetadata.
//
// The checkpoint convention is: the snapshot contains registry state through
// journal sequence N; snapshot.checkpointed is sequence N+1 and changes no
// registry state; replay starts at sequence N+2.
func FromSnapshotAndJournal(
	ctx context.Context,
	snapshotRegistry *registry.Registry,
	snapshotMetadata persistence.SnapshotMetadata,
	source journal.JournalSnapshot,
) (*registry.Registry, ReplayMetadata, error) {
	return fromSnapshotAndJournal(ctx, snapshotRegistry, snapshotMetadata, source, func(ctx context.Context, source journal.JournalSnapshot, metadata persistence.SnapshotMetadata) (checkpointMatch, error) {
		return selectCheckpoint(ctx, source, metadata)
	})
}

// FromSnapshotAndJournalAtCheckpoint reconstructs a registry using the exact
// checkpoint named by its record sequence and hash. It is used by generation
// manifests, where selecting a later checkpoint with identical snapshot
// metadata would skip valid deltas.
func FromSnapshotAndJournalAtCheckpoint(
	ctx context.Context,
	snapshotRegistry *registry.Registry,
	snapshotMetadata persistence.SnapshotMetadata,
	source journal.JournalSnapshot,
	checkpointSequence uint64,
	checkpointHash string,
) (*registry.Registry, ReplayMetadata, error) {
	return fromSnapshotAndJournal(ctx, snapshotRegistry, snapshotMetadata, source, func(ctx context.Context, source journal.JournalSnapshot, metadata persistence.SnapshotMetadata) (checkpointMatch, error) {
		return selectCheckpointAt(ctx, source, metadata, checkpointSequence, checkpointHash)
	})
}

func fromSnapshotAndJournal(
	ctx context.Context,
	snapshotRegistry *registry.Registry,
	snapshotMetadata persistence.SnapshotMetadata,
	source journal.JournalSnapshot,
	chooseCheckpoint func(context.Context, journal.JournalSnapshot, persistence.SnapshotMetadata) (checkpointMatch, error),
) (*registry.Registry, ReplayMetadata, error) {
	if err := contextError(ctx); err != nil {
		return nil, ReplayMetadata{}, err
	}
	if snapshotRegistry == nil {
		return nil, ReplayMetadata{}, fmt.Errorf("%w: snapshot registry is nil", ErrInvalidReplayInput)
	}
	if err := validateJournalSnapshot(ctx, source); err != nil {
		return nil, ReplayMetadata{}, err
	}
	if err := validateSnapshotMetadata(snapshotMetadata); err != nil {
		return nil, ReplayMetadata{}, err
	}

	state, err := cloneRegistry(ctx, snapshotRegistry, snapshotMetadata.ChainCount)
	if err != nil {
		return nil, ReplayMetadata{}, err
	}
	checkpoint, err := chooseCheckpoint(ctx, source, snapshotMetadata)
	if err != nil {
		return nil, ReplayMetadata{}, err
	}

	metadata := ReplayMetadata{
		Mode:                  ReplayModeSnapshotAndJournal,
		JournalID:             source.JournalID,
		SnapshotUsed:          true,
		SnapshotCreatedAt:     snapshotMetadata.CreatedAt,
		SnapshotPayloadSHA256: snapshotMetadata.PayloadSHA256,
		SnapshotChainCount:    snapshotMetadata.ChainCount,
		CheckpointSequence:    checkpoint.record.Sequence,
		CheckpointHash:        checkpoint.record.RecordHash,
		RecordsExamined:       uint64(len(source.Records)),
		FinalHeadSequence:     source.HeadSequence,
		FinalHeadHash:         source.HeadHash,
		CheckpointsSkipped:    countCheckpoints(source),
	}
	start := int(checkpoint.record.Sequence)
	if start > len(source.Records) {
		return nil, ReplayMetadata{}, ErrSnapshotAheadOfJournal
	}
	if err := replayRecords(ctx, state, source.Records, start, &metadata); err != nil {
		return nil, ReplayMetadata{}, err
	}
	target, err := state.registry(ctx)
	if err != nil {
		return nil, ReplayMetadata{}, err
	}
	if err := finalize(ctx, target, &metadata); err != nil {
		return nil, ReplayMetadata{}, err
	}
	return target, metadata, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	return nil
}

func validateJournalSnapshot(ctx context.Context, source journal.JournalSnapshot) error {
	if source.SchemaVersion != journal.CurrentSchemaVersion || source.RecordCount == 0 || len(source.Records) == 0 || source.JournalID == "" {
		return fmt.Errorf("%w: journal snapshot metadata is incomplete", ErrInvalidReplayInput)
	}
	if source.RecordCount != uint64(len(source.Records)) || source.HeadSequence != source.Records[len(source.Records)-1].Sequence || source.HeadHash != source.Records[len(source.Records)-1].RecordHash {
		return fmt.Errorf("%w: journal snapshot counts or head are inconsistent", ErrInvalidReplayInput)
	}
	previousHash := journal.GenesisPreviousHash
	for index, record := range source.Records {
		if err := contextError(ctx); err != nil {
			return err
		}
		if record.SchemaVersion != journal.CurrentSchemaVersion {
			return fmt.Errorf("%w: sequence=%d", ErrInvalidReplayInput, record.Sequence)
		}
		if err := record.Kind.Validate(); err != nil {
			return fmt.Errorf("%w: sequence=%d: %v", ErrUnsupportedRecord, record.Sequence, err)
		}
		expected := uint64(index + 1)
		if record.Sequence != expected {
			return fmt.Errorf("%w: expected=%d found=%d", ErrInvalidReplayInput, expected, record.Sequence)
		}
		if record.RecordHash == "" || record.PreviousHash == "" || record.PreviousHash != previousHash || len(record.Payload) == 0 {
			return fmt.Errorf("%w: incomplete record at sequence=%d", ErrInvalidReplayInput, record.Sequence)
		}
		previousHash = record.RecordHash
	}
	if source.Records[0].Kind != journal.RecordKindGenesis || source.Records[0].Sequence != 1 {
		return fmt.Errorf("%w: journal genesis must be sequence one", ErrInvalidReplayInput)
	}
	genesis, err := decodeGenesis(source.Records[0])
	if err != nil {
		return err
	}
	if genesis.JournalID != source.JournalID || !genesis.CreatedAt.Equal(source.Records[0].RecordedAt) {
		return fmt.Errorf("%w: genesis identity does not match journal snapshot", ErrInvalidReplayInput)
	}
	return nil
}

func validateSnapshotMetadata(metadata persistence.SnapshotMetadata) error {
	if metadata.SchemaVersion != persistence.CurrentSchemaVersion || metadata.CreatedAt.IsZero() || metadata.ChainCount < 0 || metadata.PayloadSHA256 == "" || metadata.SizeBytes < 0 {
		return fmt.Errorf("%w: snapshot metadata is incomplete", ErrInvalidReplayInput)
	}
	return nil
}

type replayState struct {
	chains map[chains.ChainID]*chains.Chain
}

func newReplayState() *replayState {
	return &replayState{chains: make(map[chains.ChainID]*chains.Chain)}
}

func cloneRegistry(ctx context.Context, source *registry.Registry, expectedCount int) (*replayState, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	snapshots := source.List()
	if len(snapshots) != expectedCount {
		return nil, fmt.Errorf("%w: expected=%d found=%d", ErrCheckpointMetadataMismatch, expectedCount, len(snapshots))
	}
	target := newReplayState()
	for _, snapshot := range snapshots {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		chain, err := chains.Restore(snapshot)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrSnapshotCloneFailed, snapshot.ID, err)
		}
		if err := target.add(chain); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrSnapshotCloneFailed, snapshot.ID, err)
		}
	}
	return target, nil
}

func (s *replayState) add(chain *chains.Chain) error {
	if chain == nil {
		return ErrChainRestoreFailed
	}
	snapshot := chain.Snapshot()
	if _, exists := s.chains[snapshot.ID]; exists {
		return fmt.Errorf("%w: %s", ErrChainAlreadyExists, snapshot.ID)
	}
	owned, err := chain.Clone()
	if err != nil {
		return err
	}
	s.chains[snapshot.ID] = owned
	return nil
}

func (s *replayState) get(id chains.ChainID) (chains.Snapshot, error) {
	chain, ok := s.chains[id]
	if !ok {
		return chains.Snapshot{}, fmt.Errorf("%w: %s", ErrChainNotFound, id)
	}
	return chain.Snapshot(), nil
}

func (s *replayState) replace(chain *chains.Chain) error {
	if chain == nil {
		return ErrFinalRegistryInvalid
	}
	id := chain.Snapshot().ID
	if _, exists := s.chains[id]; !exists {
		return fmt.Errorf("%w: %s", ErrChainNotFound, id)
	}
	owned, err := chain.Clone()
	if err != nil {
		return err
	}
	s.chains[id] = owned
	return nil
}

func (s *replayState) registry(ctx context.Context) (*registry.Registry, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	ids := make([]chains.ChainID, 0, len(s.chains))
	for id := range s.chains {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	target := registry.New()
	for _, id := range ids {
		if err := contextError(ctx); err != nil {
			return nil, err
		}
		if err := target.Add(s.chains[id]); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrFinalRegistryInvalid, id, err)
		}
	}
	return target, nil
}

func replayRecords(ctx context.Context, target *replayState, records []journal.Record, start int, metadata *ReplayMetadata) error {
	for index := start; index < len(records); index++ {
		if err := contextError(ctx); err != nil {
			return err
		}
		record := records[index]
		var err error
		var chainID chains.ChainID
		switch record.Kind {
		case journal.RecordKindGenesis:
			if index != 0 {
				err = ErrInvalidReplayInput
				break
			}
			payload, decodeErr := decodeGenesis(record)
			if decodeErr != nil {
				err = decodeErr
				break
			}
			if payload.JournalID != metadata.JournalID {
				err = ErrInvalidReplayInput
			}
		case journal.RecordKindChainAdded:
			var snapshot chains.Snapshot
			snapshot, err = decodeChainAdded(record)
			chainID = snapshot.ID
			if err == nil {
				err = applyChainAdded(target, snapshot)
			}
			if err == nil {
				metadata.ChainsAdded++
				markApplied(metadata, record.Sequence)
			}
		case journal.RecordKindLifecycleTransitioned:
			var payload journal.LifecycleTransitionPayload
			payload, err = decodeLifecycle(record)
			chainID = payload.ChainID
			if err == nil {
				err = applyLifecycle(target, record, payload)
			}
			if err == nil {
				metadata.TransitionsApplied++
				markApplied(metadata, record.Sequence)
			}
		case journal.RecordKindObservationAdded:
			var payload journal.ObservationAddedPayload
			payload, err = decodeObservationAdded(record)
			chainID = payload.ChainID
			if err == nil {
				err = applyObservation(target, record, payload)
			}
			if err == nil {
				metadata.ObservationsAdded++
				markApplied(metadata, record.Sequence)
			}
		case journal.RecordKindContributionAdded:
			var payload journal.ContributionAddedPayload
			payload, err = decodeContributionAdded(record)
			chainID = payload.ChainID
			if err == nil {
				err = applyContribution(target, record, payload)
			}
			if err == nil {
				metadata.ContributionsAdded++
				markApplied(metadata, record.Sequence)
			}
		case journal.RecordKindSnapshotCheckpointed:
			var payload journal.SnapshotCheckpointPayload
			payload, err = decodeCheckpoint(record)
			if err == nil {
				head := records[len(records)-1].Sequence
				err = validateCheckpointPositionRecords(records, head, record, payload)
			}
		case journal.RecordKindHypothesisOpened, journal.RecordKindHypothesisStatusChanged, journal.RecordKindHypothesisRebased, journal.RecordKindHypothesisSuperseded:
			metadata.RecordsSkipped++
		case journal.RecordKindHypothesisResolved:
			var payload journal.HypothesisResolvedPayload
			payload, err = decodeHypothesisResolved(record)
			metadata.ResolutionsExamined++
			if err == nil {
				err = applyHypothesisResolved(target, record, payload)
			}
			if err == nil {
				metadata.ResolutionsApplied++
				switch payload.ChainDelta.Kind {
				case hypotheses.ResolutionEffectAttachObservation:
					metadata.ResolutionObservationEffects++
				case hypotheses.ResolutionEffectCreateCandidate:
					metadata.ResolutionChainCreations++
				case hypotheses.ResolutionEffectAddContribution:
					metadata.ResolutionContributionEffects++
				case hypotheses.ResolutionEffectNoChain:
					metadata.ResolutionNoChainEffects++
				}
				markApplied(metadata, record.Sequence)
			}
		case journal.RecordKindRoutineCreated, journal.RecordKindRoutineOccurrenceAdded, journal.RecordKindRoutineStatusChanged:
			metadata.RecordsSkipped++
		default:
			err = ErrUnsupportedRecord
		}
		if err != nil {
			return ReplayError{Sequence: record.Sequence, Kind: record.Kind, ChainID: chainID, Err: err}
		}
	}
	return nil
}

func decodeHypothesisResolved(record journal.Record) (journal.HypothesisResolvedPayload, error) {
	var payload journal.HypothesisResolvedPayload
	if err := decodePayload(record.Payload, &payload); err != nil {
		return payload, fmt.Errorf("%w: hypothesis.resolved: %v", ErrInvalidReplayInput, err)
	}
	if payload.HypothesisRevision.Actor != record.Actor || payload.HypothesisRevision.CorrelationID != record.CorrelationID || record.RecordedAt.Before(payload.HypothesisRevision.At) {
		return payload, ErrRevisionRecordMismatch
	}
	return payload, nil
}

func applyHypothesisResolved(target *replayState, record journal.Record, payload journal.HypothesisResolvedPayload) error {
	if err := validateResolutionDelta(payload); err != nil {
		return err
	}
	switch payload.ChainDelta.Kind {
	case hypotheses.ResolutionEffectAttachObservation:
		before, err := target.get(payload.ChainDelta.ObservationAdded.ChainID)
		if err != nil {
			return err
		}
		if err := applyObservation(target, record, *payload.ChainDelta.ObservationAdded); err != nil {
			return err
		}
		after, err := target.get(before.ID)
		if err != nil {
			return err
		}
		if after.Revision != payload.Outcome.AttachObservation.NewRevision || after.History[len(after.History)-1].Operation != chains.OperationObservationAdded {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectCreateCandidate:
		if err := applyChainAdded(target, payload.ChainDelta.ChainAdded.Chain); err != nil {
			return err
		}
		created, err := target.get(payload.ChainDelta.ChainAdded.Chain.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(created, payload.ChainDelta.ChainAdded.Chain) {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectAddContribution:
		before, err := target.get(payload.ChainDelta.ContributionAdded.ChainID)
		if err != nil {
			return err
		}
		if err := applyContribution(target, record, *payload.ChainDelta.ContributionAdded); err != nil {
			return err
		}
		after, err := target.get(before.ID)
		if err != nil {
			return err
		}
		if after.Revision != payload.Outcome.AddContribution.NewRevision || after.CurrentConfidence != payload.Outcome.AddContribution.NewConfidence {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectNoChain:
		if payload.ChainDelta.NoChainEffect == nil {
			return ErrRevisionRecordMismatch
		}
	}
	return nil
}

func validateResolutionDelta(payload journal.HypothesisResolvedPayload) error {
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 || payload.PreviousStatus != hypotheses.StatusOpen && payload.PreviousStatus != hypotheses.StatusUnderReview || payload.NewStatus != hypotheses.StatusResolved || payload.AssessmentVersion == 0 || payload.AssessmentID == "" || payload.AlternativeID == "" || payload.AlternativeKind.Validate() != nil || payload.Effect.Kind != payload.ChainDelta.Kind {
		return ErrRevisionMismatch
	}
	fingerprint, err := payload.Effect.Fingerprint()
	if err != nil || fingerprint != payload.EffectFingerprint {
		return ErrRevisionRecordMismatch
	}
	if err := payload.Outcome.Validate(); err != nil || payload.Outcome.Kind != payload.Effect.Kind {
		return ErrRevisionRecordMismatch
	}
	if err := payload.HypothesisRevision.Validate(); err != nil || payload.HypothesisRevision.Operation != hypotheses.OperationHypothesisResolved || payload.HypothesisRevision.PreviousRevision != payload.PreviousRevision || payload.HypothesisRevision.NewRevision != payload.NewRevision || payload.HypothesisRevision.SelectedEffectFingerprint != payload.EffectFingerprint {
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
		if p == nil || e == nil || o == nil || p.ChainID != e.ChainID || p.PreviousRevision != e.SourceRevision || p.Observation != e.Observation || p.NewRevision != o.NewRevision || p.Revision.PreviousRevision != o.PreviousRevision || p.Revision.NewRevision != o.NewRevision {
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
		if p.ChainID != e.ChainID || p.PreviousRevision != e.SourceRevision || p.Contribution.ID != t.ID || p.Contribution.Source != t.Source || p.Contribution.Kind != t.Kind || p.Contribution.Value != t.Value || p.Contribution.Reason != t.ReasonCode || p.Contribution.CreatedAt != payload.HypothesisRevision.At || !equalStrings(p.Contribution.ObservationIDs, t.ObservationIDs) || p.NewRevision != o.NewRevision || p.PreviousConfidence != o.PreviousConfidence || p.NewConfidence != o.NewConfidence {
			return ErrRevisionRecordMismatch
		}
	case hypotheses.ResolutionEffectNoChain:
		if payload.ChainDelta.NoChainEffect == nil || payload.Outcome.NoChainEffect == nil || payload.Effect.NoChainEffect == nil || payload.ChainDelta.NoChainEffect.ReasonCode != payload.Effect.NoChainEffect.ReasonCode || payload.ChainDelta.NoChainEffect.ReasonCode != payload.Outcome.NoChainEffect.ReasonCode {
			return ErrRevisionRecordMismatch
		}
	}
	return nil
}

func applyChainAdded(target *replayState, snapshot chains.Snapshot) error {
	if _, err := chains.NewChainID(string(snapshot.ID)); err != nil {
		return fmt.Errorf("%w: %v", ErrChainRestoreFailed, err)
	}
	chain, err := chains.Restore(snapshot)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrChainRestoreFailed, snapshot.ID, err)
	}
	if err := target.add(chain); err != nil {
		if errors.Is(err, ErrChainAlreadyExists) {
			return err
		}
		return fmt.Errorf("%w: %v", ErrChainRestoreFailed, err)
	}
	return nil
}

func applyLifecycle(target *replayState, record journal.Record, payload journal.LifecycleTransitionPayload) error {
	if err := validateLifecyclePayload(record, payload); err != nil {
		return err
	}
	current, err := target.get(payload.ChainID)
	if err != nil {
		return err
	}
	if current.Revision != payload.PreviousRevision {
		return fmt.Errorf("%w: chain=%s expected=%d found=%d", ErrRevisionMismatch, payload.ChainID, payload.PreviousRevision, current.Revision)
	}
	if current.Status != payload.From {
		return fmt.Errorf("%w: chain=%s expected=%s found=%s", ErrStatusMismatch, payload.ChainID, payload.From, current.Status)
	}
	clone, err := chains.Restore(current)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSnapshotCloneFailed, payload.ChainID, err)
	}
	mutation := chains.MutationContext{
		At:             payload.Revision.At,
		Actor:          payload.Revision.Actor,
		Reason:         payload.Revision.Reason,
		CorrelationID:  payload.Revision.CorrelationID,
		ObservationIDs: append([]string(nil), payload.Revision.ObservationIDs...),
	}
	if err := clone.SetStatus(payload.To, mutation); err != nil {
		if errors.Is(err, chains.ErrInvalidTransition) {
			return fmt.Errorf("%w: %v", ErrInvalidTransition, err)
		}
		return err
	}
	history := clone.History()
	if len(history) == 0 || !revisionRecordEqual(history[len(history)-1], payload.Revision) {
		return ErrRevisionRecordMismatch
	}
	if clone.Snapshot().Revision != payload.NewRevision {
		return fmt.Errorf("%w: produced=%d expected=%d", ErrRevisionMismatch, clone.Snapshot().Revision, payload.NewRevision)
	}
	if err := clone.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	if err := target.replace(clone); err != nil {
		return fmt.Errorf("%w: replace chain: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func applyObservation(target *replayState, record journal.Record, payload journal.ObservationAddedPayload) error {
	current, err := target.get(payload.ChainID)
	if err != nil {
		return err
	}
	if current.Revision != payload.PreviousRevision {
		return fmt.Errorf("%w: chain=%s expected=%d found=%d", ErrRevisionMismatch, payload.ChainID, payload.PreviousRevision, current.Revision)
	}
	if err := current.Status.ValidateObservationMutation(); err != nil {
		return err
	}
	clone, err := chains.Restore(current)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSnapshotCloneFailed, payload.ChainID, err)
	}
	mutation := chains.MutationContext{
		At: payload.Revision.At, Actor: payload.Revision.Actor, Reason: payload.Revision.Reason,
		CorrelationID:  payload.Revision.CorrelationID,
		ObservationIDs: append([]string(nil), payload.Revision.ObservationIDs...),
	}
	if err := clone.AddObservation(payload.Observation, mutation); err != nil {
		return err
	}
	after := clone.Snapshot()
	if after.Revision != payload.NewRevision || after.Status != current.Status || len(after.Observations) != len(current.Observations)+1 {
		return ErrRevisionMismatch
	}
	if len(after.History) == 0 || !revisionRecordEqual(after.History[len(after.History)-1], payload.Revision) {
		return ErrRevisionRecordMismatch
	}
	if !containsObservation(after.Observations, payload.Observation) {
		return ErrRevisionRecordMismatch
	}
	if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
		return ErrRevisionRecordMismatch
	}
	if err := clone.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	if err := target.replace(clone); err != nil {
		return fmt.Errorf("%w: replace chain: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func applyContribution(target *replayState, record journal.Record, payload journal.ContributionAddedPayload) error {
	current, err := target.get(payload.ChainID)
	if err != nil {
		return err
	}
	if current.Revision != payload.PreviousRevision {
		return fmt.Errorf("%w: chain=%s expected=%d found=%d", ErrRevisionMismatch, payload.ChainID, payload.PreviousRevision, current.Revision)
	}
	if err := current.Status.ValidateContributionMutation(); err != nil {
		return err
	}
	if payload.PreviousConfidence != current.CurrentConfidence || payload.PreviousSupportCount != current.ConfirmationCount || payload.PreviousContradictionCount != current.ContradictionCount {
		return errors.Join(ErrContributionResultMismatch, ErrRevisionMismatch)
	}
	for _, observationID := range payload.Contribution.ObservationIDs {
		if !containsObservationID(current.Observations, observationID) {
			return ErrUnknownObservationReference
		}
	}
	for _, contribution := range current.Contributions {
		if contribution.ID == payload.Contribution.ID {
			return ErrDuplicateContribution
		}
	}
	clone, err := chains.Restore(current)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrSnapshotCloneFailed, payload.ChainID, err)
	}
	mutation := chains.MutationContext{
		At: payload.Revision.At, Actor: payload.Revision.Actor, Reason: payload.Revision.Reason,
		CorrelationID:  payload.Revision.CorrelationID,
		ObservationIDs: append([]string(nil), payload.Revision.ObservationIDs...),
	}
	if err := clone.AddContribution(payload.Contribution.Clone(), mutation); err != nil {
		return err
	}
	after := clone.Snapshot()
	if after.Revision != payload.NewRevision || after.Status != current.Status || after.HistoricalReliability != current.HistoricalReliability || len(after.Contributions) != len(current.Contributions)+1 || after.CurrentConfidence != payload.NewConfidence || after.ConfirmationCount != payload.NewSupportCount || after.ContradictionCount != payload.NewContradictionCount {
		return errors.Join(ErrContributionResultMismatch, ErrRevisionMismatch)
	}
	if len(after.History) == 0 || !revisionRecordEqual(after.History[len(after.History)-1], payload.Revision) {
		return ErrRevisionRecordMismatch
	}
	if !containsContribution(after.Contributions, payload.Contribution) {
		return ErrRevisionRecordMismatch
	}
	if payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
		return ErrRevisionRecordMismatch
	}
	if err := clone.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrFinalRegistryInvalid, err)
	}
	if err := target.replace(clone); err != nil {
		return fmt.Errorf("%w: replace chain: %v", ErrFinalRegistryInvalid, err)
	}
	return nil
}

func containsObservation(observations []chains.ObservationRef, expected chains.ObservationRef) bool {
	for _, observation := range observations {
		if reflect.DeepEqual(observation, expected) {
			return true
		}
	}
	return false
}

func containsObservationID(observations []chains.ObservationRef, expected string) bool {
	for _, observation := range observations {
		if observation.ID == expected {
			return true
		}
	}
	return false
}

func containsContribution(contributions []chains.ConfidenceContribution, expected chains.ConfidenceContribution) bool {
	for _, contribution := range contributions {
		if contribution.ID == expected.ID && contribution.Source == expected.Source && contribution.Kind == expected.Kind && contribution.Value == expected.Value && contribution.Reason == expected.Reason && contribution.CreatedAt.Equal(expected.CreatedAt) && equalStrings(contribution.ObservationIDs, expected.ObservationIDs) {
			return true
		}
	}
	return false
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func validateLifecyclePayload(record journal.Record, payload journal.LifecycleTransitionPayload) error {
	if _, err := chains.NewChainID(string(payload.ChainID)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidReplayInput, err)
	}
	if payload.PreviousRevision == 0 || payload.NewRevision != payload.PreviousRevision+1 {
		return ErrRevisionMismatch
	}
	if err := chains.ValidateTransition(payload.From, payload.To); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTransition, err)
	}
	if payload.Revision.ChainID != payload.ChainID || payload.Revision.PreviousRevision != payload.PreviousRevision || payload.Revision.NewRevision != payload.NewRevision || payload.Revision.PreviousStatus != payload.From || payload.Revision.NewStatus != payload.To {
		return ErrRevisionRecordMismatch
	}
	if !payload.Revision.At.Equal(record.RecordedAt) || payload.Revision.Actor != record.Actor || payload.Revision.CorrelationID != record.CorrelationID {
		return ErrRevisionRecordMismatch
	}
	expectedOperation := chains.OperationStatusChanged
	if payload.To == chains.StatusArchived {
		expectedOperation = chains.OperationChainArchived
	}
	if payload.To == chains.StatusReactivated {
		expectedOperation = chains.OperationChainReactivated
	}
	if payload.Revision.Operation != expectedOperation {
		return ErrRevisionRecordMismatch
	}
	return nil
}

func revisionRecordEqual(left, right chains.RevisionRecord) bool {
	return left.ChainID == right.ChainID &&
		left.Operation == right.Operation &&
		left.PreviousRevision == right.PreviousRevision &&
		left.NewRevision == right.NewRevision &&
		left.At.Equal(right.At) &&
		left.Actor == right.Actor &&
		left.Reason == right.Reason &&
		left.CorrelationID == right.CorrelationID &&
		reflect.DeepEqual(left.ObservationIDs, right.ObservationIDs) &&
		reflect.DeepEqual(left.ContributionIDs, right.ContributionIDs) &&
		left.PreviousStatus == right.PreviousStatus &&
		left.NewStatus == right.NewStatus &&
		reflect.DeepEqual(left.PreviousEntityID, right.PreviousEntityID) &&
		reflect.DeepEqual(left.NewEntityID, right.NewEntityID) &&
		reflect.DeepEqual(left.PreviousConfidence, right.PreviousConfidence) &&
		reflect.DeepEqual(left.NewConfidence, right.NewConfidence) &&
		reflect.DeepEqual(left.PreviousHistoricalReliability, right.PreviousHistoricalReliability) &&
		reflect.DeepEqual(left.NewHistoricalReliability, right.NewHistoricalReliability)
}

func countCheckpoints(source journal.JournalSnapshot) uint64 {
	var count uint64
	for _, record := range source.Records {
		if record.Kind == journal.RecordKindSnapshotCheckpointed {
			count++
		}
	}
	return count
}

func markApplied(metadata *ReplayMetadata, sequence uint64) {
	metadata.RecordsApplied++
	if metadata.FirstAppliedSequence == 0 {
		metadata.FirstAppliedSequence = sequence
	}
	metadata.LastAppliedSequence = sequence
}

func finalize(ctx context.Context, target *registry.Registry, metadata *ReplayMetadata) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	snapshots := target.List()
	for _, snapshot := range snapshots {
		if err := contextError(ctx); err != nil {
			return err
		}
		chain, err := chains.Restore(snapshot)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrFinalRegistryInvalid, snapshot.ID, err)
		}
		if err := chain.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrFinalRegistryInvalid, snapshot.ID, err)
		}
	}
	metadata.FinalChainCount = len(snapshots)
	return nil
}
