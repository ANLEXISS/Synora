package rpc

import (
	"encoding/json"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

func (s *Server) securityMode(_ contract.Message) (any, error) {
	if s == nil || s.state == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "security state unavailable")
	}
	return s.state.SystemState().Security, nil
}

func (s *Server) securityModeUpdate(msg contract.Message) (any, error) {
	var request contract.SecurityModeRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid security mode payload")
	}
	return s.updateSecurityMode(request)
}

func (s *Server) securityArm(msg contract.Message) (any, error) {
	var request contract.SecurityArmRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid security arm payload")
	}
	if request.Mode == contract.SecurityModeHome {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "armed mode must be night, away or high_security")
	}
	return s.updateSecurityMode(request)
}

func (s *Server) securityDisarm(msg contract.Message) (any, error) {
	var request contract.SecurityDisarmRequest
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &request); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid security disarm payload")
		}
	}
	return s.updateSecurityMode(contract.SecurityModeRequest{Mode: contract.SecurityModeHome, Reason: request.Reason, SetBy: request.SetBy, Source: request.Source})
}

func (s *Server) updateSecurityMode(request contract.SecurityModeRequest) (contract.SecurityModeState, error) {
	if s == nil || s.state == nil {
		return contract.SecurityModeState{}, contract.NewAPIError(contract.ErrorInternal, "security state unavailable")
	}
	mode := contract.SecurityMode(strings.ToLower(strings.TrimSpace(string(request.Mode))))
	switch mode {
	case contract.SecurityModeHome, contract.SecurityModeNight, contract.SecurityModeAway, contract.SecurityModeHighSecurity:
	default:
		return contract.SecurityModeState{}, contract.NewAPIError(contract.ErrorInvalidRequest, "mode must be home, night, away or high_security")
	}
	if request.DurationSeconds < 0 {
		return contract.SecurityModeState{}, contract.NewAPIError(contract.ErrorInvalidRequest, "duration_seconds must be positive")
	}
	now := time.Now().UTC()
	old := s.state.SystemState().Security
	next := contract.SecurityModeState{
		Mode: mode, SetBy: strings.TrimSpace(request.SetBy), Reason: strings.TrimSpace(request.Reason),
		Since: now, Source: strings.TrimSpace(request.Source),
	}
	if next.SetBy == "" {
		next.SetBy = "admin"
	}
	if next.Source == "" {
		next.Source = "manual"
	}
	if next.Reason == "" {
		next.Reason = "manual security mode change"
	}
	if request.DurationSeconds > 0 {
		expires := now.Add(time.Duration(request.DurationSeconds) * time.Second)
		next.ExpiresAt = &expires
	}
	next = contract.NormalizeSecurityModeState(next, now)
	current := s.state.SystemState()
	current.Security = next
	current.Armed = next.Armed
	s.state.SetSystemState(current)
	if err := s.state.SaveNow(); err != nil {
		return contract.SecurityModeState{}, err
	}
	payload := map[string]any{
		"old_mode": old.Mode, "new_mode": next.Mode, "armed": next.Armed,
		"expected_occupancy": next.ExpectedOccupancy, "source": next.Source,
		"reason": next.Reason, "security": next, "event_type": contract.EventSecurityModeChanged,
	}
	if s.publishEvent != nil {
		s.publishEvent(contract.EventSecurityModeChanged, payload, contract.PriorityHigh)
	}
	if s.ingestEvent != nil {
		s.ingestEvent(&contract.Event{ID: idgen.New("evt"), Type: contract.EventSecurityModeChanged, Source: next.Source, Timestamp: now, Payload: payload, Priority: contract.PriorityHigh})
	}
	return next, nil
}
