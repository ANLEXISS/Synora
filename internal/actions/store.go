package actions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"synora/pkg/contract"
)

const MaxStoredResults = 200

// ResultStore is the durable boundary between action execution and message
// delivery. A stored result makes a redelivered request observable without
// executing its effect again.
type ResultStore interface {
	Lookup(idempotencyKey string) (contract.ActionResult, bool, error)
	Save(idempotencyKey string, result contract.ActionResult) error
}

type MemoryResultStore struct {
	mu      sync.Mutex
	results map[string]contract.ActionResult
}

func NewMemoryResultStore() *MemoryResultStore {
	return &MemoryResultStore{results: make(map[string]contract.ActionResult)}
}

func (s *MemoryResultStore) Lookup(key string) (contract.ActionResult, bool, error) {
	if s == nil {
		return contract.ActionResult{}, false, errors.New("nil action result store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return cloneResult(result), ok, nil
}

func (s *MemoryResultStore) Save(key string, result contract.ActionResult) error {
	if s == nil {
		return errors.New("nil action result store")
	}
	if err := validateStoredResult(key, result); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[key] = cloneResult(result)
	trimResults(s.results, MaxStoredResults)
	return nil
}

type FileResultStore struct {
	mu      sync.Mutex
	path    string
	results map[string]contract.ActionResult
}

type storedResult struct {
	IdempotencyKey string                `json:"idempotency_key"`
	Result         contract.ActionResult `json:"result"`
}

type storedResultFile struct {
	Version int            `json:"version"`
	Results []storedResult `json:"results"`
}

func OpenFileResultStore(path string) (*FileResultStore, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("action result store path must be a clean absolute path")
	}
	s := &FileResultStore{path: path, results: make(map[string]contract.ActionResult)}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read action result store: %w", err)
	}
	var file storedResultFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode action result store: %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("unsupported action result store version %d", file.Version)
	}
	for _, item := range file.Results {
		if err := validateStoredResult(item.IdempotencyKey, item.Result); err != nil {
			return nil, fmt.Errorf("invalid action result store entry: %w", err)
		}
		s.results[item.IdempotencyKey] = cloneResult(item.Result)
	}
	trimResults(s.results, MaxStoredResults)
	return s, nil
}

func (s *FileResultStore) Lookup(key string) (contract.ActionResult, bool, error) {
	if s == nil {
		return contract.ActionResult{}, false, errors.New("nil action result store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.results[key]
	return cloneResult(result), ok, nil
}

func (s *FileResultStore) Save(key string, result contract.ActionResult) error {
	if s == nil {
		return errors.New("nil action result store")
	}
	if err := validateStoredResult(key, result); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[key] = cloneResult(result)
	trimResults(s.results, MaxStoredResults)
	return s.flushLocked()
}

func (s *FileResultStore) flushLocked() error {
	items := make([]storedResult, 0, len(s.results))
	for key, result := range s.results {
		items = append(items, storedResult{IdempotencyKey: key, Result: cloneResult(result)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return resultTime(items[i].Result).Before(resultTime(items[j].Result))
	})
	data, err := json.Marshal(storedResultFile{Version: 1, Results: items})
	if err != nil {
		return fmt.Errorf("encode action result store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create action result store directory: %w", err)
	}
	_ = os.Chmod(filepath.Dir(s.path), 0o700)
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".action-results-*.tmp")
	if err != nil {
		return fmt.Errorf("create action result store temporary file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect action result store temporary file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write action result store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync action result store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close action result store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("publish action result store: %w", err)
	}
	return nil
}

func validateStoredResult(key string, result contract.ActionResult) error {
	if key == "" {
		return errors.New("action result idempotency key is required")
	}
	if err := contract.ValidateIdentifier("action result idempotency key", key); err != nil {
		return err
	}
	if result.IdempotencyKey != "" && result.IdempotencyKey != key {
		return errors.New("action result idempotency key mismatch")
	}
	if err := result.Validate(); err != nil {
		return err
	}
	if err := ValidateErrorClass(ErrorClass(result.ErrorClass)); err != nil {
		return err
	}
	return nil
}

func trimResults(results map[string]contract.ActionResult, limit int) {
	for len(results) > limit {
		oldestKey := ""
		var oldest time.Time
		for key, result := range results {
			when := resultTime(result)
			if oldestKey == "" || when.Before(oldest) {
				oldestKey, oldest = key, when
			}
		}
		delete(results, oldestKey)
	}
}

func resultTime(result contract.ActionResult) time.Time {
	for _, value := range []time.Time{result.FinishedAt, result.StartedAt, result.Timestamp} {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func cloneResult(result contract.ActionResult) contract.ActionResult {
	result.Data = cloneMap(result.Data)
	result.Details = cloneMap(result.Details)
	return result
}
