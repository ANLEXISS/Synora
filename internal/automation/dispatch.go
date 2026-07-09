package automation

import (
	"encoding/json"
	"fmt"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

type ActionSender interface {
	Send(contract.Message) error
}

type Dispatcher struct {
	Bus    ActionSender
	Source string
	Target string
	Now    func() time.Time
	NewID  func() string
}

type ActionContext struct {
	SourceEventID string
	DecisionID    string
}

func (d Dispatcher) Dispatch(action contract.Action, contexts ...ActionContext) error {
	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now().UTC()
	}
	id := idgen.New("msg")
	if d.NewID != nil {
		id = d.NewID()
	}
	actionContext := ActionContext{}
	if len(contexts) > 0 {
		actionContext = contexts[0]
	}
	request := contract.ActionRequest{
		ID:             id,
		Type:           action.Type,
		Version:        "v1",
		RequestID:      id,
		CorrelationID:  id,
		Source:         firstNonEmpty(d.Source, "core"),
		Timestamp:      now,
		CreatedAt:      now,
		IdempotencyKey: fmt.Sprintf("%s:%s", id, actionKey(action)),
		Target:         actionTarget(action),
		Data:           actionData(action),
		SourceEventID:  actionContext.SourceEventID,
		DecisionID:     actionContext.DecisionID,
		TimeoutMs:      action.TimeoutMs,
		Retry:          action.Retry,
		Action:         action,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	source := d.Source
	if source == "" {
		source = "core"
	}
	target := d.Target
	if target == "" {
		target = "actions"
	}
	return d.Bus.Send(contract.Message{
		ID:        id,
		Type:      contract.EventActionRequest,
		Kind:      contract.KindCommand,
		Source:    source,
		Target:    target,
		Timestamp: now,
		Payload:   body,
	})
}

func (d Dispatcher) DispatchRequest(request contract.ActionRequest) error {
	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now().UTC()
	}
	id := request.ID
	if id == "" {
		id = idgen.New("areq")
		if d.NewID != nil {
			id = d.NewID()
		}
		request.ID = id
	}
	if request.CreatedAt.IsZero() {
		request.CreatedAt = now
	}
	if request.Timestamp.IsZero() {
		request.Timestamp = now
	}
	if request.Source == "" {
		request.Source = firstNonEmpty(d.Source, "core")
	}
	if request.RequestID == "" {
		request.RequestID = request.ID
	}
	if request.CorrelationID == "" {
		request.CorrelationID = firstNonEmpty(request.DecisionID, request.SourceEventID, request.ID)
	}
	if request.Version == "" {
		request.Version = "v1"
	}
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = fmt.Sprintf("%s:%s:%s", request.AutomationID, request.ActionID, request.SourceEventID)
	}
	if request.Type == "" {
		request.Type = request.Action.Type
	}
	if request.Target == "" {
		request.Target = actionTarget(request.Action)
	}
	if request.Data == nil {
		request.Data = actionData(request.Action)
	}
	if request.Retry == 0 {
		request.Retry = request.RetryCount
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	source := d.Source
	if source == "" {
		source = "core"
	}
	target := d.Target
	if target == "" {
		target = "actions"
	}
	return d.Bus.Send(contract.Message{
		ID:            id,
		Type:          contract.EventActionRequest,
		Kind:          contract.KindCommand,
		Source:        source,
		Target:        target,
		Timestamp:     now,
		CorrelationID: request.CorrelationID,
		RequestID:     request.ID,
		Payload:       body,
	})
}

func actionKey(action contract.Action) string {
	data, err := json.Marshal(action)
	if err != nil {
		return ""
	}
	return string(data)
}

func legacyAction(action AutomationAction) contract.Action {
	data := cloneMap(action.Data)
	retry := action.RetryCount
	if retry == 0 {
		retry = action.Retry
	}
	return contract.Action{
		Type:      action.Type,
		Device:    action.Device,
		Command:   action.Command,
		Value:     action.Value,
		Channel:   action.Channel,
		Residents: append([]string(nil), action.Residents...),
		Data:      data,
		TimeoutMs: action.TimeoutMs,
		Retry:     retry,
	}
}

func actionTarget(action contract.Action) string {
	for _, value := range []string{action.Device, action.Channel, action.Type} {
		if value != "" {
			return value
		}
	}
	return ""
}

func actionData(action contract.Action) map[string]any {
	data := map[string]any{}
	for key, value := range action.Data {
		data[key] = value
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
