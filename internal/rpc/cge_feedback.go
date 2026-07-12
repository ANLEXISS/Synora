package rpc

import (
	"encoding/json"
	"strings"
	"time"

	"synora/pkg/contract"
)

type cgeProfileEngine interface {
	SetSecurityProfile(*contract.CgeSecurityProfile)
	AddEvaluationFeedback(contract.CgeEvaluationFeedback, *contract.EventChain) error
}

func (s *Server) cgeSecurityProfile(_ contract.Message) (any, error) {
	if s == nil || s.cgeProfile == nil {
		profile := contract.DefaultCgeSecurityProfile()
		return profile, nil
	}
	return s.cgeProfile.Get(), nil
}

func (s *Server) cgeSecurityProfileUpdate(msg contract.Message) (any, error) {
	if s == nil || s.cgeProfile == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "cge security profile unavailable")
	}
	profile, err := s.cgeProfile.Update(msg.Payload)
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "%s", err.Error())
	}
	if configured, ok := s.cge.(interface {
		SetSecurityProfile(*contract.CgeSecurityProfile)
	}); ok {
		configured.SetSecurityProfile(&profile)
	}
	if s.chains != nil {
		s.chains.SetSignificantInactivityTimeout(timeDurationSeconds(profile.SignificantInactivityTimeoutSeconds))
	}
	return profile, nil
}

func (s *Server) cgeFeedbackList(msg contract.Message) (any, error) {
	if s == nil || s.cgeFeedback == nil {
		return []map[string]any{}, nil
	}
	var request struct {
		ChainID string `json:"chain_id"`
	}
	_ = decodeOptionalPayload(msg.Payload, &request)
	return s.cgeFeedback.List(request.ChainID), nil
}

func (s *Server) cgeFeedbackEvaluation(msg contract.Message) (any, error) {
	if s == nil || s.cgeFeedback == nil || s.chains == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "cge feedback unavailable")
	}
	var feedback contract.CgeEvaluationFeedback
	if err := json.Unmarshal(msg.Payload, &feedback); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid evaluation feedback")
	}
	if !validCorrectionType(feedback.CorrectionType) || strings.TrimSpace(feedback.ChainID) == "" || strings.TrimSpace(feedback.EventID) == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "invalid evaluation feedback")
	}
	chain, ok := s.chains.Get(feedback.ChainID)
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "event chain not found")
	}
	found := false
	for _, event := range chain.RecentEvents {
		if event.ID == feedback.EventID {
			found = true
			break
		}
	}
	if !found {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "event not found in chain")
	}
	created, err := s.cgeFeedback.AddEvaluation(feedback)
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "%s", err.Error())
	}
	if configured, ok := s.cge.(cgeProfileEngine); ok {
		_ = configured.AddEvaluationFeedback(created, chain)
	}
	return created, nil
}

func (s *Server) cgeFeedbackChain(msg contract.Message) (any, error) {
	if s == nil || s.cgeFeedback == nil || s.chains == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "cge feedback unavailable")
	}
	var feedback contract.CgeChainFeedback
	if err := json.Unmarshal(msg.Payload, &feedback); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid chain feedback")
	}
	if strings.TrimSpace(feedback.ChainID) == "" {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "chain_id is required")
	}
	if _, ok := s.chains.Get(feedback.ChainID); !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "event chain not found")
	}
	created, err := s.cgeFeedback.AddChain(feedback)
	if err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "%s", err.Error())
	}
	memory, err := s.chains.ApplyChainFeedback(created.ChainID, created)
	if err != nil {
		return nil, err
	}
	return map[string]any{"feedback": created, "critical_chain": memory}, nil
}

func validCorrectionType(value contract.CgeCorrectionType) bool {
	switch value {
	case contract.CgeCorrectionFalsePositive, contract.CgeCorrectionTooLow, contract.CgeCorrectionTooHigh, contract.CgeCorrectionWrongState, contract.CgeCorrectionWrongAction, contract.CgeCorrectionMarkNormal, contract.CgeCorrectionMarkCritical:
		return true
	default:
		return false
	}
}

func timeDurationSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
