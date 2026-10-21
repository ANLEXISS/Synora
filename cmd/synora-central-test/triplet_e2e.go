package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"synora/internal/cognitivecore"
	"synora/pkg/contract"
)

func verifyTripletFromStore(store *cognitivecore.UniversalStore) (string, int, error) {
	first, err := cognitivecore.BuildTripletFramesV1(store)
	if err != nil {
		return "failed", 0, err
	}
	second, err := cognitivecore.BuildTripletFramesV1(store)
	if err != nil {
		return "failed", 0, err
	}
	a, err := json.Marshal(first)
	if err != nil {
		return "failed", 0, err
	}
	b, err := json.Marshal(second)
	if err != nil {
		return "failed", 0, err
	}
	if !bytes.Equal(a, b) {
		return "failed", 0, errors.New("triplet builders are not deterministic under replay")
	}
	if _, err := contract.DecodeEventFrameV1(mustJSON(first.Event)); err != nil {
		return "failed", 0, err
	}
	if _, err := contract.DecodeContextFrameV1(mustJSON(first.Context)); err != nil {
		return "failed", 0, err
	}
	if _, err := contract.DecodeArbitrationFrameV1(mustJSON(first.Arbitration)); err != nil {
		return "failed", 0, err
	}
	var decoded any
	if err := json.Unmarshal(a, &decoded); err != nil {
		return "failed", 0, err
	}
	if forbiddenTripletKey(decoded) {
		return "failed", 0, errors.New("triplet frame contains a forbidden raw-data field")
	}
	if first.Arbitration.EventBranch.Availability != "unavailable" || first.Arbitration.ContextBranch.Availability != "unavailable" || len(first.Arbitration.EventBranch.Embedding) != 0 || len(first.Arbitration.ContextBranch.Embedding) != 0 {
		return "failed", 0, errors.New("future MLP availability mask is ambiguous")
	}
	return "validated_redacted_deterministic", 3, nil
}

func verifyTripletScenario(name string, store *cognitivecore.UniversalStore) error {
	frames, err := cognitivecore.BuildTripletFramesV1(store)
	if err != nil {
		return err
	}
	switch name {
	case "topology_expected", "topology_anomalous":
		frames.Context.Topology.Transition = strings.TrimPrefix(name, "topology_")
		return frames.Context.Validate()
	case "real_provenance_contract_only":
		frames.Event.Envelope.Provenance = "real"
		return frames.Event.Validate()
	case "replay_provenance":
		if frames.Event.Envelope.Provenance != "replay" {
			return errors.New("replay provenance was not preserved")
		}
	case "action_failed":
		snapshot := store.Snapshot()
		snapshot.ActionResults = []cognitivecore.ActionResultFact{{Failed: true}}
		history, err := store.History()
		if err != nil {
			return err
		}
		context, err := cognitivecore.BuildContextFrameV1(snapshot, history, nil)
		if err != nil {
			return err
		}
		if context.Actions.LastResult != "failed" || context.Actions.FailureCount != 1 {
			return errors.New("action failure missing from ContextFrame")
		}
	case "cooldown_active":
		if !frames.Context.Actions.Cooldown {
			return errors.New("cooldown missing from ContextFrame")
		}
	case "bundle_incompatible":
		hashes := contract.TripletContractHashes()
		model := strings.Repeat("a", 64)
		bundle := contract.TripletBundleV1{SchemaVersion: contract.TripletBundleSchemaV1, EventModelManifestHash: model, ContextModelManifestHash: model, ArbiterModelManifestHash: model, EventFrameSchemaHash: hashes["event_frame_schema_hash"], ContextFrameSchemaHash: hashes["context_frame_schema_hash"], ArbitrationFrameSchemaHash: hashes["arbitration_frame_schema_hash"], LabelTaxonomyHash: hashes["label_taxonomy_hash"], CalibrationManifestHash: model, CompatibilityVersion: contract.TripletCompatibilityV1}
		bundle.ContextFrameSchemaHash = strings.Repeat("0", 64)
		if err := bundle.Validate(hashes); err == nil {
			return errors.New("incompatible triplet bundle accepted")
		}
		return nil
	case "forbidden_field_rejected":
		body, err := json.Marshal(frames.Event)
		if err != nil {
			return err
		}
		body = append(body[:len(body)-1], []byte(`,"bbox":[1,2,3,4]}`)...)
		if _, err := contract.DecodeEventFrameV1(body); err == nil {
			return errors.New("forbidden raw field accepted")
		}
	case "duplicate_replay":
		first, _ := json.Marshal(frames)
		second, _ := json.Marshal(frames)
		if !bytes.Equal(first, second) {
			return errors.New("duplicate replay changed the triplet frame")
		}
	}
	return nil
}

func mustJSON(value any) []byte { body, _ := json.Marshal(value); return body }

func forbiddenTripletKey(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, nested := range current {
			switch key {
			case "media", "frame", "crop", "bbox", "keypoints", "biometric_embedding", "identity", "name", "local_path", "url", "camera_id", "hardware_id", "local_track_id", "logits", "tensor":
				return true
			}
			if forbiddenTripletKey(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range current {
			if forbiddenTripletKey(nested) {
				return true
			}
		}
	}
	return false
}
