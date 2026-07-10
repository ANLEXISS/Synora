package rpc

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/engine/graph"
	"synora/pkg/contract"
)

// RestoreLearnedBehaviorOverrides reapplies the durable StateStore guidance
// after Core has restored its persisted state. cmd/synora-core therefore stays
// independent from graph and CGE contract internals.
func (s *Server) RestoreLearnedBehaviorOverrides() error {
	provider, err := s.cgeMutator()
	if err != nil {
		return err
	}
	stored := s.state.BehaviorOverridesList()
	ids := make([]string, 0, len(stored))
	for id := range stored {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	overrides := make([]cgecontracts.LearnedBehaviorOverride, 0, len(ids))
	for _, id := range ids {
		var override cgecontracts.LearnedBehaviorOverride
		if err := json.Unmarshal(stored[id], &override); err != nil {
			return contract.NewAPIError(contract.ErrorValidationFailed, "invalid persisted learned behavior override %q", id)
		}
		if strings.TrimSpace(override.BehaviorID) == "" {
			override.BehaviorID = id
		}
		overrides = append(overrides, override)
	}
	return provider.ApplyLearnedBehaviorOverrides(overrides)
}

type cgeMutationProvider interface {
	CriticalSeeds() []cgecontracts.CriticalSeed
	CriticalSeed(string) (cgecontracts.CriticalSeed, bool)
	CreateCriticalSeed(cgecontracts.CriticalSeed, bool) (cgecontracts.CriticalSeed, error)
	PatchCriticalSeed(string, cgecontracts.CriticalSeedPatch) (cgecontracts.CriticalSeed, error)
	DeleteCriticalSeed(string) (cgecontracts.CriticalSeed, error)

	LearnedBehaviors() []cgecontracts.LearnedBehavior
	LearnedBehavior(string) (cgecontracts.LearnedBehavior, bool)
	PatchLearnedBehavior(string, cgecontracts.LearnedBehaviorPatch) (cgecontracts.LearnedBehavior, error)
	ApproveLearnedBehavior(string, *bool) (cgecontracts.LearnedBehavior, error)
	RejectLearnedBehavior(string) (cgecontracts.LearnedBehavior, error)
	DisableLearnedBehavior(string) (cgecontracts.LearnedBehavior, error)
	ResetLearnedBehavior(string) (cgecontracts.LearnedBehavior, error)
	ForgetLearnedBehavior(string) (cgecontracts.LearnedBehavior, error)
	ApplyLearnedBehaviorFeedback(string, string) (cgecontracts.LearnedBehavior, error)
	ExportLearnedBehaviorOverrides() []cgecontracts.LearnedBehaviorOverride
	ApplyLearnedBehaviorOverrides([]cgecontracts.LearnedBehaviorOverride) error
}

type learnedBehaviorActionRequest struct {
	ID     string          `json:"id"`
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data,omitempty"`
}

func (s *Server) cgeMutator() (cgeMutationProvider, error) {
	provider, ok := s.cge.(cgeMutationProvider)
	if !ok || provider == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "cge configuration unavailable")
	}
	return provider, nil
}

func (s *Server) cgeCriticalSeeds(msg contract.Message) (any, error) {
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	var req cgeListRequest
	_ = decodeOptionalPayload(msg.Payload, &req)
	seeds := provider.CriticalSeeds()
	start, end := pageBounds(len(seeds), req.Offset, req.Limit)
	out := make([]map[string]any, 0, end-start)
	for _, seed := range seeds[start:end] {
		out = append(out, criticalSeedView(seed, true))
	}
	return out, nil
}

func (s *Server) cgeCriticalSeed(msg contract.Message) (any, error) {
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	seed, ok := provider.CriticalSeed(strings.TrimSpace(req.ID))
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "critical seed not found")
	}
	return criticalSeedView(seed, false), nil
}

func (s *Server) cgeCriticalSeedCreate(msg contract.Message) (any, error) {
	if err := validateObjectFields(msg.Payload,
		"id", "name", "description", "enabled", "danger_score", "risk_level",
		"expected_state", "sequence", "context", "proposed_actions", "forbidden_actions",
		"requires_validation", "allow_low_score"); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := decodePayload(msg.Payload, &fields); err != nil {
		return nil, err
	}
	allowLowScore := false
	if raw, ok := fields["allow_low_score"]; ok {
		if err := json.Unmarshal(raw, &allowLowScore); err != nil {
			return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "allow_low_score must be a boolean")
		}
		delete(fields, "allow_low_score")
	}
	if _, ok := fields["enabled"]; !ok {
		fields["enabled"] = json.RawMessage("true")
	}
	data, _ := json.Marshal(fields)
	var seed cgecontracts.CriticalSeed
	if err := decodePayload(data, &seed); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	created, err := provider.CreateCriticalSeed(seed, allowLowScore)
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	s.notifyMutation("cge.updated", created.ID)
	return criticalSeedView(created, false), nil
}

func (s *Server) cgeCriticalSeedUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if err := validateObjectFields(req.Data,
		"name", "enabled", "danger_score", "risk_level", "expected_state",
		"proposed_actions", "forbidden_actions", "requires_validation", "allow_low_score"); err != nil {
		return nil, err
	}
	var patch cgecontracts.CriticalSeedPatch
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	updated, err := provider.PatchCriticalSeed(strings.TrimSpace(req.ID), patch)
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	s.notifyMutation("cge.updated", updated.ID)
	return criticalSeedView(updated, false), nil
}

func (s *Server) cgeCriticalSeedDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	deleted, err := provider.DeleteCriticalSeed(strings.TrimSpace(req.ID))
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	s.notifyMutation("cge.updated", deleted.ID)
	return criticalSeedView(deleted, false), nil
}

func (s *Server) cgeLearnedBehaviorUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	if err := validateObjectFields(req.Data, "status", "requires_validation", "proposed_actions",
		"forbidden_actions", "user_notes", "confidence_override", "risk_override", "enabled"); err != nil {
		return nil, err
	}
	var patch cgecontracts.LearnedBehaviorPatch
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	before := provider.ExportLearnedBehaviorOverrides()
	updated, err := provider.PatchLearnedBehavior(strings.TrimSpace(req.ID), patch)
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	if err := s.persistLearnedBehaviorMutation(provider, before, updated.ID); err != nil {
		return nil, err
	}
	s.notifyMutation("cge.updated", updated.ID)
	return learnedBehaviorView(updated), nil
}

func (s *Server) cgeLearnedBehaviorDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	before := provider.ExportLearnedBehaviorOverrides()
	updated, err := provider.ForgetLearnedBehavior(strings.TrimSpace(req.ID))
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	if err := s.persistLearnedBehaviorMutation(provider, before, updated.ID); err != nil {
		return nil, err
	}
	s.notifyMutation("cge.updated", updated.ID)
	return learnedBehaviorView(updated), nil
}

func (s *Server) cgeLearnedBehaviorAction(msg contract.Message) (any, error) {
	var req learnedBehaviorActionRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	provider, err := s.cgeMutator()
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	before := provider.ExportLearnedBehaviorOverrides()
	var updated cgecontracts.LearnedBehavior
	switch action {
	case "approve":
		if len(req.Data) == 0 {
			req.Data = json.RawMessage(`{}`)
		}
		if err := validateObjectFields(req.Data, "requires_validation"); err != nil {
			return nil, err
		}
		var body struct {
			RequiresValidation *bool `json:"requires_validation,omitempty"`
		}
		if err := decodePayload(req.Data, &body); err != nil {
			return nil, err
		}
		updated, err = provider.ApproveLearnedBehavior(id, body.RequiresValidation)
	case "reject":
		updated, err = provider.RejectLearnedBehavior(id)
	case "disable":
		updated, err = provider.DisableLearnedBehavior(id)
	case "reset":
		updated, err = provider.ResetLearnedBehavior(id)
	default:
		return nil, contract.NewAPIError(contract.ErrorNotFound, "learned behavior action not found")
	}
	if err != nil {
		return nil, mapCGEDomainError(err)
	}
	if err := s.persistLearnedBehaviorMutation(provider, before, updated.ID); err != nil {
		return nil, err
	}
	s.notifyMutation("cge.updated", updated.ID)
	return learnedBehaviorView(updated), nil
}

func (s *Server) persistLearnedBehaviorMutation(provider cgeMutationProvider, before []cgecontracts.LearnedBehaviorOverride, id string) error {
	after := provider.ExportLearnedBehaviorOverrides()
	var selected *cgecontracts.LearnedBehaviorOverride
	for i := range after {
		if after[i].BehaviorID == id {
			value := after[i]
			selected = &value
			break
		}
	}
	var err error
	if selected == nil {
		err = s.state.DeleteBehaviorOverride(id)
	} else {
		data, marshalErr := json.Marshal(selected)
		if marshalErr != nil {
			err = marshalErr
		} else {
			err = s.state.SaveBehaviorOverride(id, data)
		}
	}
	if err != nil {
		_ = provider.ApplyLearnedBehaviorOverrides(before)
		return err
	}
	return nil
}

func criticalSeedView(seed cgecontracts.CriticalSeed, compact bool) map[string]any {
	view := map[string]any{
		"id": seed.ID, "name": seed.Name, "enabled": seed.Enabled,
		"danger_score": seed.DangerScore, "risk_level": seed.RiskLevel,
		"expected_state": seed.ExpectedState, "requires_validation": seed.RequiresValidation,
		"version": seed.Version, "created_at": seed.CreatedAt, "updated_at": seed.UpdatedAt,
		"deleted_at": seed.DeletedAt,
	}
	if compact {
		view["sequence_length"] = len(seed.Sequence)
		view["proposed_action_count"] = len(seed.ProposedActions)
		view["forbidden_action_count"] = len(seed.ForbiddenActions)
		return view
	}
	view["description"] = seed.Description
	view["sequence"] = seed.Sequence
	view["context"] = sanitizeConfigurationMap(seed.Context)
	view["proposed_actions"] = append([]string(nil), seed.ProposedActions...)
	view["forbidden_actions"] = append([]string(nil), seed.ForbiddenActions...)
	return view
}

func learnedBehaviorView(item cgecontracts.LearnedBehavior) map[string]any {
	data, _ := json.Marshal(item)
	var view map[string]any
	_ = json.Unmarshal(data, &view)
	return sanitizeConfigurationMap(view)
}

func mapCGEDomainError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *contract.APIError
	if errors.As(err, &apiErr) {
		return err
	}
	text := strings.ToLower(err.Error())
	switch {
	case errors.Is(err, graph.ErrForbiddenAction), strings.Contains(text, "forbidden action"):
		return contract.NewAPIError(contract.ErrorForbiddenAction, "%s", err.Error())
	case errors.Is(err, graph.ErrLearnedBehaviorNotFound), strings.Contains(text, "not found"):
		return contract.NewAPIError(contract.ErrorNotFound, "%s", err.Error())
	case strings.Contains(text, "duplicate"):
		return contract.NewAPIError(contract.ErrorDuplicateID, "%s", err.Error())
	default:
		return contract.NewAPIError(contract.ErrorValidationFailed, "%s", err.Error())
	}
}

func pageBounds(length int, offset int, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > cgeListLimitMax {
		limit = cgeListLimitMax
	}
	if offset > length {
		offset = length
	}
	end := offset + limit
	if end > length {
		end = length
	}
	return offset, end
}
