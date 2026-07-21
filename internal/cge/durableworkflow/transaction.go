package durableworkflow

import (
	"encoding/json"
	"fmt"
	"time"
)

func initialWorkflowState(policy Policy) WorkflowState {
	state := WorkflowState{SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint()}
	state.Digest = WorkflowStateFingerprint(state)
	return state
}

func Open(store Store, policy Policy) (*Coordinator, error) {
	if store == nil {
		return nil, ErrInvalidPolicy
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	recovery, err := store.Load()
	if err != nil {
		return nil, err
	}
	if len(recovery.Records) == 0 {
		state := initialWorkflowState(policy)
		genesis := Genesis{SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: policy.Fingerprint(), State: state, StateDigest: state.Digest}
		payload, marshalErr := json.Marshal(genesis)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if err := store.Append(Record{Version: recordVersion, Sequence: 0, Kind: RecordGenesis, Payload: payload}); err != nil {
			return nil, err
		}
		if policy.SyncOnCommit {
			syncStore, ok := store.(SyncStore)
			if !ok {
				return nil, ErrCommitNotDurable
			}
			if err := syncStore.Sync(); err != nil {
				return nil, err
			}
		}
		return &Coordinator{store: store, policy: policy, state: state, transactions: make(map[WorkflowTransactionID]string)}, nil
	}
	replay, err := Replay(recovery, policy)
	if err != nil {
		return nil, err
	}
	coordinator := &Coordinator{store: store, policy: policy, state: replay.State, transactions: make(map[WorkflowTransactionID]string)}
	for _, record := range recovery.Records {
		if record.Kind != RecordTransaction {
			continue
		}
		var transaction WorkflowTransaction
		if err := json.Unmarshal(record.Payload, &transaction); err == nil {
			coordinator.transactions[transaction.ID] = transaction.Fingerprint
		}
		if record.Sequence > coordinator.walSequence {
			coordinator.walSequence = record.Sequence
		}
	}
	return coordinator, nil
}

func (c *Coordinator) Snapshot() WorkflowState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state.Clone()
}

func (c *Coordinator) Plan(mutation WorkflowMutation, id WorkflowTransactionID, sequence uint64, createdAt time.Time) (WorkflowTransaction, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return WorkflowTransaction{}, ErrStoreClosed
	}
	transaction, _, err := PlanTransaction(c.state, mutation, id, sequence, createdAt, c.policy)
	return transaction, err
}

func (c *Coordinator) Commit(transaction WorkflowTransaction) (CommitResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return CommitResult{}, ErrStoreClosed
	}
	if transaction.Fingerprint == "" || transaction.Fingerprint != WorkflowTransactionFingerprint(transaction) {
		return CommitResult{}, ErrFingerprintMismatch
	}
	if existing, ok := c.transactions[transaction.ID]; ok {
		if existing != transaction.Fingerprint {
			return CommitResult{}, ErrTransactionIDCollision
		}
		return CommitResult{Idempotent: true, WorkflowRevision: c.state.Revision, Sequence: c.state.LastSequence, Digest: c.state.Digest}, nil
	}
	if transaction.SourceWorkflowRevision != c.state.Revision {
		return CommitResult{}, ErrSourceRevisionConflict
	}
	if transaction.SourceWorkflowDigest != c.state.Digest {
		return CommitResult{}, ErrSourceDigestConflict
	}
	if transaction.Sequence != c.state.LastSequence+1 {
		return CommitResult{}, ErrSequenceRegression
	}
	_, result, err := PlanTransaction(c.state, transaction.Mutation, transaction.ID, transaction.Sequence, transaction.CreatedAt, c.policy)
	if err != nil {
		return CommitResult{}, err
	}
	if result.Digest != transaction.ResultWorkflowDigest || result.Revision != transaction.ResultWorkflowRevision {
		return CommitResult{}, ErrReplayMismatch
	}
	payload, err := json.Marshal(transaction)
	if err != nil {
		return CommitResult{}, err
	}
	record := Record{Version: recordVersion, Sequence: c.walSequence + 1, Kind: RecordTransaction, Payload: payload}
	if err := c.store.Append(record); err != nil {
		return CommitResult{}, err
	}
	if c.policy.SyncOnCommit {
		syncStore, ok := c.store.(SyncStore)
		if !ok {
			return CommitResult{}, ErrCommitNotDurable
		}
		if err := syncStore.Sync(); err != nil {
			return CommitResult{}, err
		}
	}
	c.state = result
	c.walSequence = record.Sequence
	c.transactions[transaction.ID] = transaction.Fingerprint
	return CommitResult{Applied: true, WorkflowRevision: result.Revision, Sequence: transaction.Sequence, Digest: result.Digest}, nil
}

func (c *Coordinator) Checkpoint() (CheckpointResult, error) { return c.CheckpointAt(time.Time{}) }

func (c *Coordinator) CheckpointAt(createdAt time.Time) (CheckpointResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return CheckpointResult{}, ErrStoreClosed
	}
	checkpoint := Checkpoint{Sequence: c.state.LastSequence, State: c.state.Clone(), SchemaFingerprint: SchemaFingerprint(), PolicyFingerprint: c.policy.Fingerprint(), CreatedAt: createdAt.UTC()}
	checkpoint.Fingerprint = CheckpointFingerprint(checkpoint)
	checkpoint.Checksum = CheckpointChecksum(checkpoint)
	if err := c.store.WriteCheckpoint(checkpoint); err != nil {
		return CheckpointResult{}, err
	}
	payload, err := json.Marshal(CheckpointMarker{Sequence: checkpoint.Sequence, CheckpointFingerprint: checkpoint.Fingerprint})
	if err != nil {
		return CheckpointResult{}, err
	}
	record := Record{Version: recordVersion, Sequence: c.walSequence + 1, Kind: RecordCheckpointMarker, Payload: payload}
	if err := c.store.Append(record); err != nil {
		return CheckpointResult{}, fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	if c.policy.SyncOnCommit {
		syncStore, ok := c.store.(SyncStore)
		if !ok {
			return CheckpointResult{}, ErrCommitNotDurable
		}
		if err := syncStore.Sync(); err != nil {
			return CheckpointResult{}, err
		}
	}
	c.walSequence = record.Sequence
	return CheckpointResult{Written: true, Sequence: checkpoint.Sequence, Fingerprint: checkpoint.Fingerprint}, nil
}

func (c *Coordinator) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.store.Close()
}
