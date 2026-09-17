package cognitive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"synora/pkg/contract"
)

type Bus interface {
	Send(contract.Message) error
	SubscribeChannel(string) <-chan contract.Message
}

type Service struct {
	Bus       Bus
	Scheduler *Scheduler
	Name      string
}

func (s *Service) PublishStatus() error {
	if s == nil || s.Bus == nil || s.Scheduler == nil {
		return fmt.Errorf("cognitive service is not configured")
	}
	name := s.Name
	if name == "" {
		name = "cognitive"
	}
	body, err := json.Marshal(Status{
		SchemaVersion: SchemaVersion,
		Service:       name,
		Status:        "ready",
		AdvisoryOnly:  true,
		DryRunOnly:    true,
		Adapters:      s.Scheduler.registry.List(),
	})
	if err != nil {
		return err
	}
	return s.Bus.Send(contract.Message{
		Type:      MessageTypeStatus,
		Kind:      contract.KindEvent,
		Source:    name,
		Timestamp: time.Now().UTC(),
		Payload:   body,
	})
}

func (s *Service) Handle(ctx context.Context, msg contract.Message) error {
	if s == nil || s.Bus == nil || s.Scheduler == nil {
		return fmt.Errorf("cognitive service is not configured")
	}
	if msg.Type != MessageTypeTask {
		return nil
	}
	var input CognitiveInput
	if err := json.Unmarshal(msg.Payload, &input); err != nil {
		return s.publishError(msg, err)
	}
	if input.RequestID == "" {
		input.RequestID = msg.ID
	}
	if input.SchemaVersion == "" {
		input.SchemaVersion = SchemaVersion
	}
	if input.Task.ID == "" {
		input.Task.ID = input.RequestID
	}
	output, err := s.Scheduler.Run(ctx, input)
	if err != nil {
		return s.publishError(msg, err)
	}
	body, err := json.Marshal(output)
	if err != nil {
		return s.publishError(msg, err)
	}
	name := s.Name
	if name == "" {
		name = "cognitive"
	}
	return s.Bus.Send(contract.Message{
		Type:          MessageTypeOutput,
		Kind:          contract.KindEvent,
		Source:        name,
		Target:        msg.Source,
		CorrelationID: msg.ID,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
	})
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil || s.Bus == nil || s.Scheduler == nil {
		return fmt.Errorf("cognitive service is not configured")
	}
	name := s.Name
	if name == "" {
		name = "cognitive"
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-s.Bus.SubscribeChannel(name):
			if !ok {
				return nil
			}
			if err := s.Handle(ctx, msg); err != nil {
				log.Printf("cognitive task handling error: %v", err)
			}
		}
	}
}

func (s *Service) publishError(msg contract.Message, cause error) error {
	body, err := json.Marshal(ErrorNotice{
		SchemaVersion: SchemaVersion,
		RequestID:     msg.ID,
		Error:         cause.Error(),
		AdvisoryOnly:  true,
	})
	if err != nil {
		return err
	}
	name := s.Name
	if name == "" {
		name = "cognitive"
	}
	return s.Bus.Send(contract.Message{
		Type:          MessageTypeError,
		Kind:          contract.KindEvent,
		Source:        name,
		Target:        msg.Source,
		CorrelationID: msg.ID,
		Timestamp:     time.Now().UTC(),
		Payload:       body,
	})
}
