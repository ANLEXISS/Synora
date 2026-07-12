package cge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type FeedbackStore struct {
	mu          sync.RWMutex
	path        string
	evaluations []contract.CgeEvaluationFeedback
	chains      []contract.CgeChainFeedback
}

type feedbackFile struct {
	Evaluations []contract.CgeEvaluationFeedback `json:"evaluations,omitempty"`
	Chains      []contract.CgeChainFeedback      `json:"chains,omitempty"`
}

func NewFeedbackStore(path string) *FeedbackStore {
	return &FeedbackStore{path: strings.TrimSpace(path), evaluations: []contract.CgeEvaluationFeedback{}, chains: []contract.CgeChainFeedback{}}
}

func (s *FeedbackStore) Load() error {
	if s == nil || s.path == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file feedbackFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	s.evaluations = file.Evaluations
	s.chains = file.Chains
	return nil
}

func (s *FeedbackStore) AddEvaluation(value contract.CgeEvaluationFeedback) (contract.CgeEvaluationFeedback, error) {
	if s == nil {
		return contract.CgeEvaluationFeedback{}, errors.New("cge feedback unavailable")
	}
	if strings.TrimSpace(value.ChainID) == "" || strings.TrimSpace(value.EventID) == "" {
		return contract.CgeEvaluationFeedback{}, errors.New("chain_id and event_id are required")
	}
	value.ID = idgen.New("cge-feedback")
	value.CreatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evaluations = append(s.evaluations, value)
	if err := s.saveLocked(); err != nil {
		return contract.CgeEvaluationFeedback{}, err
	}
	return value, nil
}

func (s *FeedbackStore) AddChain(value contract.CgeChainFeedback) (contract.CgeChainFeedback, error) {
	if s == nil {
		return contract.CgeChainFeedback{}, errors.New("cge feedback unavailable")
	}
	if strings.TrimSpace(value.ChainID) == "" {
		return contract.CgeChainFeedback{}, errors.New("chain_id is required")
	}
	switch value.FinalOutcome {
	case contract.CgeOutcomeNormal, contract.CgeOutcomeFalsePositive, contract.CgeOutcomeRealIncident, contract.CgeOutcomeUncertain:
	default:
		return contract.CgeChainFeedback{}, errors.New("invalid final_outcome")
	}
	value.ID = idgen.New("cge-feedback")
	value.CreatedAt = time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chains = append(s.chains, value)
	if err := s.saveLocked(); err != nil {
		return contract.CgeChainFeedback{}, err
	}
	return value, nil
}

func (s *FeedbackStore) List(chainID string) []map[string]any {
	if s == nil {
		return []map[string]any{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	chainID = strings.TrimSpace(chainID)
	out := make([]map[string]any, 0)
	for _, item := range s.evaluations {
		if chainID == "" || item.ChainID == chainID {
			out = append(out, toMap(item))
		}
	}
	for _, item := range s.chains {
		if chainID == "" || item.ChainID == chainID {
			out = append(out, toMap(item))
		}
	}
	return out
}

func (s *FeedbackStore) saveLocked() error {
	if strings.TrimSpace(s.path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(feedbackFile{Evaluations: s.evaluations, Chains: s.chains}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".cge-feedback-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func toMap(value any) map[string]any {
	data, _ := json.Marshal(value)
	var output map[string]any
	_ = json.Unmarshal(data, &output)
	return output
}
