package rpc

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"synora/internal/event"
	"synora/internal/idgen"
	"synora/pkg/contract"
)

var supportedValidationEventTypes = map[string]bool{
	contract.EventVisionUnknown: true, contract.EventVisionIdentity: true,
	contract.EventVisionUncertain: true, contract.EventVisionWeapon: true,
	contract.EventVisionFall: true, contract.EventDeviceOffline: true,
	"camera.offline": true, "camera.online": true, "camera.tampered": true, contract.EventVisionTamper: true,
	contract.EventManualRisk: true, contract.EventVisionMotion: true,
}

func (s *Server) cgeValidationEvent(msg contract.Message) (any, error) {
	var request contract.CGEValidationEventRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid CGE validation event payload")
	}
	return s.queueValidationEvents([]contract.CGEValidationEventRequest{request}, "")
}

func (s *Server) cgeValidationSequence(msg contract.Message) (any, error) {
	var request contract.CGEValidationSequenceRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return nil, contract.NewAPIError(contract.ErrorInvalidJSON, "invalid CGE validation sequence payload")
	}
	if len(request.Events) == 0 || len(request.Events) > 20 {
		return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "events must contain between one and twenty items")
	}
	for i := range request.Events {
		if request.Events[i].Reason == "" {
			request.Events[i].Reason = request.Reason
		}
		request.Events[i].Learn = request.Learn
	}
	return s.queueValidationEvents(request.Events, "sequence")
}

func (s *Server) queueValidationEvents(requests []contract.CGEValidationEventRequest, kind string) (map[string]any, error) {
	if s.state == nil || s.ingestEvent == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "event ingestion unavailable")
	}
	validationID := idgen.New("validation")
	base := time.Now().UTC()
	results := make([]map[string]any, 0, len(requests))
	for index, request := range requests {
		rawEventType := strings.ToLower(strings.TrimSpace(request.EventType))
		if !supportedValidationEventTypes[rawEventType] {
			return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "unsupported validation event type %q", rawEventType)
		}
		eventType := request.NormalizedEventType()
		if request.Confidence < 0 || request.Confidence > 1 || math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) {
			return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "confidence must be between 0 and 1")
		}
		hint := strings.ToLower(strings.TrimSpace(request.DangerLevelHint))
		switch hint {
		case "", "none", "low", "medium", "high", "critical":
		default:
			return nil, contract.NewAPIError(contract.ErrorInvalidRequest, "danger_level_hint must be none, low, medium, high or critical")
		}
		eventID := idgen.New("evt")
		reason := strings.TrimSpace(request.Reason)
		if reason == "" {
			reason = "controlled real CGE validation"
		}
		metadata := map[string]any{
			"validation": true, "source_type": contract.SourceValidation,
			"test_mode":     contract.ValidationTestModeControlledReal,
			"validation_id": validationID, "event_id": eventID, "learn": request.Learn, "reason": reason,
		}
		payload := map[string]any{
			"event_id": eventID, "device_id": strings.TrimSpace(request.DeviceID),
			"node_id": strings.TrimSpace(request.NodeID), "identity": strings.TrimSpace(request.Identity),
			"confidence": request.Confidence, "activation_id": validationID, "sequence_key": validationID,
			"metadata": metadata, "reason": reason,
			"validation": true, "test_mode": contract.ValidationTestModeControlledReal,
		}
		if hint != "" {
			payload["danger_level_hint"] = hint
			if eventType == contract.EventManualRisk {
				payload["danger_level"] = hint
			}
		}
		at := base.Add(time.Duration(index) * 500 * time.Millisecond)
		priority := contract.EventPriority(eventType)
		switch strings.ToLower(strings.TrimSpace(request.DangerLevelHint)) {
		case "critical":
			priority = contract.PriorityCritical
		case "high":
			priority = contract.PriorityHigh
		}
		event := &contract.Event{
			ID: eventID, Type: eventType, Source: contract.SourceValidation, Timestamp: at,
			Payload: payload, DeviceID: strings.TrimSpace(request.DeviceID), NodeID: strings.TrimSpace(request.NodeID),
			Identity: strings.TrimSpace(request.Identity), Confidence: request.Confidence,
			Priority: priority, ActivationID: validationID, SequenceKey: validationID,
		}
		s.state.AddValidationEvent(event)
		s.ingestEvent(event)
		results = append(results, map[string]any{
			"event_id": eventID, "validation_id": validationID, "event_type": eventType,
			"source_type": contract.SourceValidation, "test_mode": contract.ValidationTestModeControlledReal,
			"learn": request.Learn, "queued_at": at,
		})
	}
	return map[string]any{"status": "queued", "kind": firstNonEmpty(kind, "event"), "validation_id": validationID, "events": results}, nil
}

func (s *Server) cgeValidationHistory(_ contract.Message) (any, error) {
	if s.state == nil {
		return []contract.CGEValidationHistoryItem{}, nil
	}
	items := s.state.ValidationEventsList()
	var chains []*contract.EventChain
	if s.chains != nil {
		chains = s.chains.List(eventChainAllFilter())
	}
	result := make([]contract.CGEValidationHistoryItem, 0, len(items))
	for _, item := range items {
		metadata, _ := item.Payload["metadata"].(map[string]any)
		chainID := ""
		for _, chain := range chains {
			for _, chainEvent := range chain.RecentEvents {
				if chainEvent.ID == item.ID {
					chainID = chain.ID
					break
				}
			}
			if chainID != "" {
				break
			}
		}
		result = append(result, contract.CGEValidationHistoryItem{
			ValidationID: stringMetadata(metadata, "validation_id"), EventID: item.ID, EventType: item.Type,
			Timestamp: item.Timestamp, DeviceID: item.DeviceID, NodeID: item.NodeID, ChainID: chainID,
			Learn: boolMetadata(metadata, "learn"), Reason: stringMetadata(metadata, "reason"),
			SourceType: contract.SourceValidation, TestMode: stringMetadata(metadata, "test_mode"),
		})
	}
	return result, nil
}

func (s *Server) cgeValidationHistoryClear(_ contract.Message) (any, error) {
	if s.state == nil {
		return map[string]any{"cleared": 0}, nil
	}
	return map[string]any{"cleared": s.state.ClearValidationEvents()}, nil
}

func eventChainAllFilter() event.ChainFilter { return event.ChainFilter{Status: "all"} }

func stringMetadata(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
func boolMetadata(metadata map[string]any, key string) bool {
	value, _ := metadata[key].(bool)
	return value
}
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
