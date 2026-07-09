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

func actionKey(action contract.Action) string {
	data, err := json.Marshal(action)
	if err != nil {
		return ""
	}
	return string(data)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
