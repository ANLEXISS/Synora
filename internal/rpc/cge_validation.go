package rpc

import (
	"encoding/json"
	"log"
	"math"
	"strings"
	"time"

	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/idgen"
	"synora/pkg/contract"
)

var supportedValidationEventTypes = map[string]bool{
	contract.EventVisionUnknown: true, contract.EventVisionIdentity: true,
	contract.EventVisionUncertain: true, contract.EventVisionWeapon: true,
	contract.EventVisionFall: true, contract.EventDeviceOffline: true,
	"motion.detected": true, "weapon.detected": true, "fall.detected": true,
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
	}
	return s.queueValidationEvents(request.Events, "sequence", request.Learn, request.Reason)
}

func (s *Server) queueValidationEvents(requests []contract.CGEValidationEventRequest, kind string, inheritedLearn ...any) (map[string]any, error) {
	if s.state == nil || s.ingestEvent == nil {
		return nil, contract.NewAPIError(contract.ErrorInternal, "event ingestion unavailable")
	}
	learn := false
	sequenceReason := ""
	if len(inheritedLearn) > 0 {
		learn, _ = inheritedLearn[0].(bool)
	}
	if len(inheritedLearn) > 1 {
		sequenceReason, _ = inheritedLearn[1].(string)
	}
	validationID := idgen.New("validation")
	base := time.Now().UTC()
	log.Printf("core: validation sequence received validation_id=%s events=%d learn=%t", validationID, len(requests), learn)

	// Validate the complete sequence before enqueueing anything. This avoids a
	// partially injected validation run when a later item is malformed.
	for index, request := range requests {
		rawEventType := strings.ToLower(strings.TrimSpace(request.EventType))
		if !supportedValidationEventTypes[rawEventType] {
			log.Printf("core: validation sequence rejected reason=unsupported_event_type event_index=%d type=%q", index, rawEventType)
			return nil, contract.NewAPIErrorWithDetails(contract.ErrorValidationFailed,
				map[string]any{"event_index": index, "event_type": rawEventType},
				"unsupported event_type %q at events[%d]", rawEventType, index)
		}
		if request.Confidence < 0 || request.Confidence > 1 || math.IsNaN(request.Confidence) || math.IsInf(request.Confidence, 0) {
			log.Printf("core: validation sequence rejected reason=invalid_confidence event_index=%d", index)
			return nil, contract.NewAPIErrorWithDetails(contract.ErrorValidationFailed,
				map[string]any{"event_index": index, "field": "confidence"},
				"confidence must be between 0 and 1 at events[%d]", index)
		}
		hint := strings.ToLower(strings.TrimSpace(request.DangerLevelHint))
		switch hint {
		case "", "none", "low", "medium", "medium_high", "high", "critical":
		default:
			log.Printf("core: validation sequence rejected reason=invalid_danger_level_hint event_index=%d hint=%q", index, hint)
			return nil, contract.NewAPIErrorWithDetails(contract.ErrorValidationFailed,
				map[string]any{"event_index": index, "field": "danger_level_hint", "value": hint},
				"invalid danger_level_hint %q at events[%d]", hint, index)
		}
		nodeID, err := s.resolveValidationNode(request, index)
		if err != nil {
			return nil, err
		}
		request.NodeID = nodeID
		requests[index] = request
	}

	results := make([]map[string]any, 0, len(requests))
	for index, request := range requests {
		eventType := request.NormalizedEventType()
		hint := strings.ToLower(strings.TrimSpace(request.DangerLevelHint))
		eventLearn := request.LearnEnabled(learn)
		eventID := idgen.New("evt")
		reason := strings.TrimSpace(request.Reason)
		if reason == "" {
			reason = strings.TrimSpace(sequenceReason)
		}
		if reason == "" {
			reason = "controlled real CGE validation"
		}
		metadata := map[string]any{
			"validation": true, "source_type": contract.SourceValidation,
			"lab_source_type": "synora_lab",
			"test_mode":       contract.ValidationTestModeControlledReal,
			"generated_by":    "synora_lab",
			"validation_id":   validationID, "event_id": eventID, "learn": eventLearn, "reason": reason,
			"event_index": index,
		}
		payload := map[string]any{
			"event_id": eventID, "device_id": strings.TrimSpace(request.DeviceID),
			"node_id": strings.TrimSpace(request.NodeID), "identity": strings.TrimSpace(request.Identity),
			"confidence": request.Confidence, "activation_id": validationID, "sequence_key": validationID,
			"event_index": index, "clip_index": index,
			"metadata": metadata, "reason": reason,
			"source_type": contract.SourceValidation, "lab_source_type": "synora_lab",
			"validation": true, "test_mode": contract.ValidationTestModeControlledReal,
			"generated_by": "synora_lab",
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
		case "high", "medium_high":
			priority = contract.PriorityHigh
		}
		event := &contract.Event{
			ID: eventID, Type: eventType, Source: contract.SourceValidation, Timestamp: at,
			Payload: payload, DeviceID: strings.TrimSpace(request.DeviceID), NodeID: strings.TrimSpace(request.NodeID),
			Identity: strings.TrimSpace(request.Identity), Confidence: request.Confidence,
			Priority: priority, ActivationID: validationID, SequenceKey: validationID, ClipIndex: index,
		}
		log.Printf("core: validation sequence event index=%d type=%s event_id=%s", index, eventType, eventID)
		s.state.AddValidationEvent(event)
		s.ingestEvent(event)
		log.Printf("core: validation sequence queued event_id=%s", eventID)
		results = append(results, map[string]any{
			"event_id": eventID, "validation_id": validationID, "event_type": eventType,
			"device_id": strings.TrimSpace(request.DeviceID), "node_id": strings.TrimSpace(request.NodeID),
			"confidence":  request.Confidence,
			"source_type": contract.SourceValidation, "lab_source_type": "synora_lab",
			"test_mode":    contract.ValidationTestModeControlledReal,
			"generated_by": "synora_lab",
			"learn":        eventLearn, "event_index": index, "clip_index": index, "queued_at": at,
		})
	}
	return map[string]any{"status": "queued", "kind": firstNonEmpty(kind, "event"), "validation_id": validationID, "activation_id": validationID, "sequence_key": validationID, "events": results}, nil
}

func (s *Server) resolveValidationNode(request contract.CGEValidationEventRequest, index int) (string, error) {
	deviceID := strings.TrimSpace(request.DeviceID)
	nodeID := strings.TrimSpace(request.NodeID)

	if s.devices != nil && deviceID != "" {
		configured, ok := s.devices.Get(deviceID)
		if !ok || configured == nil || configured.DeletedAt != nil {
			return "", contract.NewAPIErrorWithDetails(contract.ErrorValidationFailed,
				map[string]any{"event_index": index, "field": "device_id", "value": deviceID},
				"unknown device_id %q at events[%d]", deviceID, index)
		}
		nodeID = strings.TrimSpace(configured.NodeID)
		if nodeID == "" {
			nodeID = strings.TrimSpace(configured.Room)
		}
	}

	if nodeID == "" || nodeID == device.UnlocatedNodeID {
		field := "node_id"
		if deviceID != "" {
			field = "device_id"
		}
		return "", contract.NewAPIErrorWithDetails(contract.ErrorValidationFailed,
			map[string]any{"event_index": index, "field": field, "device_id": deviceID},
			"device %q has no assigned node at events[%d]; assign a room before injecting", deviceID, index)
	}
	return nodeID, nil
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
