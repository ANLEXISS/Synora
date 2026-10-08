package cognitivecore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"synora/pkg/contract"
)

// ServiceV3 is the bus-owned candidate Core service. The test harness reaches
// it through the same Unix bus envelope as every other Core input; it never
// calls CoreV3 directly from the injector.
type ServiceV3 struct {
	Bus  Bus
	Core *CoreV3
	Name string
	Now  func() time.Time
}

type v3BusEnvelope struct {
	Test                 bool   `json:"test"`
	Provenance           string `json:"provenance"`
	EventType            string `json:"event_type"`
	SimulatedCamera      bool   `json:"simulated_camera"`
	VisionStatus         string `json:"vision_status"`
	VisionEvidenceSource string `json:"vision_evidence_source"`
	InferenceExecuted    bool   `json:"inference_executed"`
	WorkerResult         struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"worker_result"`
	Snapshot CognitiveSnapshotV3 `json:"snapshot"`
}

func (s *ServiceV3) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *ServiceV3) Handle(ctx context.Context, message contract.Message) error {
	if s == nil || s.Bus == nil || s.Core == nil {
		return fmt.Errorf("V3 cognitive core service is not configured")
	}
	if message.Type == contract.EventActionResult || message.Type == "discovery.action.result" {
		var payload map[string]any
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			return fmt.Errorf("decode V3 action result: %w", err)
		}
		event := contract.Event{ID: message.ID, Type: message.Type, Source: message.Source, Timestamp: message.Timestamp.UTC(), Payload: payload}
		if event.Timestamp.IsZero() {
			event.Timestamp = s.now()
		}
		result, err := s.Core.RecordActionResult(event)
		status := "recorded"
		if err != nil {
			status = "rejected"
		} else if result.Duplicate {
			status = "duplicate"
		}
		body, marshalErr := json.Marshal(map[string]any{"schema_version": "core-action-result/v1", "status": status, "revision": result.Revision, "request_id": ActionResultRequestID(payload)})
		if marshalErr != nil {
			return marshalErr
		}
		if sendErr := s.Bus.Send(contract.Message{ID: message.ID + ":action-result", Type: "core.action_result", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: message.ID, Revision: result.Revision, Timestamp: s.now(), Payload: body}); sendErr != nil {
			return sendErr
		}
		// An orphan or duplicate result is an explicit harness-observable
		// rejection/idempotence outcome, not a reason to restart Core.
		return nil
	}
	if message.Type != contract.EventVisionEnrichmentV3 && message.Type != contract.EventValidationTestInference {
		return nil
	}
	var envelope v3BusEnvelope
	if err := json.Unmarshal(message.Payload, &envelope); err != nil {
		return fmt.Errorf("decode V3 bus envelope: %w", err)
	}
	if envelope.Provenance == "simulated_test_worker" {
		if os.Getenv("SYNORA_TEST_VISION_WORKER") != "1" || !envelope.SimulatedCamera ||
			envelope.VisionStatus != "unavailable" || envelope.VisionEvidenceSource != "simulated_test_worker" || envelope.InferenceExecuted ||
			envelope.WorkerResult.Status != "unavailable" || envelope.WorkerResult.Reason == "" {
			return fmt.Errorf("invalid simulated test worker provenance")
		}
		envelope.Snapshot.SimulatedCamera = true
		envelope.Snapshot.VisionStatus = "unavailable"
		envelope.Snapshot.VisionEvidenceSource = "simulated_test_worker"
		envelope.Snapshot.InferenceExecuted = false
	}
	if message.Type == contract.EventValidationTestInference && (!envelope.Test || envelope.Provenance != "test-harness") {
		return fmt.Errorf("invalid V3 test-harness envelope")
	}
	if envelope.EventType == "" {
		envelope.EventType = contract.EventVisionEnrichmentV3
	}
	eventPayload := map[string]any{"schema_version": contract.EventVisionEnrichmentV3}
	if envelope.SimulatedCamera {
		eventPayload["simulated_camera"] = true
		eventPayload["vision_status"] = envelope.VisionStatus
		eventPayload["vision_evidence_source"] = envelope.VisionEvidenceSource
		eventPayload["inference_executed"] = false
	}
	event := contract.Event{ID: message.ID, Type: envelope.EventType, Source: "discovery", Timestamp: message.Timestamp.UTC(), Payload: eventPayload}
	if event.Timestamp.IsZero() {
		event.Timestamp = s.now()
	}
	result, err := s.Core.Process(ctx, event, envelope.Snapshot)
	if err != nil {
		return err
	}
	if result.Result.Duplicate {
		return nil
	}
	decisionPayload, err := json.Marshal(struct {
		SchemaVersion          string     `json:"schema_version"`
		Revision               uint64     `json:"revision"`
		Decision               DecisionV3 `json:"decision"`
		SimulatedCamera        bool       `json:"simulated_camera"`
		PhysicalActionExecuted bool       `json:"physical_action_executed"`
		AudioRendered          bool       `json:"audio_rendered"`
	}{"core-decision/v3", result.Result.Revision, *result.Commit.DecisionV3, result.Commit.SnapshotV3.SimulatedCamera, false, false})
	if err != nil {
		return err
	}
	if err := s.Bus.Send(contract.Message{ID: message.ID + ":decision:v3", Type: "core.decision.v3", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: message.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: decisionPayload}); err != nil {
		return err
	}
	snapshotPayload, err := json.Marshal(struct {
		SchemaVersion          string              `json:"schema_version"`
		Revision               uint64              `json:"revision"`
		Snapshot               CognitiveSnapshotV3 `json:"snapshot"`
		SimulatedCamera        bool                `json:"simulated_camera"`
		PhysicalActionExecuted bool                `json:"physical_action_executed"`
		AudioRendered          bool                `json:"audio_rendered"`
	}{"core-snapshot/v3", result.Result.Revision, *result.Commit.SnapshotV3, result.Commit.SnapshotV3.SimulatedCamera, false, false})
	if err != nil {
		return err
	}
	if err := s.Bus.Send(contract.Message{ID: message.ID + ":snapshot:v3", Type: "core.snapshot.v3", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: message.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: snapshotPayload}); err != nil {
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
		return s.Bus.Send(contract.Message{ID: result.Result.Action.RequestID + ":request", Type: contract.EventActionRequest, Kind: contract.KindCommand, Source: serviceName(s.Name), Target: "discovery", CorrelationID: message.ID, Revision: result.Result.Revision, Timestamp: s.now(), Payload: body})
	}
	return nil
}

func (s *ServiceV3) Run(ctx context.Context) error {
	if s == nil || s.Bus == nil || s.Core == nil {
		return fmt.Errorf("V3 cognitive core service is not configured")
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
				log.Printf("V3 core service rejected message type=%s id=%s: %v", message.Type, message.ID, err)
				return err
			}
		}
	}
}
