package calibrationledger

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

func (s *FileStore) Recover(ctx context.Context) (RecoveryResult, error) {
	if s == nil {
		return RecoveryResult{}, ErrStoreClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.file == nil {
		return RecoveryResult{}, ErrStoreClosed
	}
	if err := contextErr(ctx); err != nil {
		return RecoveryResult{}, err
	}
	return s.recoverLocked(ctx)
}

func (s *FileStore) recoverLocked(ctx context.Context) (RecoveryResult, error) {
	info, err := s.file.Stat()
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("%w: %v", ErrRecoveryFailed, err)
	}
	if info.Size() > s.policy.MaxLedgerBytes {
		return RecoveryResult{}, ErrLedgerLimitReached
	}
	readFile, err := os.Open(s.path)
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("%w: %v", ErrRecoveryFailed, err)
	}
	defer readFile.Close()
	working := newIndex()
	reader := bufio.NewReader(readFile)
	var offset, lastValid int64
	expected := uint64(1)
	previous := genesisHash(s.policy)
	repaired := false
	for {
		if err := contextErr(ctx); err != nil {
			return RecoveryResult{}, err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		offset += int64(len(line))
		if len(line) > s.policy.MaxRecordBytes {
			return RecoveryResult{}, ErrRecordTooLarge
		}
		if len(line) == 0 {
			if readErr != nil {
				return RecoveryResult{}, fmt.Errorf("%w: %v", ErrRecoveryFailed, readErr)
			}
			continue
		}
		if line[len(line)-1] != '\n' {
			if readErr == io.EOF {
				if !s.policy.RepairTrailingRecord {
					return RecoveryResult{}, fmt.Errorf("%w: %w", ErrTrailingRecordTruncated, ErrRepairDisabled)
				}
				if err := s.file.Truncate(lastValid); err != nil {
					return RecoveryResult{}, fmt.Errorf("%w: %v", ErrRepairFailed, err)
				}
				if s.policy.Fsync {
					if err := s.file.Sync(); err != nil {
						return RecoveryResult{}, fmt.Errorf("%w: %v", ErrRepairFailed, err)
					}
				}
				repaired = true
				break
			}
			return RecoveryResult{}, fmt.Errorf("%w: read", ErrMidFileCorruption)
		}
		var envelope JournalEnvelope
		if err := json.Unmarshal(line[:len(line)-1], &envelope); err != nil {
			return RecoveryResult{}, fmt.Errorf("%w: %w", ErrMidFileCorruption, ErrInvalidEnvelope)
		}
		if envelope.SchemaVersion == "" || envelope.Record.SchemaVersion == "" && envelope.SchemaVersion == EnvelopeSchemaVersion {
			return RecoveryResult{}, fmt.Errorf("%w: %w", ErrMidFileCorruption, ErrInvalidEnvelope)
		}
		if envelope.SchemaVersion != EnvelopeSchemaVersion || envelope.Record.SchemaVersion != RecordSchemaVersion {
			return RecoveryResult{}, ErrUnsupportedSchema
		}
		if envelope.Sequence < expected {
			if envelope.Sequence == expected-1 {
				return RecoveryResult{}, ErrDuplicateSequence
			}
			return RecoveryResult{}, ErrInvalidSequence
		}
		if envelope.Sequence > expected {
			return RecoveryResult{}, ErrSequenceGap
		}
		if envelope.Record.Sequence != envelope.Sequence {
			return RecoveryResult{}, ErrInvalidSequence
		}
		if envelope.PreviousEnvelopeHash != previous {
			if len(working.records) == 0 {
				return RecoveryResult{}, ErrInvalidGenesis
			}
			return RecoveryResult{}, ErrHashChainMismatch
		}
		if envelope.Record.RecordFingerprint != recordFingerprint(envelope.Record) {
			return RecoveryResult{}, ErrRecordFingerprintMismatch
		}
		if err := envelope.Record.Validate(s.policy); err != nil {
			return RecoveryResult{}, err
		}
		if envelope.RecordHash != recordHash(envelope.Record) {
			return RecoveryResult{}, ErrEnvelopeFingerprintMismatch
		}
		if envelope.EnvelopeHash != envelopeFingerprint(envelope) {
			return RecoveryResult{}, ErrEnvelopeFingerprintMismatch
		}
		if len(working.records) > 0 && envelope.Record.PreviousRecordFingerprint != working.records[len(working.records)-1].RecordFingerprint {
			return RecoveryResult{}, ErrHashChainMismatch
		}
		if len(working.records) == 0 && envelope.Record.PreviousRecordFingerprint != "" {
			return RecoveryResult{}, ErrHashChainMismatch
		}
		if err := working.add(envelope, int64(len(line))); err != nil {
			return RecoveryResult{}, err
		}
		lastValid = offset
		expected++
		previous = envelope.EnvelopeHash
		if readErr != nil && readErr != io.EOF {
			return RecoveryResult{}, fmt.Errorf("%w: read", ErrRecoveryFailed)
		}
	}
	working.bytes = lastValid
	s.index = working
	result := RecoveryResult{Completed: true, RepairedTrailingRecord: repaired, RecordCount: uint64(len(working.records)), Bytes: lastValid}
	if len(working.records) > 0 {
		result.LastSequence = working.records[len(working.records)-1].Sequence
		result.LastRecordFingerprint = working.records[len(working.records)-1].RecordFingerprint
		result.LastEnvelopeFingerprint = working.envelopes[len(working.envelopes)-1].EnvelopeHash
	}
	s.lastRecovery = result
	return result, nil
}
