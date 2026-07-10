package rpc

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"synora/internal/engine/contracts"
	"synora/pkg/contract"
)

const cgeListLimitMax = 100

type cgeDetailProvider interface {
	CGEDetailInspection() map[string]any
}

type cgeListRequest struct {
	Limit             int    `json:"limit,omitempty"`
	Offset            int    `json:"offset,omitempty"`
	Simulated         string `json:"simulated,omitempty"`
	MinCount          int    `json:"min_count,omitempty"`
	SignatureContains string `json:"signature_contains,omitempty"`
	ScenarioID        string `json:"scenario_id,omitempty"`
}

type cgeIDRequest struct {
	ID string `json:"id"`
}

func (s *Server) cgeSummary(_ contract.Message) (any, error) {
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	stats, _ := inspection["stats"].(map[string]any)
	if stats == nil {
		stats = map[string]any{}
	}
	return stats, nil
}

func (s *Server) cgeSequences(msg contract.Message) (any, error) {
	var req cgeListRequest
	_ = decodeOptionalPayload(msg.Payload, &req)
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	items := filterSequences(sequenceItems(inspection["sequences"]), req)
	return pageSequences(items, req), nil
}

func (s *Server) cgeTransitions(msg contract.Message) (any, error) {
	var req cgeListRequest
	_ = decodeOptionalPayload(msg.Payload, &req)
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	items := transitionItems(inspection["transitions"])
	return pageTransitions(items, req), nil
}

func (s *Server) cgeLearnedBehaviors(msg contract.Message) (any, error) {
	var req cgeListRequest
	_ = decodeOptionalPayload(msg.Payload, &req)
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	items := behaviorItems(inspection["learned_behaviors"])
	return pageBehaviors(items, req), nil
}

func (s *Server) cgeSequence(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return nil, errors.New("sequence id is required")
	}
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	for _, item := range sequenceItems(inspection["sequences"]) {
		if item.ID == req.ID {
			return item, nil
		}
	}
	return nil, errors.New("sequence not found")
}

func (s *Server) cgeLearnedBehavior(msg contract.Message) (any, error) {
	var req cgeIDRequest
	if err := decodePayload(msg.Payload, &req); err != nil {
		return nil, err
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		return nil, errors.New("learned behavior id is required")
	}
	inspection, err := s.cgeInspection()
	if err != nil {
		return nil, err
	}
	for _, item := range behaviorItems(inspection["learned_behaviors"]) {
		if item.ID == req.ID {
			return item, nil
		}
	}
	return nil, errors.New("learned behavior not found")
}

func (s *Server) cgeInspection() (map[string]any, error) {
	if s == nil || s.snapshot == nil || s.snapshot.CGE == nil {
		return nil, errors.New("cge inspection unavailable")
	}
	if detailed, ok := s.snapshot.CGE.(cgeDetailProvider); ok {
		return detailed.CGEDetailInspection(), nil
	}
	return s.snapshot.CGE.CGEInspection(), nil
}

func decodeOptionalPayload(raw []byte, out any) error {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func filterSequences(items []contracts.LearnedSequence, req cgeListRequest) []contracts.LearnedSequence {
	out := make([]contracts.LearnedSequence, 0, len(items))
	simulated := strings.ToLower(strings.TrimSpace(req.Simulated))
	signatureContains := strings.ToLower(strings.TrimSpace(req.SignatureContains))
	scenarioID := strings.TrimSpace(req.ScenarioID)
	for _, item := range items {
		if req.MinCount > 0 && item.Count < req.MinCount {
			continue
		}
		switch simulated {
		case "true":
			if item.SimulatedCount == 0 {
				continue
			}
		case "false":
			if item.RealCount == 0 {
				continue
			}
		}
		if signatureContains != "" && !strings.Contains(strings.ToLower(item.Signature), signatureContains) {
			continue
		}
		if scenarioID != "" && item.LastScenarioID != scenarioID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func pageSequences(items []contracts.LearnedSequence, req cgeListRequest) []map[string]any {
	items = page(items, req)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, compactSequenceMap(item))
	}
	return out
}

func pageTransitions(items []contracts.LearnedTransition, req cgeListRequest) []map[string]any {
	items = page(items, req)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":              item.ID,
			"from_event_type": item.FromEventType,
			"to_event_type":   item.ToEventType,
			"count":           item.Count,
			"simulated_count": item.SimulatedCount,
			"real_count":      item.RealCount,
			"confidence":      item.Confidence,
			"avg_delta_ms":    item.AvgDeltaMs,
			"first_seen":      item.FirstSeen,
			"last_seen":       item.LastSeen,
		})
	}
	return out
}

func pageBehaviors(items []contracts.LearnedBehavior, req cgeListRequest) []map[string]any {
	items = page(items, req)
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, compactBehaviorMap(item))
	}
	return out
}

func page[T any](items []T, req cgeListRequest) []T {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > cgeListLimitMax {
		limit = cgeListLimitMax
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

func compactSequenceMap(item contracts.LearnedSequence) map[string]any {
	return map[string]any{
		"id":               item.ID,
		"signature":        item.Signature,
		"event_types":      append([]string(nil), item.EventTypes...),
		"count":            item.Count,
		"simulated_count":  item.SimulatedCount,
		"real_count":       item.RealCount,
		"confidence":       item.Confidence,
		"first_seen":       item.FirstSeen,
		"last_seen":        item.LastSeen,
		"avg_delta_ms":     item.AvgDeltaMs,
		"last_test_run_id": emptyStringAsNil(item.LastTestRunID),
		"evidence_count":   len(item.Evidence),
		"example_count":    len(item.Examples),
	}
}

func compactBehaviorMap(item contracts.LearnedBehavior) map[string]any {
	return map[string]any{
		"id":                         item.ID,
		"trigger_sequence_signature": item.TriggerSequenceSignature,
		"status":                     item.Status,
		"requires_validation":        item.RequiresValidation,
		"count":                      item.Count,
		"confidence":                 item.Confidence,
		"simulated_count":            item.SimulatedCount,
		"real_count":                 item.RealCount,
		"proposed_actions":           compactActionMaps(item.ProposedActions),
		"evidence_count":             len(item.Evidence),
		"last_matched_at":            zeroTimeAsNil(item.LastMatchedAt),
		"last_triggered_at":          item.LastTriggeredAt,
	}
}

func compactActionMaps(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	allowed := map[string]bool{
		"id": true, "type": true, "action": true, "device_id": true,
		"command": true, "status": true,
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		compact := map[string]any{}
		for key, value := range item {
			if allowed[key] {
				compact[key] = value
			}
		}
		out = append(out, compact)
	}
	return out
}

func sequenceItems(value any) []contracts.LearnedSequence {
	switch typed := value.(type) {
	case []contracts.LearnedSequence:
		return typed
	default:
		return nil
	}
}

func transitionItems(value any) []contracts.LearnedTransition {
	switch typed := value.(type) {
	case []contracts.LearnedTransition:
		return typed
	default:
		return nil
	}
}

func behaviorItems(value any) []contracts.LearnedBehavior {
	switch typed := value.(type) {
	case []contracts.LearnedBehavior:
		return typed
	default:
		return nil
	}
}

func emptyStringAsNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func zeroTimeAsNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
