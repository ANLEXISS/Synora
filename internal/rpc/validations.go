package rpc

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	cgecontracts "synora/internal/engine/contracts"
	"synora/internal/idgen"
	"synora/internal/state"
	"synora/pkg/contract"
)

var validationTypes = map[string]bool{
	contract.ValidationTypeIdentity:            true,
	contract.ValidationTypeEventClassification: true,
	contract.ValidationTypeBehaviorApproval:    true,
	contract.ValidationTypeActionFeedback:      true,
	contract.ValidationTypeFalsePositive:       true,
	contract.ValidationTypeFalseNegative:       true,
}

var validationStatuses = map[string]bool{
	contract.ValidationStatusPending:   true,
	contract.ValidationStatusAccepted:  true,
	contract.ValidationStatusApproved:  true,
	contract.ValidationStatusRejected:  true,
	contract.ValidationStatusCorrected: true,
	contract.ValidationStatusIgnored:   true,
}

func (s *Server) validationGet(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	item, ok := s.state.Validation(strings.TrimSpace(req.ID))
	if !ok || item == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "validation not found")
	}
	return item, nil
}

func (s *Server) validationCreate(msg contract.Message) (any, error) {
	item := contract.ValidationRequest{Enabled: true}
	if err := decodePayload(msg.Payload, &item); err != nil {
		return nil, err
	}
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = idgen.New("validation")
	}
	if _, exists := s.state.Validation(item.ID); exists {
		return nil, contract.NewAPIError(contract.ErrorDuplicateID, "validation %q already exists", item.ID)
	}
	if err := normalizeUserValidation(&item, true); err != nil {
		return nil, err
	}
	if err := s.applyValidationGuidance(&item); err != nil {
		return nil, err
	}
	s.applyValidationStateCorrection(&item)
	if err := s.state.SaveValidation(&item); err != nil {
		return nil, err
	}
	s.notifyMutation("validation.updated", item.ID)
	return &item, nil
}

func (s *Server) validationUpdate(msg contract.Message) (any, error) {
	var req MutationPayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(req.ID)
	item, ok := s.state.Validation(id)
	if !ok || item == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "validation not found")
	}
	var patch map[string]json.RawMessage
	if err := decodePayload(req.Data, &patch); err != nil {
		return nil, err
	}
	for key, raw := range patch {
		switch key {
		case "status":
			if err := json.Unmarshal(raw, &item.Status); err != nil {
				return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "status must be a string")
			}
		case "correction":
			if string(raw) == "null" {
				item.Correction = nil
			} else if err := json.Unmarshal(raw, &item.Correction); err != nil {
				return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "correction must be an object")
			}
		case "notes":
			if err := json.Unmarshal(raw, &item.Notes); err != nil {
				return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "notes must be a string")
			}
		default:
			return nil, contract.NewAPIError(contract.ErrorValidationFailed, "field %q cannot be modified", key)
		}
	}
	if err := normalizeUserValidation(item, false); err != nil {
		return nil, err
	}
	if err := s.applyValidationGuidance(item); err != nil {
		return nil, err
	}
	s.applyValidationStateCorrection(item)
	if err := s.state.SaveValidation(item); err != nil {
		return nil, err
	}
	s.notifyMutation("validation.updated", item.ID)
	return item, nil
}

func (s *Server) validationDelete(msg contract.Message) (any, error) {
	var req DeletePayload
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	item, ok := s.state.Validation(strings.TrimSpace(req.ID))
	if !ok || item == nil {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "validation not found")
	}
	now := time.Now().UTC()
	item.Enabled = false
	item.DeletedAt = &now
	item.UpdatedAt = now
	if err := s.state.SaveValidation(item); err != nil {
		return nil, err
	}
	s.notifyMutation("validation.updated", item.ID)
	return item, nil
}

func normalizeUserValidation(item *contract.ValidationRequest, creating bool) error {
	item.Type = strings.ToLower(strings.TrimSpace(item.Type))
	item.Status = strings.ToLower(strings.TrimSpace(item.Status))
	if item.Type == "" {
		item.Type = contract.ValidationTypeEventClassification
	}
	if !validationTypes[item.Type] {
		return contract.NewAPIError(contract.ErrorValidationFailed, "unsupported validation type %q", item.Type)
	}
	if item.Status == "" {
		item.Status = contract.ValidationStatusPending
	}
	if !validationStatuses[item.Status] {
		return contract.NewAPIError(contract.ErrorValidationFailed, "unsupported validation status %q", item.Status)
	}
	now := time.Now().UTC()
	if creating && item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Correction == nil {
		item.Correction = map[string]any{}
	}
	if item.Type == contract.ValidationTypeFalsePositive && strings.TrimSpace(item.BehaviorID) == "" {
		item.Correction["exception_recorded"] = true
	}
	return nil
}

func (s *Server) applyValidationStateCorrection(item *contract.ValidationRequest) {
	if s == nil || s.state == nil || item == nil || item.Type != contract.ValidationTypeIdentity {
		return
	}
	switch item.Status {
	case contract.ValidationStatusAccepted, contract.ValidationStatusApproved, contract.ValidationStatusCorrected:
	default:
		return
	}
	identityID := firstString(item.Correction, "identity_id", "resident_id", "proposed_identity")
	if identityID == "" {
		identityID = strings.TrimSpace(item.ProposedIdentity)
	}
	if identityID == "" {
		identityID = strings.TrimSpace(item.ResidentID)
	}
	if identityID == "" {
		return
	}
	now := time.Now().UTC()
	identity, _ := s.state.Identity(identityID)
	if identity == nil {
		identity = &state.IdentityState{ID: identityID, CreatedAt: now}
	}
	identity.State = "present"
	identity.Confidence = 1
	identity.LastNodeID = item.NodeID
	identity.LastSeen = now
	identity.UpdatedAt = now
	s.state.SetIdentity(identity)
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// validationGuidanceProvider is implemented by the CGE facade in Core. Keeping
// it here prevents the HTTP service from importing or manipulating CGE memory.
type validationGuidanceProvider interface {
	ApplyUserValidation(contract.ValidationRequest) error
}

func (s *Server) applyValidationGuidance(item *contract.ValidationRequest) error {
	if item == nil || s.cge == nil {
		return nil
	}
	provider, ok := s.cge.(validationGuidanceProvider)
	if !ok {
		return nil
	}
	mutator, persistable := s.cge.(cgeMutationProvider)
	var before []cgecontracts.LearnedBehaviorOverride
	if persistable {
		before = mutator.ExportLearnedBehaviorOverrides()
	}
	if err := provider.ApplyUserValidation(*item); err != nil {
		return mapCGEDomainError(err)
	}
	if persistable && strings.TrimSpace(item.BehaviorID) != "" {
		if err := s.persistLearnedBehaviorMutation(mutator, before, strings.TrimSpace(item.BehaviorID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) dangerAssessments(msg contract.Message) (any, error) {
	var req cgeListRequest
	_ = decodeOptionalPayload(msg.Payload, &req)
	items := s.snapshot.DangerAssessmentViews()
	limit := req.Limit
	if limit <= 0 || limit > cgeListLimitMax {
		limit = cgeListLimitMax
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []map[string]any{}, nil
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end], nil
}

func (s *Server) dangerAssessment(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	item, ok := s.snapshot.DangerAssessmentView(strings.TrimSpace(req.ID))
	if !ok {
		return nil, contract.NewAPIError(contract.ErrorNotFound, "danger assessment not found")
	}
	return item, nil
}

func sortedValidations(items []contract.ValidationRequest) []contract.ValidationRequest {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}
