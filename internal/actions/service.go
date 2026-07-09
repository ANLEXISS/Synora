package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type Service struct {
	Executor       Executor
	Bus            Publisher
	Deduper        *Deduper
	Now            func() time.Time
	NewID          func(prefix string) string
	DefaultTimeout time.Duration
}

func (s *Service) HandleMessage(ctx context.Context, msg contract.Message) {
	if msg.Kind != contract.KindCommand || (msg.Type != contract.EventActionRequest && msg.Type != contract.EventAutomationAction) {
		return
	}

	request, err := DecodeRequest(msg)
	if err != nil {
		log.Printf("actions: invalid action payload id=%s err=%v", msg.ID, err)
		now := s.now()
		s.publishResult(msg, contract.ActionRequest{}, StatusError, err.Error(), now, now, nil)
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
	if request.Timestamp.IsZero() {
		request.Timestamp = msg.Timestamp
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = request.Timestamp
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = s.now()
	}
	normalizeRequest(&request)

	key := idempotencyKey(request, msg)
	if s.deduper().SeenOrAdd(key) {
		now := s.now()
		s.publishResult(msg, request, StatusDuplicate, "", now, now, map[string]any{
			"idempotency_key": key,
			"reason":          "duplicate",
		})
		return
	}

	if requestDryRun(request) {
		now := s.now()
		s.publishResult(msg, request, StatusSimulatedSuccess, "", now, now, map[string]any{
			"dry_run":   true,
			"simulated": true,
			"reason":    "simulation dry-run; action handler was not executed",
		})
		return
	}

	executor := s.Executor
	if executor == nil {
		executor = DryRunExecutor{Adapter: "dry_run"}
	}

	status, errorMessage, startedAt, finishedAt, details := s.executeWithRetry(ctx, executor, request)
	s.publishResult(msg, request, status, errorMessage, startedAt, finishedAt, details)
}

func DecodeRequest(msg contract.Message) (contract.ActionRequest, error) {
	var request contract.ActionRequest
	if len(msg.Payload) == 0 {
		return request, fmt.Errorf("empty payload")
	}

	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return request, err
	}
	if !isEmptyAction(request.Action) ||
		request.ID != "" ||
		request.Type != "" ||
		request.Target != "" ||
		len(request.Data) > 0 ||
		request.RequestID != "" ||
		request.IdempotencyKey != "" {
		normalizeRequest(&request)
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
	normalizeRequest(&request)
	return request, nil
}

func normalizeRequest(request *contract.ActionRequest) {
	if request == nil {
		return
	}
	if request.Type == "" {
		request.Type = request.Action.Type
	}
	if request.Target == "" {
		request.Target = actionTarget(request.Action)
	}
	if len(request.Data) == 0 {
		request.Data = actionData(request.Action)
	}
	if request.Action.Type == "" {
		request.Action.Type = request.Type
	}
	if request.Action.Data == nil && request.Data != nil {
		request.Action.Data = cloneMap(request.Data)
	}
	if request.Action.Device == "" && strings.HasPrefix(strings.ToLower(request.Type), "device.") {
		request.Action.Device = request.Target
	}
	if request.Action.Channel == "" && strings.HasPrefix(strings.ToLower(request.Type), "mqtt.") {
		request.Action.Channel = request.Target
	}
	if request.Action.Command == "" {
		if command, ok := request.Data["command"].(string); ok {
			request.Action.Command = command
		}
	}
	if request.Action.Value == nil {
		if value, ok := request.Data["value"]; ok {
			request.Action.Value = value
		}
	}
	if request.TimeoutMs == 0 {
		request.TimeoutMs = request.Action.TimeoutMs
	}
	if request.RetryCount == 0 {
		request.RetryCount = firstNonZero(request.Retry, request.Action.Retry)
	}
	if request.Retry == 0 {
		request.Retry = request.RetryCount
	}
}

func (s *Service) executeWithRetry(
	ctx context.Context,
	executor Executor,
	request contract.ActionRequest,
) (string, string, time.Time, time.Time, map[string]any) {
	attempts := request.RetryCount + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	var lastResult ExecutionResult
	var startedAt time.Time
	var finishedAt time.Time
	for attempt := 1; attempt <= attempts; attempt++ {
		startedAt = s.now()
		execCtx := ctx
		cancel := func() {}
		if timeout := s.timeout(request); timeout > 0 {
			execCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		result, err := executor.Execute(execCtx, request)
		cancel()
		finishedAt = s.now()

		lastResult = result
		lastErr = err
		if err == nil {
			status := result.Status
			if status == "" {
				status = StatusSuccess
			}
			details := cloneMap(result.Details)
			if attempt > 1 {
				if details == nil {
					details = map[string]any{}
				}
				details["attempts"] = attempt
			}
			return status, "", startedAt, finishedAt, details
		}
		if !isTimeoutError(err) && attempt < attempts {
			continue
		}
		if isTimeoutError(err) && attempt < attempts {
			continue
		}
	}

	status := StatusError
	errorMessage := ""
	if lastErr != nil {
		errorMessage = lastErr.Error()
	}
	if isTimeoutError(lastErr) {
		status = StatusTimeout
	}
	details := cloneMap(lastResult.Details)
	if request.RetryCount > 0 {
		if details == nil {
			details = map[string]any{}
		}
		details["attempts"] = attempts
	}
	return status, errorMessage, startedAt, finishedAt, details
}

func (s *Service) timeout(request contract.ActionRequest) time.Duration {
	if request.TimeoutMs > 0 {
		return time.Duration(request.TimeoutMs) * time.Millisecond
	}
	return s.DefaultTimeout
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) publishResult(
	msg contract.Message,
	request contract.ActionRequest,
	status string,
	errorMessage string,
	startedAt time.Time,
	finishedAt time.Time,
	details map[string]any,
) {
	if s.Bus == nil {
		return
	}

	now := finishedAt
	if now.IsZero() {
		now = s.now()
	}

	id := idgen.New("ares")
	if s.NewID != nil {
		id = s.NewID("ares")
	}

	result := contract.ActionResult{
		ID:            id,
		Version:       request.Version,
		RequestID:     firstNonEmpty(request.ID, request.RequestID, msg.RequestID, msg.ID),
		AutomationID:  request.AutomationID,
		ActionID:      firstNonEmpty(request.ActionID, request.ID, msg.ID),
		Type:          request.Type,
		Target:        request.Target,
		CorrelationID: firstNonEmpty(request.CorrelationID, msg.CorrelationID),
		Source:        "actions",
		Timestamp:     now,
		Status:        status,
		Error:         errorMessage,
		StartedAt:     startedAt,
		FinishedAt:    now,
		DurationMs:    durationMillis(startedAt, now),
		Attempts:      attemptsFromDetails(details),
		Data:          details,
		Details:       details,
	}
	if result.Data == nil {
		result.Data = map[string]any{}
	}
	if len(request.Metadata) > 0 {
		result.Data["metadata"] = cloneMap(request.Metadata)
	}
	if requestDryRun(request) {
		result.Data["dry_run"] = true
		result.Data["simulated"] = true
	}
	result.Details = result.Data

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
		len(action.Data) == 0 &&
		len(action.Residents) == 0
}

func actionTarget(action contract.Action) string {
	for _, value := range []string{action.Device, action.Channel, action.Type} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func actionData(action contract.Action) map[string]any {
	data := cloneMap(action.Data)
	if data == nil {
		data = map[string]any{}
	}
	if action.Command != "" {
		data["command"] = action.Command
	}
	if action.Value != nil {
		data["value"] = action.Value
	}
	if len(action.Residents) > 0 {
		data["residents"] = append([]string(nil), action.Residents...)
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

func isTimeoutError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func cloneMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func requestDryRun(request contract.ActionRequest) bool {
	if request.Metadata == nil {
		return false
	}
	value, ok := request.Metadata["dry_run"].(bool)
	return ok && value
}

func durationMillis(startedAt time.Time, finishedAt time.Time) int64 {
	if startedAt.IsZero() || finishedAt.IsZero() {
		return 0
	}
	return finishedAt.Sub(startedAt).Milliseconds()
}

func attemptsFromDetails(details map[string]any) int {
	if details == nil {
		return 1
	}
	switch value := details["attempts"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 1
	}
}
