package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/chains/replay"
	"synora/internal/cge/hypotheses"
	hypothesisreplay "synora/internal/cge/hypotheses/replay"
	"synora/internal/cge/routines"
	routinereplay "synora/internal/cge/routines/replay"
)

const (
	maxActorLength       = 128
	maxCorrelationLength = 256
)

// Coordinator is the durable ownership boundary for the mutations supported
// by this pass. It does not expose its registry or journal pointers.
type Coordinator struct {
	mu sync.RWMutex

	current           *registry.Registry
	currentHypotheses *hypotheses.Registry
	currentRoutines   *routines.Registry
	journal           *journal.FileJournal

	state          CoordinatorState
	lastSequence   uint64
	lastHash       string
	degradedReason string

	publishHook func() error
}

// SetQualificationPublicationHook installs a per-coordinator validation seam.
// It is intentionally explicit and unused by runtime callers.
func (c *Coordinator) SetQualificationPublicationHook(hook func() error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.publishHook = hook
}

// FromJournal constructs a ready coordinator only after the complete journal
// has been read and deterministically replayed into a new registry.
func FromJournal(ctx context.Context, source *journal.FileJournal) (*Coordinator, RecoveryMetadata, error) {
	if err := validateContext(ctx); err != nil {
		return nil, RecoveryMetadata{}, err
	}
	if source == nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: journal is nil", ErrInvalidCoordinatorInput)
	}
	journalSnapshot, err := source.ReadAll(ctx)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: read journal: %w", ErrRecoveryFailed, err)
	}
	current, replayMetadata, err := replay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay journal: %w", ErrRecoveryFailed, err)
	}
	hypothesisRegistry, hypothesisMetadata, err := hypothesisreplay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay hypotheses: %w", ErrRecoveryFailed, err)
	}
	routineRegistry, routineMetadata, err := routinereplay.ReplayRecords(journalSnapshot.Records)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay routines: %w", ErrRecoveryFailed, err)
	}
	if err := validateReplayHeads(replayMetadata.FinalHeadSequence, replayMetadata.FinalHeadHash, hypothesisMetadata.FinalHeadSequence, hypothesisMetadata.FinalHeadHash, routineMetadata.FinalSequence, routineMetadata.FinalHash); err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay heads: %w", ErrRecoveryFailed, err)
	}
	coordinator := &Coordinator{
		current: current, currentHypotheses: hypothesisRegistry, currentRoutines: routineRegistry, journal: source,
		state: StateReady, lastSequence: journalSnapshot.HeadSequence, lastHash: journalSnapshot.HeadHash,
	}
	return coordinator, recoveryMetadata(replayMetadata, hypothesisMetadata, routineMetadata, coordinator), nil
}

// FromSnapshotAndJournal loads and validates a snapshot, reads the journal,
// and uses replay's exact checkpoint selection. It never saves a snapshot or
// appends a checkpoint.
func FromSnapshotAndJournal(
	ctx context.Context,
	snapshotStore *persistence.FileStore,
	source *journal.FileJournal,
) (*Coordinator, RecoveryMetadata, error) {
	if err := validateContext(ctx); err != nil {
		return nil, RecoveryMetadata{}, err
	}
	if snapshotStore == nil || source == nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: snapshot store and journal are required", ErrInvalidCoordinatorInput)
	}
	snapshotRegistry, snapshotMetadata, err := snapshotStore.Load(ctx)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: load snapshot: %w", ErrRecoveryFailed, err)
	}
	journalSnapshot, err := source.ReadAll(ctx)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: read journal: %w", ErrRecoveryFailed, err)
	}
	current, replayMetadata, err := replay.FromSnapshotAndJournal(ctx, snapshotRegistry, snapshotMetadata, journalSnapshot)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay snapshot and journal: %w", ErrRecoveryFailed, err)
	}
	hypothesisRegistry, hypothesisMetadata, err := hypothesisreplay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay hypotheses: %w", ErrRecoveryFailed, err)
	}
	routineRegistry, routineMetadata, err := routinereplay.ReplayRecords(journalSnapshot.Records)
	if err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay routines: %w", ErrRecoveryFailed, err)
	}
	if err := validateReplayHeads(replayMetadata.FinalHeadSequence, replayMetadata.FinalHeadHash, hypothesisMetadata.FinalHeadSequence, hypothesisMetadata.FinalHeadHash, routineMetadata.FinalSequence, routineMetadata.FinalHash); err != nil {
		return nil, RecoveryMetadata{}, fmt.Errorf("%w: replay heads: %w", ErrRecoveryFailed, err)
	}
	coordinator := &Coordinator{
		current: current, currentHypotheses: hypothesisRegistry, currentRoutines: routineRegistry, journal: source,
		state: StateReady, lastSequence: journalSnapshot.HeadSequence, lastHash: journalSnapshot.HeadHash,
	}
	return coordinator, recoveryMetadata(replayMetadata, hypothesisMetadata, routineMetadata, coordinator), nil
}

func validateReplayHeads(chainSequence uint64, chainHash string, hypothesisSequence uint64, hypothesisHash string, routineSequence uint64, routineHash string) error {
	if chainSequence != hypothesisSequence || chainHash != hypothesisHash || chainSequence != routineSequence || chainHash != routineHash {
		return fmt.Errorf("journal heads differ: chains=%d/%s hypotheses=%d/%s routines=%d/%s", chainSequence, chainHash, hypothesisSequence, hypothesisHash, routineSequence, routineHash)
	}
	return nil
}

func recoveryMetadata(metadata replay.ReplayMetadata, hypothesisMetadata hypothesisreplay.ReplayMetadata, routineMetadata routinereplay.Metadata, coordinator *Coordinator) RecoveryMetadata {
	return RecoveryMetadata{
		Replay:           metadata,
		State:            coordinator.state,
		ChainCount:       coordinator.current.Count(),
		HypothesisCount:  coordinator.currentHypotheses.Count(),
		JournalSequence:  coordinator.lastSequence,
		JournalHeadHash:  coordinator.lastHash,
		HypothesisReplay: hypothesisMetadata,
		RoutineReplay:    routineMetadata,
	}
}

// Close prevents further operations. FileJournal has no owned open handle, so
// closing is a logical state transition only.
func (c *Coordinator) Close() error {
	if c == nil {
		return ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == StateClosed {
		return ErrCoordinatorClosed
	}
	c.state = StateClosed
	c.degradedReason = ""
	return nil
}

// Status returns a detached operational view.
func (c *Coordinator) Status() StatusSnapshot {
	if c == nil {
		return StatusSnapshot{State: StateClosed}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	status := StatusSnapshot{
		State:           c.state,
		JournalSequence: c.lastSequence,
		JournalHeadHash: c.lastHash,
		DegradedReason:  c.degradedReason,
	}
	if c.current != nil {
		status.ChainCount = c.current.Count()
	}
	if c.currentHypotheses != nil {
		status.HypothesisCount = c.currentHypotheses.Count()
	}
	if c.currentRoutines != nil {
		status.RoutineCount = c.currentRoutines.Count()
	}
	return status
}

// Get returns a defensive snapshot. Degraded coordinators remain readable;
// closed coordinators reject new reads.
func (c *Coordinator) Get(id chains.ChainID) (chains.Snapshot, error) {
	if c == nil {
		return chains.Snapshot{}, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return chains.Snapshot{}, ErrCoordinatorClosed
	}
	return c.current.Get(id)
}

// List returns deterministic defensive snapshots. A closed coordinator has
// no readable current state through this façade.
func (c *Coordinator) List() []chains.Snapshot {
	if c == nil {
		return []chains.Snapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.current == nil {
		return []chains.Snapshot{}
	}
	return c.current.List()
}

// Count returns the number of chains visible through the current state.
func (c *Coordinator) Count() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.current == nil {
		return 0
	}
	return c.current.Count()
}

func (c *Coordinator) GetHypothesis(id hypotheses.SetID) (hypotheses.Snapshot, error) {
	if c == nil {
		return hypotheses.Snapshot{}, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return hypotheses.Snapshot{}, ErrCoordinatorClosed
	}
	if c.currentHypotheses == nil {
		return hypotheses.Snapshot{}, ErrCoordinatorNotReady
	}
	return c.currentHypotheses.Get(id)
}

func (c *Coordinator) ListHypotheses() []hypotheses.Snapshot {
	if c == nil {
		return []hypotheses.Snapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.currentHypotheses == nil {
		return []hypotheses.Snapshot{}
	}
	return c.currentHypotheses.List()
}

func (c *Coordinator) CountHypotheses() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.currentHypotheses == nil {
		return 0
	}
	return c.currentHypotheses.Count()
}

func (c *Coordinator) GetRoutine(id routines.RoutineID) (routines.Snapshot, error) {
	if c == nil {
		return routines.Snapshot{}, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return routines.Snapshot{}, ErrCoordinatorClosed
	}
	if c.currentRoutines == nil {
		return routines.Snapshot{}, ErrCoordinatorNotReady
	}
	return c.currentRoutines.Get(id)
}

func (c *Coordinator) ListRoutines() []routines.Snapshot {
	if c == nil {
		return []routines.Snapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.currentRoutines == nil {
		return []routines.Snapshot{}
	}
	return c.currentRoutines.List()
}

func (c *Coordinator) ListRoutinesBySubject(subject routines.Subject) ([]routines.Snapshot, error) {
	if c == nil {
		return nil, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return nil, ErrCoordinatorClosed
	}
	if c.currentRoutines == nil {
		return nil, ErrCoordinatorNotReady
	}
	return c.currentRoutines.ListBySubject(subject)
}

// ListRoutinesBySubjectAndKind returns detached snapshots from the derived
// subject/kind index. It never scans unrelated routines or exposes mutable
// registry state.
func (c *Coordinator) ListRoutinesBySubjectAndKind(subject routines.Subject, kind routines.Kind) ([]routines.Snapshot, error) {
	if c == nil {
		return nil, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return nil, ErrCoordinatorClosed
	}
	if c.currentRoutines == nil {
		return nil, ErrCoordinatorNotReady
	}
	return c.currentRoutines.ListBySubjectAndKind(subject, kind)
}

func (c *Coordinator) RoutineCount() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed || c.currentRoutines == nil {
		return 0
	}
	return c.currentRoutines.Count()
}

// FindCurrentEvidenceSubject is a targeted defensive hypothesis lookup. The
// registry index is derived from replay and is never persisted.
func (c *Coordinator) FindCurrentEvidenceSubject(chainID chains.ChainID, observationID string) (hypotheses.Snapshot, bool, error) {
	if c == nil {
		return hypotheses.Snapshot{}, false, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return hypotheses.Snapshot{}, false, ErrCoordinatorClosed
	}
	if c.currentHypotheses == nil {
		return hypotheses.Snapshot{}, false, ErrCoordinatorNotReady
	}
	return c.currentHypotheses.FindCurrentEvidenceSubject(chainID, observationID)
}

// FindCurrentAssociationSubject is the corresponding targeted association
// hypothesis lookup.
func (c *Coordinator) FindCurrentAssociationSubject(observationID string) (hypotheses.Snapshot, bool, error) {
	if c == nil {
		return hypotheses.Snapshot{}, false, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return hypotheses.Snapshot{}, false, ErrCoordinatorClosed
	}
	if c.currentHypotheses == nil {
		return hypotheses.Snapshot{}, false, ErrCoordinatorNotReady
	}
	return c.currentHypotheses.FindCurrentAssociationSubject(observationID)
}

// ListOpenEvidenceForChain returns active evidence dossiers in stable order.
func (c *Coordinator) ListOpenEvidenceForChain(chainID chains.ChainID) ([]hypotheses.Snapshot, error) {
	if c == nil {
		return nil, ErrCoordinatorClosed
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.state == StateClosed {
		return nil, ErrCoordinatorClosed
	}
	if c.currentHypotheses == nil {
		return nil, ErrCoordinatorNotReady
	}
	return c.currentHypotheses.ListOpenEvidenceForChain(chainID)
}

// AddChain prepares and durably records a chain before publishing it.
func (c *Coordinator) AddChain(ctx context.Context, chain *chains.Chain, actor, correlationID string, recordedAt time.Time) (MutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return MutationResult{}, err
	}
	if c == nil {
		return MutationResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return MutationResult{}, err
	}
	return c.addChainLocked(ctx, chain, actor, correlationID, recordedAt)
}

func (c *Coordinator) addChainLocked(ctx context.Context, chain *chains.Chain, actor, correlationID string, recordedAt time.Time) (MutationResult, error) {
	if err := validateMutationInput(actor, correlationID, recordedAt); err != nil {
		return MutationResult{}, coordinatorError("chain.added", "validate", "", err)
	}
	if chain == nil {
		return MutationResult{}, coordinatorError("chain.added", "prepare", "", fmt.Errorf("%w: chain is nil", ErrMutationPrepareFailed))
	}
	id := chain.Snapshot().ID
	candidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return MutationResult{}, coordinatorError("chain.added", "clone", string(id), err)
	}
	if err := candidate.Add(chain); err != nil {
		return MutationResult{}, coordinatorError("chain.added", "prepare", string(id), fmt.Errorf("%w: %v", ErrMutationPrepareFailed, err))
	}
	after, err := candidate.Get(id)
	if err != nil {
		return MutationResult{}, coordinatorError("chain.added", "prepare", string(id), fmt.Errorf("%w: %v", ErrMutationPrepareFailed, err))
	}
	expectation := appendExpectation{
		kind:        journal.RecordKindChainAdded,
		recordedAt:  recordedAt,
		actor:       actor,
		correlation: correlationID,
		chain:       &after,
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendChainAdded(ctx, journal.ChainAddedInput{
		Chain: after, RecordedAt: recordedAt, Actor: actor, CorrelationID: correlationID,
	})
	if err != nil {
		return c.appendErrorLocked("chain.added", string(id), beforeSequence, beforeHash, expectation, after, err)
	}
	result := MutationResult{Kind: MutationChainAdded, ChainID: id, After: after}
	if err := c.acceptAppendLocked("chain.added", string(id), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence = record.Sequence
	result.JournalRecordHash = record.RecordHash
	return c.publishLocked("chain.added", string(id), candidate, result)
}

// ApplyLifecycleProposal prepares and durably records one explicit lifecycle
// transition. The domain registry remains the source of transition logic.
func (c *Coordinator) ApplyLifecycleProposal(ctx context.Context, proposal chains.TransitionProposal, actor, correlationID string, recordedAt time.Time) (MutationResult, error) {
	if err := validateContext(ctx); err != nil {
		return MutationResult{}, err
	}
	if c == nil {
		return MutationResult{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.requireReadyLocked(); err != nil {
		return MutationResult{}, err
	}
	id := proposal.ChainID
	if err := validateMutationInput(actor, correlationID, recordedAt); err != nil {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "validate", string(id), err)
	}
	if proposal.EvaluatedAt.IsZero() || !proposal.EvaluatedAt.Equal(recordedAt) {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "validate", string(id), ErrInvalidTimestamp)
	}
	before, err := c.current.Get(id)
	if err != nil {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "prepare", string(id), fmt.Errorf("%w: %w", ErrMutationPrepareFailed, err))
	}
	candidate, err := cloneRegistry(ctx, c.current)
	if err != nil {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "clone", string(id), err)
	}
	after, err := candidate.ApplyLifecycleProposal(proposal, actor, correlationID)
	if err != nil {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "prepare", string(id), fmt.Errorf("%w: %w", ErrMutationPrepareFailed, err))
	}
	if err := validateTransitionDelta(before, after, actor, correlationID); err != nil {
		return MutationResult{}, coordinatorError("chain.lifecycle_transitioned", "prepare", string(id), err)
	}
	revision := after.History[len(after.History)-1]
	expectation := appendExpectation{
		kind:        journal.RecordKindLifecycleTransitioned,
		recordedAt:  recordedAt,
		actor:       actor,
		correlation: correlationID,
		transition: &journal.LifecycleTransitionPayload{
			ChainID: id, PreviousRevision: before.Revision, NewRevision: after.Revision,
			From: before.Status, To: after.Status, Revision: revision,
		},
	}
	beforeSequence, beforeHash := c.lastSequence, c.lastHash
	record, err := c.journal.AppendLifecycleTransition(ctx, journal.LifecycleTransitionInput{
		ChainID: id, PreviousRevision: before.Revision, NewRevision: after.Revision,
		From: before.Status, To: after.Status, Revision: revision,
		RecordedAt: recordedAt, Actor: actor, CorrelationID: correlationID,
	})
	if err != nil {
		return c.appendErrorLocked("chain.lifecycle_transitioned", string(id), beforeSequence, beforeHash, expectation, after, err)
	}
	result := MutationResult{
		Kind: MutationLifecycleTransitioned, ChainID: id,
		Before: snapshotPointer(before), After: after,
	}
	if err := c.acceptAppendLocked("chain.lifecycle_transitioned", string(id), expectation, record, beforeSequence, beforeHash); err != nil {
		return result, err
	}
	result.JournalSequence = record.Sequence
	result.JournalRecordHash = record.RecordHash
	return c.publishLocked("chain.lifecycle_transitioned", string(id), candidate, result)
}

// RecoverFromJournal explicitly replaces the current state with a complete
// journal-only replay. No recovery is started automatically.
func (c *Coordinator) RecoverFromJournal(ctx context.Context) (RecoveryMetadata, error) {
	if err := validateContext(ctx); err != nil {
		return RecoveryMetadata{}, err
	}
	if c == nil {
		return RecoveryMetadata{}, ErrCoordinatorClosed
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == StateClosed {
		return RecoveryMetadata{}, ErrCoordinatorClosed
	}
	journalSnapshot, err := c.journal.ReadAll(ctx)
	if err != nil {
		c.state = StateDegraded
		c.degradedReason = "recovery_failed"
		return RecoveryMetadata{}, coordinatorError("recover", "recover", "", fmt.Errorf("%w: %w", ErrRecoveryFailed, err))
	}
	rebuilt, replayMetadata, err := replay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		c.state = StateDegraded
		c.degradedReason = "recovery_failed"
		return RecoveryMetadata{}, coordinatorError("recover", "recover", "", fmt.Errorf("%w: %w", ErrRecoveryFailed, err))
	}
	rebuiltHypotheses, hypothesisMetadata, err := hypothesisreplay.FromJournal(ctx, journalSnapshot)
	if err != nil {
		c.state = StateDegraded
		c.degradedReason = "recovery_failed"
		return RecoveryMetadata{}, coordinatorError("recover", "recover", "", fmt.Errorf("%w: %w", ErrRecoveryFailed, err))
	}
	rebuiltRoutines, routineMetadata, err := routinereplay.ReplayRecords(journalSnapshot.Records)
	if err != nil {
		c.state = StateDegraded
		c.degradedReason = "recovery_failed"
		return RecoveryMetadata{}, coordinatorError("recover", "recover", "", fmt.Errorf("%w: %w", ErrRecoveryFailed, err))
	}
	if err := validateReplayHeads(replayMetadata.FinalHeadSequence, replayMetadata.FinalHeadHash, hypothesisMetadata.FinalHeadSequence, hypothesisMetadata.FinalHeadHash, routineMetadata.FinalSequence, routineMetadata.FinalHash); err != nil {
		c.state = StateDegraded
		c.degradedReason = "recovery_failed"
		return RecoveryMetadata{}, coordinatorError("recover", "recover", "", fmt.Errorf("%w: %w", ErrRecoveryFailed, err))
	}
	c.current = rebuilt
	c.currentHypotheses = rebuiltHypotheses
	c.currentRoutines = rebuiltRoutines
	c.lastSequence = journalSnapshot.HeadSequence
	c.lastHash = journalSnapshot.HeadHash
	c.state = StateReady
	c.degradedReason = ""
	return recoveryMetadata(replayMetadata, hypothesisMetadata, routineMetadata, c), nil
}

func (c *Coordinator) requireReadyLocked() error {
	switch c.state {
	case StateReady:
		return nil
	case StateDegraded:
		return ErrCoordinatorDegraded
	case StateClosed:
		return ErrCoordinatorClosed
	default:
		return ErrCoordinatorNotReady
	}
}

type appendExpectation struct {
	kind                 journal.RecordKind
	recordedAt           time.Time
	actor                string
	correlation          string
	chain                *chains.Snapshot
	transition           *journal.LifecycleTransitionPayload
	observation          *journal.ObservationAddedPayload
	contribution         *journal.ContributionAddedPayload
	checkpoint           *journal.SnapshotCheckpointPayload
	hypothesisOpened     *journal.HypothesisOpenedPayload
	hypothesisStatus     *journal.HypothesisStatusChangedPayload
	hypothesisRebased    *journal.HypothesisRebasedPayload
	hypothesisSuperseded *journal.HypothesisSupersededPayload
	hypothesisResolved   *journal.HypothesisResolvedPayload
	routineCreated       *journal.RoutineCreatedPayload
	routineOccurrence    *journal.RoutineOccurrenceAddedPayload
	routineStatus        *journal.RoutineStatusChangedPayload
}

func (c *Coordinator) acceptAppendLocked(operation, id string, expectation appendExpectation, record journal.Record, beforeSequence uint64, beforeHash string) error {
	if record.Sequence != beforeSequence+1 || record.PreviousHash != beforeHash || !recordMatches(record, expectation) {
		c.lastSequence = record.Sequence
		c.lastHash = record.RecordHash
		c.state = StateDegraded
		c.degradedReason = "journal_registry_divergence"
		return coordinatorError(operation, "append", id, ErrJournalRegistryDivergence)
	}
	c.lastSequence = record.Sequence
	c.lastHash = record.RecordHash
	return nil
}

func (c *Coordinator) appendErrorLocked(operation, id string, beforeSequence uint64, beforeHash string, expectation appendExpectation, after chains.Snapshot, cause error) (MutationResult, error) {
	observed, failure := c.classifyAppendErrorLocked(beforeSequence, beforeHash, expectation, cause)
	result := MutationResult{Kind: mutationKind(operation), ChainID: chains.ChainID(id), After: after}
	if failure.Outcome == AppendUncertain && observed.RecordHash != "" {
		result.JournalSequence = observed.Sequence
		result.JournalRecordHash = observed.RecordHash
	}
	return result, coordinatorError(operation, "append", id, failure)
}

func (c *Coordinator) classifyAppendErrorLocked(beforeSequence uint64, beforeHash string, expectation appendExpectation, cause error) (journal.Record, AppendFailure) {
	observed, readErr := c.journal.ReadAll(context.Background())
	if readErr == nil && observed.HeadSequence == beforeSequence && observed.HeadHash == beforeHash {
		return journal.Record{}, AppendFailure{Outcome: AppendRejected, Err: cause}
	}
	if readErr == nil && observed.HeadSequence == beforeSequence+1 && len(observed.Records) > 0 && recordMatches(observed.Records[len(observed.Records)-1], expectation) {
		last := observed.Records[len(observed.Records)-1]
		c.lastSequence = observed.HeadSequence
		c.lastHash = observed.HeadHash
		c.state = StateDegraded
		c.degradedReason = "journal_append_ambiguous"
		return last, AppendFailure{Outcome: AppendUncertain, Err: cause}
	}
	if readErr == nil {
		c.lastSequence = observed.HeadSequence
		c.lastHash = observed.HeadHash
	}
	c.state = StateDegraded
	c.degradedReason = "journal_head_unavailable"
	return journal.Record{}, AppendFailure{Outcome: AppendUncertain, Err: errors.Join(cause, ErrJournalRegistryDivergence)}
}

func (c *Coordinator) publishLocked(operation, id string, candidate *registry.Registry, result MutationResult) (MutationResult, error) {
	if c.publishHook != nil {
		if err := c.publishHook(); err != nil {
			c.state = StateDegraded
			c.degradedReason = "publication_failed"
			return result, coordinatorError(operation, "publish", id, fmt.Errorf("%w: %v", ErrPublicationFailed, err))
		}
	}
	c.current = candidate
	result.Published = true
	return result, nil
}

func cloneRegistry(ctx context.Context, source *registry.Registry) (*registry.Registry, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: source registry is nil", ErrRegistryCloneFailed)
	}
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	if err := validateContext(ctx); err != nil {
		return nil, err
	}
	candidate, err := source.CloneShallow()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRegistryCloneFailed, err)
	}
	return candidate, nil
}

func validateTransitionDelta(before, after chains.Snapshot, actor, correlationID string) error {
	if len(after.History) != len(before.History)+1 {
		return ErrJournalRegistryDivergence
	}
	if after.Revision != before.Revision+1 || after.Status == before.Status {
		return ErrJournalRegistryDivergence
	}
	revision := after.History[len(after.History)-1]
	if revision.ChainID != after.ID || revision.PreviousRevision != before.Revision || revision.NewRevision != after.Revision || revision.PreviousStatus != before.Status || revision.NewStatus != after.Status || revision.Actor != actor || revision.CorrelationID != correlationID {
		return ErrJournalRegistryDivergence
	}
	if err := afterChainValidate(after); err != nil {
		return err
	}
	return nil
}

func afterChainValidate(snapshot chains.Snapshot) error {
	chain, err := chains.Restore(snapshot)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMutationPrepareFailed, err)
	}
	if err := chain.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrMutationPrepareFailed, err)
	}
	return nil
}

func recordMatches(record journal.Record, expectation appendExpectation) bool {
	if record.Kind != expectation.kind || !record.RecordedAt.Equal(expectation.recordedAt) || record.Actor != expectation.actor || record.CorrelationID != expectation.correlation {
		return false
	}
	switch expectation.kind {
	case journal.RecordKindChainAdded:
		if expectation.chain == nil {
			return false
		}
		var payload journal.ChainAddedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload.Chain, *expectation.chain)
	case journal.RecordKindLifecycleTransitioned:
		if expectation.transition == nil {
			return false
		}
		var payload journal.LifecycleTransitionPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.transition)
	case journal.RecordKindObservationAdded:
		if expectation.observation == nil {
			return false
		}
		var payload journal.ObservationAddedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.observation)
	case journal.RecordKindContributionAdded:
		if expectation.contribution == nil {
			return false
		}
		var payload journal.ContributionAddedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.contribution)
	case journal.RecordKindSnapshotCheckpointed:
		if expectation.checkpoint == nil {
			return false
		}
		var payload journal.SnapshotCheckpointPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.checkpoint)
	case journal.RecordKindHypothesisOpened:
		if expectation.hypothesisOpened == nil {
			return false
		}
		var payload journal.HypothesisOpenedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.hypothesisOpened)
	case journal.RecordKindHypothesisStatusChanged:
		if expectation.hypothesisStatus == nil {
			return false
		}
		var payload journal.HypothesisStatusChangedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.hypothesisStatus)
	case journal.RecordKindHypothesisRebased:
		if expectation.hypothesisRebased == nil {
			return false
		}
		var payload journal.HypothesisRebasedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.hypothesisRebased)
	case journal.RecordKindHypothesisSuperseded:
		if expectation.hypothesisSuperseded == nil {
			return false
		}
		var payload journal.HypothesisSupersededPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.hypothesisSuperseded)
	case journal.RecordKindHypothesisResolved:
		if expectation.hypothesisResolved == nil {
			return false
		}
		var payload journal.HypothesisResolvedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return reflect.DeepEqual(payload, *expectation.hypothesisResolved)
	case journal.RecordKindRoutineCreated:
		if expectation.routineCreated == nil {
			return false
		}
		var payload journal.RoutineCreatedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return payloadJSONEqual(payload, *expectation.routineCreated)
	case journal.RecordKindRoutineOccurrenceAdded:
		if expectation.routineOccurrence == nil {
			return false
		}
		var payload journal.RoutineOccurrenceAddedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return payloadJSONEqual(payload, *expectation.routineOccurrence)
	case journal.RecordKindRoutineStatusChanged:
		if expectation.routineStatus == nil {
			return false
		}
		var payload journal.RoutineStatusChangedPayload
		if json.Unmarshal(record.Payload, &payload) != nil {
			return false
		}
		return payloadJSONEqual(payload, *expectation.routineStatus)
	default:
		return false
	}
}

// payloadJSONEqual compares the canonical wire representation. It avoids
// false divergence for decoded timestamps whose location metadata is
// semantically equal but not pointer-identical in memory.
func payloadJSONEqual(left, right any) bool {
	leftBytes, leftErr := json.Marshal(left)
	rightBytes, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftBytes) == string(rightBytes)
}

func snapshotPointer(snapshot chains.Snapshot) *chains.Snapshot {
	copy := snapshot
	return &copy
}

func mutationKind(operation string) MutationKind {
	if operation == "chain.observation_added" {
		return MutationObservationAdded
	}
	if operation == "chain.contribution_added" {
		return MutationContributionAdded
	}
	if operation == "chain.lifecycle_transitioned" {
		return MutationLifecycleTransitioned
	}
	return MutationChainAdded
}

func coordinatorError(operation, step, id string, err error) error {
	return CoordinatorError{Operation: operation, Step: step, ChainID: id, Err: err}
}

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	return nil
}

func validateMutationInput(actor, correlationID string, recordedAt time.Time) error {
	if strings.TrimSpace(actor) == "" || actor != strings.TrimSpace(actor) || len([]rune(actor)) > maxActorLength || strings.ContainsAny(actor, "\r\n") {
		return ErrInvalidActor
	}
	if strings.TrimSpace(correlationID) == "" || correlationID != strings.TrimSpace(correlationID) || len([]rune(correlationID)) > maxCorrelationLength || strings.ContainsAny(correlationID, "\r\n") {
		return ErrInvalidCorrelation
	}
	if !validRecordedAt(recordedAt) {
		return ErrInvalidTimestamp
	}
	return nil
}
