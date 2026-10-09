package cognitivecore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// InitialState carries only non-Vision runtime context already owned by
	// Core/Store. The Evidence V1 event remains the sole Vision input.
	InitialState *CognitiveSnapshotV3
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
	VisionEvidence *contract.VisionEvidenceV1 `json:"vision_evidence,omitempty"`
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
	if message.Kind == contract.KindRPC && message.Type == RPCResidentGallery {
		// Resident/gallery administration is a non-Vision Core/Store RPC shared
		// by the nominal and V3 services. Reuse the same Universal Store without
		// introducing gallery fields into the V3 CognitiveSnapshot.
		sharedStore := &Core{Store: s.Core.Store}
		return (&Service{Bus: s.Bus, Core: sharedStore, Name: s.Name, Now: s.Now}).handleResidentGalleryRPC(message)
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
	if message.Type != contract.EventVisionEvidenceV1 && message.Type != contract.EventValidationTestInference {
		if isLegacyVisionContract(message.Type) {
			return rejectLegacyVisionV3(s, message)
		}
		return nil
	}
	var envelope v3BusEnvelope
	var evidence contract.VisionEvidenceV1
	var snapshot CognitiveSnapshotV3
	if message.Type == contract.EventVisionEvidenceV1 {
		if message.Source != "discovery" {
			return fmt.Errorf("Vision Evidence V1 source must be Discovery")
		}
		decoded, err := contract.DecodeVisionEvidenceV1(message.Payload)
		if err != nil {
			return fmt.Errorf("decode Vision Evidence V1: %w", err)
		}
		evidence = decoded
	} else if err := decodeStrictV3Envelope(message.Payload, &envelope); err != nil {
		return fmt.Errorf("decode V3 test envelope: %w", err)
	}
	if envelope.Provenance == "simulated_test_worker" {
		if os.Getenv("SYNORA_TEST_VISION_WORKER") != "1" || !envelope.SimulatedCamera ||
			envelope.VisionStatus != "unavailable" || envelope.VisionEvidenceSource != "simulated_test_worker" || envelope.InferenceExecuted ||
			envelope.WorkerResult.Status != "unavailable" || envelope.WorkerResult.Reason == "" {
			return fmt.Errorf("invalid simulated test worker provenance")
		}
	}
	if message.Type == contract.EventValidationTestInference {
		if !envelope.Test || envelope.Provenance != "test-harness" || envelope.EventType != contract.EventVisionEvidenceV1 || envelope.VisionEvidence == nil {
			return fmt.Errorf("invalid V1-only test-harness envelope")
		}
		evidence = *envelope.VisionEvidence
	}
	if err := evidence.Validate(); err != nil {
		return fmt.Errorf("invalid V1 aggregate Vision evidence: %w", err)
	}
	snapshot = CognitiveSnapshotV3{CapturedAt: evidence.WindowEnd.UTC(), SimulatedCamera: evidence.SimulatedCamera}
	if s.InitialState != nil {
		snapshot = *s.InitialState
		snapshot.CapturedAt = evidence.WindowEnd.UTC()
		snapshot.SimulatedCamera = evidence.SimulatedCamera
		snapshot.VisionEvidence = nil
		snapshot.VisionEvidenceProjection = VisionEvidenceProjectionV3{}
	}
	snapshot.BaseV2.Topology = evidence.Topology
	snapshot.BaseV2.Presence.HumanPresent = evidence.Presence.Human.State == "present"
	snapshot.BaseV2.Presence.TrackCount = evidence.Presence.HumanTrackCount
	snapshot.BaseV2.Presence.TrackConfirmed = evidence.Presence.ConfirmedHumanTracks > 0
	mapped, err := snapshot.ApplyVisionEvidenceV1(evidence)
	if err != nil {
		return fmt.Errorf("invalid V1 aggregate Vision evidence: %w", err)
	}
	snapshot = mapped
	if message.Type == contract.EventValidationTestInference && evidence.SimulatedCamera != (envelope.Provenance == "test-harness" && envelope.SimulatedCamera) {
		return fmt.Errorf("test evidence provenance disagrees with harness")
	}
	event := contract.Event{ID: evidence.EventID, Type: contract.EventVisionEvidenceV1, Source: "discovery", Timestamp: evidence.WindowEnd.UTC(), Payload: map[string]any{"simulated_camera": evidence.SimulatedCamera}}
	if event.Timestamp.IsZero() {
		event.Timestamp = s.now()
	}
	result, err := s.Core.Process(ctx, event, snapshot)
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

func rejectLegacyVisionV3(s *ServiceV3, message contract.Message) error {
	body, err := json.Marshal(map[string]any{"schema_version": "core.vision-ingress-result/v1", "status": "rejected", "reason": "legacy_vision_contract_not_admitted", "legacy_to_core_attempts": 1})
	if err != nil {
		return err
	}
	return s.Bus.Send(contract.Message{ID: message.ID + ":legacy-rejected", Type: "core.vision_rejected", Kind: contract.KindEvent, Source: serviceName(s.Name), Target: "api", CorrelationID: message.ID, Timestamp: s.now(), Payload: body})
}

func decodeStrictV3Envelope(payload []byte, out *v3BusEnvelope) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("trailing V3 test envelope data")
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
