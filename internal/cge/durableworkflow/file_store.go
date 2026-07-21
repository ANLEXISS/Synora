package durableworkflow

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type FileStore struct {
	mu     sync.Mutex
	dir    string
	wal    *os.File
	policy Policy
	closed bool
}

func OpenFileStore(directory string, policy Policy) (*FileStore, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if directory == "" {
		return nil, ErrInvalidPolicy
	}
	if err := os.MkdirAll(directory, os.FileMode(policy.DirectoryMode)); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, os.FileMode(policy.DirectoryMode)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "workflow.wal"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, os.FileMode(policy.FileMode))
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(os.FileMode(policy.FileMode)); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &FileStore{dir: directory, wal: file, policy: policy}, nil
}

// NewFileStore is an explicit constructor alias for callers that use the
// repository's New-style constructors.
func NewFileStore(directory string, policy Policy) (*FileStore, error) {
	return OpenFileStore(directory, policy)
}

func (s *FileStore) Append(record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.wal == nil {
		return ErrStoreClosed
	}
	encoded, err := EncodeRecord(record, s.policy.MaxRecordBytes)
	if err != nil {
		return err
	}
	if _, err := s.wal.Write(encoded); err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	return nil
}

func (s *FileStore) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.wal == nil {
		return ErrStoreClosed
	}
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	return nil
}

func (s *FileStore) Load() (RecoveryInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return RecoveryInput{}, ErrStoreClosed
	}
	input := RecoveryInput{}
	file, err := os.Open(filepath.Join(s.dir, "workflow.wal"))
	if os.IsNotExist(err) {
		return input, nil
	}
	if err != nil {
		return RecoveryInput{}, err
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			record, decodeErr := DecodeRecord(line, s.policy.MaxRecordBytes)
			if decodeErr != nil {
				if readErr == io.EOF && s.policy.AllowTruncatedFinalRecord {
					input.TruncatedFinalRecord = true
					input.Warnings = append(input.Warnings, "final_record_truncated")
					break
				}
				if readErr == io.EOF {
					return RecoveryInput{}, fmt.Errorf("%w: %v", ErrTruncatedRecord, decodeErr)
				}
				return RecoveryInput{}, fmt.Errorf("%w: %v", ErrMidLogCorruption, decodeErr)
			}
			input.Records = append(input.Records, record)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return RecoveryInput{}, readErr
		}
	}
	checkpointData, checkpointErr := os.ReadFile(filepath.Join(s.dir, "workflow.checkpoint.json"))
	if os.IsNotExist(checkpointErr) {
		return input, nil
	}
	if checkpointErr != nil {
		input.CheckpointError = checkpointErr
		input.Warnings = append(input.Warnings, "checkpoint_read_failed")
		return input, nil
	}
	checkpoint, checkpointErr := decodeCheckpoint(checkpointData, s.policy.MaxCheckpointBytes)
	if checkpointErr != nil {
		input.CheckpointError = checkpointErr
		input.Warnings = append(input.Warnings, "checkpoint_corrupt")
		return input, nil
	}
	input.Checkpoint = &checkpoint
	return input, nil
}

func (s *FileStore) WriteCheckpoint(checkpoint Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrStoreClosed
	}
	encoded, err := encodeCheckpoint(checkpoint, s.policy.MaxCheckpointBytes)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(s.dir, "workflow.checkpoint-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(os.FileMode(s.policy.FileMode)); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	if err := os.Rename(temporaryName, filepath.Join(s.dir, "workflow.checkpoint.json")); err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	directory, err := os.Open(s.dir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	err = directory.Sync()
	_ = directory.Close()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommitNotDurable, err)
	}
	return nil
}

func (s *FileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.wal == nil {
		return nil
	}
	err := s.wal.Close()
	s.wal = nil
	return err
}

// WALSize reports the current journal size without exposing the file handle.
// It is an observational boundary for experimental quota enforcement.
func (s *FileStore) WALSize() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, ErrStoreClosed
	}
	info, err := os.Stat(filepath.Join(s.dir, "workflow.wal"))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
