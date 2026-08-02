package durableworkflow

import (
	"encoding/json"
	"fmt"
)

func Replay(recovery RecoveryInput, policy Policy) (ReplayResult, error) {
	if err := policy.Validate(); err != nil {
		return ReplayResult{}, err
	}
	if err := ValidateLayerGraph(); err != nil {
		return ReplayResult{}, err
	}
	if len(recovery.Records) == 0 {
		return ReplayResult{}, ErrInvalidGenesis
	}
	if recovery.Records[0].Kind != RecordGenesis || recovery.Records[0].Sequence != 0 {
		return ReplayResult{}, ErrInvalidGenesis
	}
	var genesis Genesis
	if err := json.Unmarshal(recovery.Records[0].Payload, &genesis); err != nil {
		return ReplayResult{}, fmt.Errorf("%w: %v", ErrInvalidGenesis, err)
	}
	if genesis.SchemaFingerprint != SchemaFingerprint() || genesis.PolicyFingerprint != policy.Fingerprint() || genesis.StateDigest != genesis.State.Digest || genesis.State.Digest != WorkflowStateFingerprint(genesis.State) {
		return ReplayResult{}, ErrInvalidGenesis
	}
	if err := ValidateWorkflowState(genesis.State); err != nil {
		return ReplayResult{}, fmt.Errorf("%w: genesis state", ErrInvalidGenesis)
	}
	state := genesis.State.Clone()
	report := RecoveryReport{GenesisValidated: true, RecordsRead: len(recovery.Records), TruncatedFinalRecordIgnored: recovery.TruncatedFinalRecord, Warnings: append([]string(nil), recovery.Warnings...)}
	if recovery.Checkpoint != nil {
		report.CheckpointFound = true
		if err := validateCheckpoint(*recovery.Checkpoint, policy); err != nil {
			return ReplayResult{}, err
		}
	}
	if recovery.CheckpointError != nil {
		if len(recovery.Records) <= 1 {
			return ReplayResult{}, fmt.Errorf("%w: no complete wal fallback", ErrCheckpointCorrupt)
		}
		report.CheckpointFallback = true
		report.Warnings = append(report.Warnings, "checkpoint_fallback_to_wal")
	}
	seenTransactions := make(map[WorkflowTransactionID]string)
	for index, record := range recovery.Records[1:] {
		expectedSequence := uint64(index + 1)
		if record.Sequence != expectedSequence {
			if record.Sequence < expectedSequence {
				return ReplayResult{}, ErrSequenceRegression
			}
			return ReplayResult{}, ErrSequenceGap
		}
		switch record.Kind {
		case RecordTransaction:
			var transaction WorkflowTransaction
			if err := json.Unmarshal(record.Payload, &transaction); err != nil {
				return ReplayResult{}, fmt.Errorf("%w: transaction", ErrInvalidTransaction)
			}
			if transaction.Fingerprint != WorkflowTransactionFingerprint(transaction) {
				return ReplayResult{}, ErrFingerprintMismatch
			}
			if previous, ok := seenTransactions[transaction.ID]; ok {
				if previous != transaction.Fingerprint {
					return ReplayResult{}, ErrTransactionIDCollision
				}
				report.DuplicateTransactionsIgnored++
				continue
			}
			seenTransactions[transaction.ID] = transaction.Fingerprint
			if transaction.SchemaFingerprint != SchemaFingerprint() || transaction.PolicyFingerprint != policy.Fingerprint() {
				return ReplayResult{}, ErrPolicyMismatch
			}
			_, resulting, err := PlanTransaction(state, transaction.Mutation, transaction.ID, transaction.Sequence, transaction.CreatedAt, policy)
			if err != nil {
				return ReplayResult{}, fmt.Errorf("%w: %v", ErrReplayMismatch, err)
			}
			if resulting.Digest != transaction.ResultWorkflowDigest || resulting.Revision != transaction.ResultWorkflowRevision {
				return ReplayResult{}, ErrReplayMismatch
			}
			state = resulting
			report.TransactionsApplied++
		case RecordCheckpointMarker:
			var marker CheckpointMarker
			if err := json.Unmarshal(record.Payload, &marker); err != nil {
				return ReplayResult{}, ErrInvalidRecord
			}
			if recovery.Checkpoint != nil && marker.CheckpointFingerprint == recovery.Checkpoint.Fingerprint && marker.Sequence == recovery.Checkpoint.Sequence {
				report.CheckpointUsed = true
				if state.Digest != recovery.Checkpoint.State.Digest {
					return ReplayResult{}, ErrCheckpointCorrupt
				}
			}
		case RecordGenesis:
			return ReplayResult{}, ErrInvalidGenesis
		default:
			return ReplayResult{}, ErrInvalidRecord
		}
	}
	if recovery.Checkpoint != nil && !report.CheckpointUsed {
		return ReplayResult{}, ErrCheckpointCorrupt
	}
	report.FinalSequence = state.LastSequence
	report.FinalRevision = state.Revision
	report.FinalDigest = state.Digest
	return ReplayResult{State: state, Report: report}, nil
}

func validateCheckpoint(checkpoint Checkpoint, policy Policy) error {
	if checkpoint.PolicyFingerprint != policy.Fingerprint() {
		return ErrPolicyMismatch
	}
	if checkpoint.Checksum != CheckpointChecksum(checkpoint) || checkpoint.Fingerprint != CheckpointFingerprint(checkpoint) {
		return ErrCheckpointCorrupt
	}
	if err := ValidateWorkflowState(checkpoint.State); err != nil {
		return ErrCheckpointCorrupt
	}
	if checkpoint.Sequence != checkpoint.State.LastSequence {
		return ErrCheckpointCorrupt
	}
	return nil
}
