package main

// Generated scenarios are declarative expansions of the fixed manifest.  They
// do not use wall clock, random values, media or training data.  The manifest
// supplies the family, count and seed; this file only expands those semantic
// dimensions into the existing bus fixture contract.

import (
	"encoding/json"
	"fmt"
)

func generatedScenarioCount(suites []generatedSuite) int {
	total := 0
	for _, suite := range suites {
		if suite.Count > 0 {
			total += suite.Count
		}
	}
	return total
}

func generatedFixture(spec generatedSuite, index int) fixture {
	if spec.Bundle == "" {
		spec.Bundle = "v3"
	}
	if spec.Suite == "" {
		spec.Suite = spec.Family
	}
	id := fmt.Sprintf("%s-%03d", spec.IDPrefix, index+1)
	clock := "2026-01-01T00:00:00Z"
	seeded := int(index) + int(spec.Seed%997)

	poseStatus := []string{"unavailable", "not_requested", "low_quality", "available"}[seeded%4]
	posture := []string{"unknown", "upright", "seated", "ground"}[seeded%4]
	if poseStatus != "available" {
		posture = "unknown"
	}
	// The optional RTMPose family is unavailable on this host because no
	// converted RKNN artifact exists. These cases must not manufacture success.
	if spec.Family == "rtmpose_runtime" {
		poseStatus = "unavailable"
		posture = "unknown"
	}
	motion := []string{"unknown", "still", "normal", "rapid", "very_rapid"}[seeded%5]
	fall := "none"
	transition := false
	if poseStatus == "available" && posture == "ground" && seeded%7 == 0 {
		fall = "candidate"
		transition = true
	}
	interaction := "none"
	physicalInteractionCandidate := false
	if seeded%11 == 0 {
		interaction = "candidate"
		physicalInteractionCandidate = true
	}

	faceStatus := []string{"not_requested", "unavailable", "low_quality", "uncertain", "recognized", "unknown"}[seeded%6]
	faceConfidence := float64(0)
	faceQuality := float64(0)
	if faceStatus == "recognized" {
		faceConfidence, faceQuality = .91, .93
	} else if faceStatus == "uncertain" {
		faceConfidence, faceQuality = .42, .48
	} else if faceStatus == "low_quality" {
		faceQuality = .25
	}

	cameraHealth := []string{"healthy", "degraded", "offline", "uncertain", "unknown"}[seeded%5]
	cameraIntegrity := []string{"healthy", "healthy", "degraded", "uncertain", "unknown"}[seeded%5]
	communication := map[string]any{"announce_available": true, "cooldown_active": false, "tts_status": "available"}
	if spec.Family == "communication_gate" {
		switch seeded % 5 {
		case 1:
			communication["cooldown_active"] = true
		case 2:
			communication["tts_status"] = "unavailable"
		case 3:
			communication["announce_available"] = false
		}
	}

	baseVision := map[string]any{
		"fall_state": "none", "pose_quality": .9, "pose_status": "available", "posture": "standing",
		"recovery_observed": false, "risk_confidence": 0.0, "risk_kind": "none",
		"risk_persistence": "none", "risk_status": "not_available", "identity_status": "unknown",
	}
	vision := map[string]any{
		"central_retracking_invocations": 0, "camera_health_status": cameraHealth,
		"camera_integrity_status": cameraIntegrity, "camera_uncertainty": cameraHealth != "healthy",
		"edge_tracking_ok": cameraHealth == "healthy" || cameraHealth == "degraded",
		"face_confidence":  faceConfidence, "face_consensus_frames": 2,
		"face_expires_at": clock, "face_qualification_provenance": "not_qualified",
		"face_quality": faceQuality, "face_status": faceStatus,
		"fall_state": fall, "transition_to_ground": transition, "ground_duration_seconds": float64(seeded%12) / 2,
		"interaction_state": interaction, "physical_interaction_candidate": physicalInteractionCandidate,
		"immobility_seconds": float64(seeded % 10), "motion_tier": motion,
		"pose_observation_count": 2, "pose_quality": map[string]float64{"available": .9, "low_quality": .3}[poseStatus],
		"pose_sampled": poseStatus == "available", "pose_status": poseStatus, "posture": posture,
		"real_detection": false, "recovery_observed": seeded%13 == 0, "replay_simulation": true,
		"risk_confidence": 0.0, "risk_kind": "none", "risk_observation_count": 0,
		"risk_persistence": "none", "risk_quality_sufficient": true, "risk_status": "not_available",
	}
	if spec.Family == "camera_ingress_j2" || spec.Family == "camera_health_j2" {
		state, source := cameraJ2Expected(index, spec.Family)
		vision["camera_health_detail_state"] = state
		vision["camera_health_detail_provenance"] = "simulated_test"
		vision["camera_health_detail_confidence"] = .9
		vision["camera_health_detail_quality"] = .85
		if state == "stream_missing" || state == "unavailable" {
			vision["camera_health_detail_confidence"] = 0.0
			vision["camera_health_detail_quality"] = 0.0
		}
		vision["camera_health_j2_case"] = source
		vision["simulated_camera"] = true
	}
	if poseStatus == "available" && posture == "upright" {
		vision["pose_quality"] = .9
	} else if poseStatus == "low_quality" {
		vision["pose_quality"] = .3
	}

	snapshot := map[string]any{
		"schema_version": "cognitive-snapshot/v3", "captured_at": clock,
		"base_v2": map[string]any{
			"schema_version": "cognitive-snapshot/v2", "captured_at": clock,
			"topology": "protected_interior", "communication": communication,
			"vision": baseVision,
		},
		"vision": vision,
	}
	payload := map[string]any{"event_type": "synora.vision.enrichment/v3", "provenance": "test-harness", "snapshot": snapshot, "test": true}
	body, _ := json.Marshal(payload)
	value := fixture{
		ID: id, Suite: spec.Suite, Clock: clock, Bundle: spec.Bundle,
		SimulatedCamera: spec.Family == "camera_ingress_j2" || spec.Family == "camera_health_j2",
		Capabilities:    []string{"announce", "record"},
		Messages:        []fixtureMessage{{ID: "msg-" + id, Type: "synora.vision.enrichment/v3", Timestamp: clock, Payload: body}},
		Expected: fixtureExpected{
			Discovery:  map[string]any{"status": "accepted"},
			Snapshot:   map[string]any{"schema_version": "cognitive-snapshot/v3", "input_dimension": float64(86)},
			MLP:        map[string]any{"status": "available", "probability_contract": "normalized", "heads": []any{"danger", "incident", "task", "action", "communication_intent"}},
			SafetyGate: map[string]any{"physical_action_executed": false},
			Store:      map[string]any{"committed": true}, Outbox: map[string]any{"non_empty": false},
			Forbidden: []string{"frame", "image", "media", "bbox", "crop", "keypoints", "embedding", "identity", "local_track_id"},
		},
	}
	if spec.Family == "resident_gallery" {
		value.GalleryScenario = residentGalleryScenario(index)
	}
	if spec.Family == "face_gallery_policy" {
		value.GalleryScenario = faceGalleryScenario(index)
	}
	return value
}

var cameraIngressJ2Cases = []struct{ name, state string }{
	{"source_healthy_ingress", "healthy"}, {"startup_source_absent", "stream_missing"},
	{"stream_loss_after_healthy", "stream_missing"}, {"stream_recovered", "recovered"},
	{"malformed_source", "unavailable"}, {"worker_timeout", "unavailable"},
	{"queue_backpressure", "degraded"}, {"duplicate_idempotent", "healthy"},
	{"discovery_restart_recovery", "recovered"},
}

var cameraHealthJ2Cases = []struct{ name, state string }{
	{"frozen_stream", "frozen_frame"}, {"black_or_occluded", "black_or_occluded"},
	{"degraded_cadence", "degraded"}, {"invalid_evidence_rejected", "unavailable"},
	{"vision_backend_unavailable", "unavailable"}, {"loss_recovery_cycle", "recovered"},
	{"health_without_signal", "stream_missing"}, {"safety_gate_dry_run", "healthy"},
	{"production_isolation", "healthy"},
}

func cameraJ2Expected(index int, family string) (state, name string) {
	cases := cameraIngressJ2Cases
	if family == "camera_health_j2" {
		cases = cameraHealthJ2Cases
	}
	if index < 0 || index >= len(cases) {
		return "unavailable", "invalid_case_index"
	}
	return cases[index].state, cases[index].name
}

func residentGalleryScenario(index int) string {
	cases := []string{
		"resident_create_scoped", "resident_no_token", "resident_scope_missing_write",
		"resident_scope_missing_manage", "resident_status_redacted", "resident_logical_delete",
		"resident_create_idempotent",
	}
	if index < 0 || index >= len(cases) {
		return ""
	}
	return cases[index]
}

func faceGalleryScenario(index int) string {
	cases := []string{
		"face_attestation_missing", "face_attestation_expired", "face_attestation_revoked",
		"face_source_not_allowlisted", "face_backend_unavailable", "face_score_0649",
		"face_score_0650", "face_score_0899", "face_score_0900_no_consensus",
		"face_consensus_synthetic", "face_multi_faces", "face_quality_low", "face_quota",
		"face_candidate_ttl", "face_generation_rollback", "face_forbidden_payload",
	}
	if index < 0 || index >= len(cases) {
		return ""
	}
	return cases[index]
}
