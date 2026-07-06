package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Service struct {
	Executor Executor
	Bus      Publisher
	Deduper  *Deduper
	Now      func() time.Time
	NewID    func(prefix string) string
}

func (s *Service) HandleMessage(ctx context.Context, msg contract.Message) {
	if msg.Kind != contract.KindCommand || msg.Type != contract.EventActionRequest {
		return
	}

	request, err := DecodeRequest(msg)
	if err != nil {
		log.Printf("actions: invalid action payload id=%s err=%v", msg.ID, err)
		s.publishResult(msg, contract.ActionRequest{}, StatusFailed, err.Error(), nil)
		return
	}

	if request.ID == "" {
		request.ID = firstNonEmpty(msg.RequestID, msg.ID)
	}
	if request.RequestID == "" {
		request.RequestID = firstNonEmpty(msg.RequestID, request.ID)
	}
	if request.CorrelationID == "" {
		request.CorrelationID = msg.CorrelationID
	}
	if request.Source == "" {
		request.Source = msg.Source
	}
	if request.Target == "" {
		request.Target = msg.Target
	}
	if request.Timestamp.IsZero() {
		request.Timestamp = msg.Timestamp
	}

	key := idempotencyKey(request, msg)
	if s.deduper().SeenOrAdd(key) {
		s.publishResult(msg, request, StatusDuplicate, "", map[string]any{
			"idempotency_key": key,
		})
		return
	}

	executor := s.Executor
	if executor == nil {
		executor = DryRunExecutor{Adapter: "dry_run"}
	}

	result, err := executor.Execute(ctx, request)
	if err != nil {
		s.publishResult(msg, request, StatusFailed, err.Error(), result.Details)
		return
	}

	status := result.Status
	if status == "" {
		status = StatusAccepted
	}
	s.publishResult(msg, request, status, "", result.Details)
}

func DecodeRequest(msg contract.Message) (contract.ActionRequest, error) {
	var request contract.ActionRequest
	if len(msg.Payload) == 0 {
		return request, fmt.Errorf("empty payload")
	}

	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return request, err
	}
	if !isEmptyAction(request.Action) || request.ID != "" || request.RequestID != "" || request.IdempotencyKey != "" {
		return request, nil
	}

	var legacy contract.Action
	if err := json.Unmarshal(msg.Payload, &legacy); err != nil {
		return request, err
	}
	if isEmptyAction(legacy) {
		return request, fmt.Errorf("empty action")
	}

	request.Action = legacy
	return request, nil
}

func (s *Service) publishResult(
	msg contract.Message,
	request contract.ActionRequest,
	status string,
	errorMessage string,
	details map[string]any,
) {
	if s.Bus == nil {
		return
	}

	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}

	id := idgen.New("ares")
	if s.NewID != nil {
		id = s.NewID("ares")
	}

	result := contract.ActionResult{
		ID:            id,
		Version:       request.Version,
		RequestID:     firstNonEmpty(request.RequestID, msg.RequestID, msg.ID),
		CorrelationID: firstNonEmpty(request.CorrelationID, msg.CorrelationID),
		ActionID:      firstNonEmpty(request.ID, msg.ID),
		Source:        "actions",
		Timestamp:     now,
		Status:        status,
		Error:         errorMessage,
		Details:       details,
	}

	payload, err := json.Marshal(result)
	if err != nil {
		log.Printf("actions: result marshal failed request=%s err=%v", result.RequestID, err)
		return
	}

	target := request.Source
	if target == "" {
		target = msg.Source
	}
	if target == "" {
		target = "core"
	}

	if err := s.Bus.Send(contract.Message{
		ID:            idgen.New("msg"),
		Type:          contract.EventActionResult,
		Kind:          contract.KindEvent,
		Source:        "actions",
		Target:        target,
		Timestamp:     now,
		CorrelationID: result.CorrelationID,
		RequestID:     result.RequestID,
		Payload:       payload,
	}); err != nil {
		log.Printf("actions: result publish failed request=%s err=%v", result.RequestID, err)
	}
}

func (s *Service) deduper() *Deduper {
	if s.Deduper == nil {
		s.Deduper = NewDeduper()
	}
	return s.Deduper
}

func idempotencyKey(request contract.ActionRequest, msg contract.Message) string {
	if request.IdempotencyKey != "" {
		return request.IdempotencyKey
	}
	if request.RequestID != "" {
		return request.RequestID
	}
	if request.ID != "" {
		return request.ID
	}
	return msg.ID
}

func isEmptyAction(action contract.Action) bool {
	return action.Type == "" &&
		action.Device == "" &&
		action.Command == "" &&
		action.Value == nil &&
		action.Channel == "" &&
		len(action.Residents) == 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
