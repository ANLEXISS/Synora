package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"synora/internal/cge/chains/generations"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/replay"
	hypothesisreplay "synora/internal/cge/hypotheses/replay"
	routinereplay "synora/internal/cge/routines/replay"
)

// SnapshotGenerationResult identifies the last durable step reached. Pending
// is retained when a snapshot exists but no checkpointed Generation exists.
type SnapshotGenerationResult struct {
	Generation generations.Generation
	Pending    *generations.PendingGeneration

	SnapshotWritten    bool
	CheckpointAppended bool
	ManifestPublished  bool

	PreviousManifest *generations.Manifest
}

// CreateSnapshotGeneration serializes snapshot creation with all WAL writes.
// The checkpoint is appended only after the generation file is durable; the
// active manifest is replaced only after the checkpoint record is validated.
// A clean checkpoint rejection leaves the coordinator ready with an inactive
// orphan snapshot. An ambiguous checkpoint append degrades it because the
// journal head cannot safely be guessed. A clean manifest failure leaves the
// coordinator ready with the durable checkpoint and previous manifest; an
// ambiguous manifest outcome degrades it until explicit recovery.
func (c *Coordinator) CreateSnapshotGeneration(ctx context.Context, store *generations.Store, createdAt time.Time, actor, correlationID string) (SnapshotGenerationResult, error) {
	if err := validateContext(ctx); err != nil {
		return SnapshotGenerationResult{}, err
	}
	if c == nil {
		return SnapshotGenerationResult{}, ErrCoordinatorClosed
	}
	if store == nil {
		return SnapshotGenerationResult{}, coordinatorError("snapshot", "capture", "", generations.ErrInvalidGenerationStore)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return SnapshotGenerationResult{}, err
	}
	if err := validateMutationInput(actor, correlationID, createdAt); err != nil {
		return SnapshotGenerationResult{}, coordinatorError("snapshot", "capture", "", err)
	}
	previous, previousErr := store.LoadManifest(ctx)
	if previousErr != nil && !errors.Is(previousErr, generations.ErrManifestNotFound) {
		return SnapshotGenerationResult{}, coordinatorError("snapshot", "capture", "", previousErr)
	}
	result := SnapshotGenerationResult{}
	if previousErr == nil {
		copy := previous
		result.PreviousManifest = &copy
	}
	candidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return result, coordinatorError("snapshot", "capture", "", err)
	}
	includedSequence, includedHash := c.lastSequence, c.lastHash
	pending, err := store.CreateGeneration(ctx, candidate, includedSequence, includedHash, createdAt)
	if err != nil {
		return result, snapshotError("snapshot_write", "", includedSequence, err)
	}
	result.SnapshotWritten = true
	result.Pending = pendingPointer(pending)
	checkpointPayload := journal.SnapshotCheckpointPayload{
		SnapshotSchemaVersion: pending.Metadata.SchemaVersion,
		SnapshotCreatedAt:     pending.Metadata.CreatedAt,
		SnapshotChainCount:    pending.Metadata.ChainCount,
		SnapshotPayloadSHA256: pending.Metadata.PayloadSHA256,
		SnapshotSizeBytes:     pending.Metadata.SizeBytes,
		JournalSequence:       pending.IncludedJournalSequence,
		JournalHeadHash:       pending.IncludedJournalHeadHash,
	}
	expectation := appendExpectation{
		kind: journal.RecordKindSnapshotCheckpointed, recordedAt: createdAt,
		actor: actor, correlation: correlationID, checkpoint: &checkpointPayload,
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendSnapshotCheckpoint(ctx, journal.SnapshotCheckpointInput{
		SnapshotSchemaVersion: pending.Metadata.SchemaVersion,
		SnapshotCreatedAt:     pending.Metadata.CreatedAt,
		SnapshotChainCount:    pending.Metadata.ChainCount,
		SnapshotPayloadSHA256: pending.Metadata.PayloadSHA256,
		SnapshotSizeBytes:     pending.Metadata.SizeBytes,
		JournalSequence:       pending.IncludedJournalSequence,
		JournalHeadHash:       pending.IncludedJournalHeadHash,
		RecordedAt:            createdAt, Actor: actor, CorrelationID: correlationID,
	})
	if err != nil {
		_, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, err)
		checkpointError := ErrCheckpointAppendFailed
		if failure.Outcome == AppendUncertain {
			checkpointError = ErrCheckpointUncertain
		} else {
			checkpointError = errors.Join(checkpointError, ErrSnapshotOrphaned)
		}
		return result, snapshotError("checkpoint_append", pending.GenerationID, pending.IncludedJournalSequence, errors.Join(checkpointError, failure))
	}
	if err := c.acceptAppendLocked("snapshot", "", expectation, record, beforeSequence, beforeHash); err != nil {
		return result, snapshotError("checkpoint_append", pending.GenerationID, pending.IncludedJournalSequence, err)
	}
	generation, err := pending.Finalize(record)
	if err != nil {
		c.state = StateDegraded
		c.degradedReason = "journal_append_ambiguous"
		return result, snapshotError("checkpoint_append", pending.GenerationID, pending.IncludedJournalSequence, err)
	}
	result.Generation = generation
	result.CheckpointAppended = true
	if err := store.PublishManifest(ctx, generation, record, createdAt); err != nil {
		if published, clean := c.resolveManifestOutcome(ctx, store, generation, result.PreviousManifest); published {
			result.ManifestPublished = true
			result.Pending = nil
			return result, nil
		} else if clean {
			return result, snapshotError("manifest_publish", generation.GenerationID, generation.IncludedJournalSequence, fmt.Errorf("%w: %v", generations.ErrManifestWriteFailed, err))
		}
		c.state = StateDegraded
		c.degradedReason = "manifest_publish_ambiguous"
		return result, snapshotError("manifest_publish", generation.GenerationID, generation.IncludedJournalSequence, fmt.Errorf("%w: %v", generations.ErrManifestWriteFailed, err))
	}
	result.ManifestPublished = true
	result.Pending = nil
	return result, nil
}

// FromGenerationManifest constructs a coordinator from the active manifest,
// its exact checkpoint, and all deltas after that checkpoint.
func FromGenerationManifest(ctx context.Context, store *generations.Store, source *journal.FileJournal) (*Coordinator, RecoveryMetadata, error) {
	if err := validateContext(ctx); err != nil {
		return nil, RecoveryMetadata{}, err
	}
	if store == nil || source == nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: generation store and journal are required", ErrInvalidCoordinatorInput)
	}
	manifest, err := store.LoadManifest(ctx)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "manifest_reload", "", 0, err)
	}
	current, metadata, err := store.LoadGeneration(ctx, manifest.Active)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	journalSnapshot, err := source.ReadAll(ctx)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	checkpoint, found := findCheckpointRecord(journalSnapshot, manifest.Active)
	if !found {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, generations.ErrCheckpointNotFound)
	}
	if err := manifest.Active.ValidateCheckpoint(checkpoint); err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	rebuilt, replayMetadata, err := replay.FromSnapshotAndJournalAtCheckpoint(
		ctx,
		current,
		metadata,
		journalSnapshot,
		manifest.Active.CheckpointRecordSequence,
		manifest.Active.CheckpointRecordHash,
	)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	hypothesisRegistry, hypothesisMetadata, err := hypothesisreplay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	routineRegistry, routineMetadata, err := routinereplay.ReplayRecords(journalSnapshot.Records)
	if err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	if err := validateReplayHeads(replayMetadata.FinalHeadSequence, replayMetadata.FinalHeadHash, hypothesisMetadata.FinalHeadSequence, hypothesisMetadata.FinalHeadHash, routineMetadata.FinalSequence, routineMetadata.FinalHash); err != nil {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, err)
	}
	if replayMetadata.CheckpointSequence != manifest.Active.CheckpointRecordSequence || replayMetadata.CheckpointHash != manifest.Active.CheckpointRecordHash {
		return nil, RecoveryMetadata{}, generationError("manifest_recovery", "recovery", manifest.Active.GenerationID, manifest.Active.IncludedJournalSequence, generations.ErrCheckpointMismatch)
	}
	coordinator := &Coordinator{
		current: rebuilt, currentHypotheses: hypothesisRegistry, currentRoutines: routineRegistry, journal: source, state: StateReady,
		lastSequence: journalSnapshot.HeadSequence, lastHash: journalSnapshot.HeadHash,
	}
	return coordinator, recoveryMetadata(replayMetadata, hypothesisMetadata, routineMetadata, coordinator), nil
}

func (c *Coordinator) resolveManifestOutcome(ctx context.Context, store *generations.Store, generation generations.Generation, previous *generations.Manifest) (published, cleanFailure bool) {
	manifest, err := store.LoadManifest(ctx)
	if err == nil && reflect.DeepEqual(manifest.Active, generation) {
		return true, false
	}
	if err != nil && errors.Is(err, generations.ErrManifestNotFound) && previous == nil {
		return false, true
	}
	if err == nil && previous != nil && reflect.DeepEqual(manifest, *previous) {
		return false, true
	}
	if err == nil && previous == nil {
		return false, true
	}
	return false, false
}

func findCheckpointRecord(snapshot journal.JournalSnapshot, generation generations.Generation) (journal.Record, bool) {
	for _, record := range snapshot.Records {
		if record.Sequence == generation.CheckpointRecordSequence && record.RecordHash == generation.CheckpointRecordHash && record.Kind == journal.RecordKindSnapshotCheckpointed {
			return record, true
		}
	}
	return journal.Record{}, false
}

func pendingPointer(pending generations.PendingGeneration) *generations.PendingGeneration {
	copy := pending
	return &copy
}

func snapshotError(step, generationID string, sequence uint64, err error) error {
	return generationError("snapshot", step, generationID, sequence, err)
}

func generationError(operation, step, generationID string, sequence uint64, err error) error {
	return CoordinatorError{
		Operation:               operation,
		Step:                    step,
		GenerationID:            generationID,
		IncludedJournalSequence: sequence,
		Err:                     err,
	}
}
