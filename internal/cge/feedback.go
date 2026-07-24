package cge

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/cge/contractcatalog"
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
	for index := range s.evaluations {
		s.evaluations[index] = normalizeEvaluation(s.evaluations[index])
	}
	for index := range s.chains {
		s.chains[index] = normalizeChain(s.chains[index])
	}
	return nil
}

func (s *FeedbackStore) AddEvaluation(value contract.CgeEvaluationFeedback) (contract.CgeEvaluationFeedback, error) {
	if s == nil {
		return contract.CgeEvaluationFeedback{}, errors.New("cge feedback unavailable")
	}
	if strings.TrimSpace(value.ChainID) == "" || strings.TrimSpace(value.EventID) == "" {
		return contract.CgeEvaluationFeedback{}, errors.New("chain_id and event_id are required")
	}
	value = normalizeEvaluation(value)
	if !validCorrectionType(value.CorrectionType) {
		return contract.CgeEvaluationFeedback{}, errors.New("invalid correction_type")
	}
	if !validScope(value.Scope) {
		return contract.CgeEvaluationFeedback{}, errors.New("invalid scope")
	}
	if err := validatePreferredActions(value.PreferredActions); err != nil {
		return contract.CgeEvaluationFeedback{}, err
	}
	if err := validateActionDetails(value.PreferredActionDetails, value.BlockedActions); err != nil {
		return contract.CgeEvaluationFeedback{}, err
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
	value = normalizeChain(value)
	if !validCorrectionType(value.CorrectionType) {
		return contract.CgeChainFeedback{}, errors.New("invalid correction_type")
	}
	if !validScope(value.Scope) {
		return contract.CgeChainFeedback{}, errors.New("invalid scope")
	}
	if err := validatePreferredActions(value.PreferredActions); err != nil {
		return contract.CgeChainFeedback{}, err
	}
	if err := validateActionDetails(value.PreferredActionDetails, value.BlockedActions); err != nil {
		return contract.CgeChainFeedback{}, err
	}
	switch value.FinalOutcome {
	case contract.CgeOutcomeNormal, contract.CgeOutcomeFalsePositive, contract.CgeOutcomeRealIncident, contract.CgeOutcomeUncertain:
	case "":
		// New intent-based chain feedback does not require final_outcome.
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

func normalizeEvaluation(value contract.CgeEvaluationFeedback) contract.CgeEvaluationFeedback {
	if value.Scope == "" {
		value.Scope = contract.CgeFeedbackCaseOnly
	}
	value.PreferredActions = append([]string{}, value.PreferredActions...)
	value.PreferredActionDetails = append([]contract.CgePreferredActionSpec{}, value.PreferredActionDetails...)
	value.BlockedActions = append([]contract.CgeBlockedAction{}, value.BlockedActions...)
	for _, action := range value.PreferredActionDetails {
		if strings.TrimSpace(action.Command) != "" && !containsString(value.PreferredActions, action.Command) {
			value.PreferredActions = append(value.PreferredActions, action.Command)
		}
	}
	value.AdminNote = strings.TrimSpace(value.AdminNote)
	if value.AdminNote == "" {
		value.AdminNote = strings.TrimSpace(value.Note)
	}
	if len(value.AdminNote) > 4000 {
		value.AdminNote = value.AdminNote[:4000]
	}
	if value.Note == "" {
		value.Note = value.AdminNote
	}
	return value
}

func normalizeChain(value contract.CgeChainFeedback) contract.CgeChainFeedback {
	legacyOutcome := value.CorrectionType == "" && value.FinalOutcome != ""
	if value.CorrectionType == "" {
		switch value.FinalOutcome {
		case contract.CgeOutcomeFalsePositive:
			value.CorrectionType = contract.CgeCorrectionFalsePositive
		case contract.CgeOutcomeRealIncident:
			value.CorrectionType = contract.CgeCorrectionFalseNegative
		default:
			value.CorrectionType = contract.CgeCorrectionCorrectTuneActions
		}
	}
	if value.Scope == "" {
		if value.ApplyToSimilarFutureChains || legacyOutcome {
			value.Scope = contract.CgeFeedbackApplyToSimilar
		} else {
			value.Scope = contract.CgeFeedbackCaseOnly
		}
	}
	value.PreferredActions = append([]string{}, value.PreferredActions...)
	value.PreferredActionDetails = append([]contract.CgePreferredActionSpec{}, value.PreferredActionDetails...)
	value.BlockedActions = append([]contract.CgeBlockedAction{}, value.BlockedActions...)
	for _, action := range value.PreferredActionDetails {
		if strings.TrimSpace(action.Command) != "" && !containsString(value.PreferredActions, action.Command) {
			value.PreferredActions = append(value.PreferredActions, action.Command)
		}
	}
	value.AdminNote = strings.TrimSpace(value.AdminNote)
	if value.AdminNote == "" {
		value.AdminNote = strings.TrimSpace(value.Note)
	}
	if len(value.AdminNote) > 4000 {
		value.AdminNote = value.AdminNote[:4000]
	}
	if value.Note == "" {
		value.Note = value.AdminNote
	}
	value.ApplyToSimilarFutureChains = value.Scope == contract.CgeFeedbackApplyToSimilar
	return value
}

func validCorrectionType(value contract.CgeCorrectionType) bool {
	switch value {
	case contract.CgeCorrectionFalsePositive, contract.CgeCorrectionFalseNegative,
		contract.CgeCorrectionReactionTooStrong, contract.CgeCorrectionReactionTooWeak,
		contract.CgeCorrectionCorrectTuneActions, contract.CgeCorrectionTooLow,
		contract.CgeCorrectionTooHigh, contract.CgeCorrectionWrongState,
		contract.CgeCorrectionWrongAction, contract.CgeCorrectionMarkNormal,
		contract.CgeCorrectionMarkCritical:
		return true
	default:
		return false
	}
}

func validScope(value contract.CgeFeedbackScope) bool {
	return value == contract.CgeFeedbackCaseOnly || value == contract.CgeFeedbackApplyToSimilar
}

func validatePreferredActions(actions []string) error {
	if len(actions) > 20 {
		return errors.New("preferred_actions cannot contain more than 20 actions")
	}
	for _, action := range actions {
		switch contract.CgePreferredAction(action) {
		case contract.CgeActionObserve, contract.CgeActionNotifyOwner,
			contract.CgeActionNotifyEmergencyContact, contract.CgeActionRecordClip,
			contract.CgeActionLockEvidence, contract.CgeActionCreateAlert,
			contract.CgeActionRequestUserValidation, contract.CgeActionIgnorePattern,
			contract.CgeActionActivateRelatedAutomation:
		case "notify.whatsapp", "record.clip", "mark_intrusion_candidate", "increase_retention", "store_evidence", "siren":
		default:
			return errors.New("invalid preferred_actions entry")
		}
	}
	return nil
}

func validateActionDetails(actions []contract.CgePreferredActionSpec, blocked []contract.CgeBlockedAction) error {
	if len(actions) > 20 {
		return errors.New("preferred_action_details cannot contain more than 20 actions")
	}
	for _, action := range actions {
		if strings.TrimSpace(action.Command) == "" {
			return errors.New("preferred action command is required")
		}
		if len(action.Command) > 128 || len(action.Target) > 128 {
			return errors.New("preferred action is too long")
		}
	}
	if len(blocked) > 20 {
		return errors.New("blocked_actions cannot contain more than 20 actions")
	}
	for _, action := range blocked {
		if strings.TrimSpace(action.Command) == "" || strings.TrimSpace(action.Reason) == "" {
			return errors.New("blocked action command and reason are required")
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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
	if err := contractcatalog.ValidateStoreWrite("synora.store.feedback", "synora.cge.feedback.v1", feedbackFile{Evaluations: s.evaluations, Chains: s.chains}); err != nil {
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
