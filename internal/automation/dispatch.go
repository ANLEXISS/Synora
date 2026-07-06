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

func (d Dispatcher) Dispatch(action contract.Action) error {
	now := time.Now().UTC()
	if d.Now != nil {
		now = d.Now().UTC()
	}
	id := idgen.New("msg")
	if d.NewID != nil {
		id = d.NewID()
	}
	request := contract.ActionRequest{
		ID:             id,
		Version:        "v1",
		RequestID:      id,
		CorrelationID:  id,
		Source:         firstNonEmpty(d.Source, "core"),
		Target:         firstNonEmpty(d.Target, "actions"),
		Timestamp:      now,
		IdempotencyKey: fmt.Sprintf("%s:%s", id, actionKey(action)),
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
