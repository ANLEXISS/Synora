package cognitivecore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"synora/pkg/contract"
)

type Bus interface {
	Send(contract.Message) error
	SubscribeChannel(string) <-chan contract.Message
}

type Service struct {
	Bus  Bus
	Core *Core
	Name string
	Now  func() time.Time
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *Service) Handle(ctx context.Context, message contract.Message) error {
	if s == nil || s.Bus == nil || s.Core == nil {
		return fmt.Errorf("cognitive core service is not configured")
	}
	event := EventFromMessage(message)
	if message.Type == contract.EventValidationTestInference {
		if event.Source != "api" || payloadString(event.Payload, "provenance") != "test-harness" || !payloadBoolDefault(event.Payload, "test", false) {
			return fmt.Errorf("invalid test-harness envelope")
		}
		event.Type = payloadString(event.Payload, "event_type")
		if event.Type == "" {
			return fmt.Errorf("test-harness event type is required")
		}
		if !cataloguedTestEventType(event.Type) {
			return fmt.Errorf("test-harness event type is not catalogued")
		}
		result, err := s.Core.ProcessTest(ctx, event)
		if err != nil {
			return err
		}
		if result.Commit.Decision.Trace == nil {
			return nil
		}
		return s.sendTestDecision(event, result)
	}
	result, err := s.Core.Process(ctx, event)
	if event.Type == contract.EventActionResult || event.Type == "discovery.action.result" {
		status := "recorded"
		if err != nil {
			status = "rejected"
		} else if result.Result.Duplicate {
			status = "duplicate"
		}
		body, marshalErr := json.Marshal(map[string]any{"schema_version": "core-action-result/v1", "status": status, "revision": result.Result.Revision, "request_id": ActionResultRequestID(event.Payload)})
		if marshalErr != nil {
			return marshalErr
		}
		if sendErr := s.Bus.Send(contract.Message{ID: event.ID + ":action-result", Type: "core.action_result", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: body}); sendErr != nil {
			return sendErr
		}
		return nil
	}
	if err != nil {
		return err
	}
	if result.Result.Duplicate {
		return nil
	}
	decisionPayload, err := json.Marshal(struct {
		SchemaVersion          string   `json:"schema_version"`
		Revision               uint64   `json:"revision"`
		Decision               Decision `json:"decision"`
		PhysicalActionExecuted bool     `json:"physical_action_executed"`
	}{"core-decision/v1", result.Result.Revision, result.Commit.Decision, false})
	if err != nil {
		return err
	}
	if err := s.Bus.Send(contract.Message{ID: event.ID + ":decision", Type: "core.decision", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "discovery", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: decisionPayload}); err != nil {
		return err
	}
	// The API is a read-only consumer of the already-redacted decision trace.
	// It receives a separate targeted event so Discovery remains the only
	// external runtime peer for the normal Core flow.
	if err := s.Bus.Send(contract.Message{ID: event.ID + ":decision:api", Type: "core.decision", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: decisionPayload}); err != nil {
		return err
	}
	snapshotPayload, err := json.Marshal(struct {
		SchemaVersion          string            `json:"schema_version"`
		Revision               uint64            `json:"revision"`
		Snapshot               CognitiveSnapshot `json:"snapshot"`
		PhysicalActionExecuted bool              `json:"physical_action_executed"`
	}{"core-snapshot/v1", result.Result.Revision, result.Commit.Snapshot, false})
	if err != nil {
		return err
	}
	if err := s.Bus.Send(contract.Message{ID: event.ID + ":snapshot", Type: "core.snapshot", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "discovery", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: snapshotPayload}); err != nil {
		return err
	}
	if result.Result.Action != nil {
		body, err := json.Marshal(struct {
			SchemaVersion string `json:"schema_version"`
			RequestID     string `json:"request_id"`
			EpisodeID     string `json:"episode_id,omitempty"`
			Action        string `json:"action"`
			Topology      string `json:"topology,omitempty"`
			Capability    string `json:"capability,omitempty"`
			DryRun        bool   `json:"dry_run"`
		}{"action-request/v1", result.Result.Action.RequestID, result.Result.Action.EpisodeID, result.Result.Action.Action.Action, result.Result.Action.Action.Topology, result.Result.Action.Action.Capability, true})
		if err != nil {
			return err
		}
		if err := s.Bus.Send(contract.Message{ID: result.Result.Action.RequestID + ":request", Type: "action.request", Kind: contract.KindCommand, Source: serviceName(s.Name), Target: "discovery", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: body}); err != nil {
			return err
		}
	}
	return nil
}

func cataloguedTestEventType(eventType string) bool {
	for _, item := range contract.TestInferenceCatalog() {
		if item.EventType == eventType {
			return true
		}
	}
	return false
}

func (s *Service) sendTestDecision(event contract.Event, result ProcessResult) error {
	decisionPayload, err := json.Marshal(struct {
		SchemaVersion          string   `json:"schema_version"`
		Revision               uint64   `json:"revision"`
		Decision               Decision `json:"decision"`
		PhysicalActionExecuted bool     `json:"physical_action_executed"`
	}{"core-decision/v1", result.Result.Revision, result.Commit.Decision, false})
	if err != nil {
		return err
	}
	return s.Bus.Send(contract.Message{ID: event.ID + ":decision:api", Type: "core.decision", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: event.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: decisionPayload})
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil || s.Bus == nil || s.Core == nil {
		return fmt.Errorf("cognitive core service is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message, ok := <-s.Bus.SubscribeChannel("core"):
			if !ok {
				return nil
			}
			if err := s.Handle(ctx, message); err != nil {
				return err
			}
		}
	}
}

func EventFromMessage(message contract.Message) contract.Event {
	timestamp := message.Timestamp.UTC()
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	var payload map[string]any
	_ = json.Unmarshal(message.Payload, &payload)
	return contract.Event{ID: message.ID, Type: message.Type, Source: message.Source, Timestamp: timestamp, Payload: payload, Priority: message.Priority, TrackID: message.TrackID, ActivationID: message.CorrelationID, Epoch: message.Epoch, Sequence: message.Sequence}
}
func serviceName(name string) string {
	if name == "" {
		return "core"
	}
	return name
}
