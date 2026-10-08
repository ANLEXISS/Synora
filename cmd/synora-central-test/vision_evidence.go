package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"synora/pkg/contract"
)

// attachFixtureVisionEvidenceV1 translates harness-only aggregate V3 inputs
// into the shared V1 evidence envelope. It never reads media or raw Vision
// payloads; the source fixture already contains only aggregate fields.
func attachFixtureVisionEvidenceV1(payload []byte, id string, at time.Time, simulated bool) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("decode fixture aggregate: %w", err)
	}
	snapshot, ok := envelope["snapshot"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fixture aggregate snapshot missing")
	}
	if capturedAt, ok := snapshot["captured_at"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, capturedAt); err == nil {
			at = parsed.UTC()
		}
	}
	vision, _ := snapshot["vision"].(map[string]any)
	base, _ := snapshot["base_v2"].(map[string]any)
	baseVision, _ := base["vision"].(map[string]any)
	poseStatus := stringMap(vision, "pose_status", stringMap(baseVision, "pose_status", "unavailable"))
	poseAvailability, posture := contract.VisionUnavailable, "unknown"
	switch poseStatus {
	case "not_requested":
		poseAvailability = contract.VisionNotRequested
	case "available", "low_quality":
		poseAvailability = contract.VisionEvaluated
	}
	switch stringMap(vision, "posture", stringMap(baseVision, "posture", "unknown")) {
	case "upright", "standing":
		posture = contract.VisionPostureUpright
	case "seated", "sitting":
		posture = contract.VisionPostureSeated
	case "reclined", "lying":
		posture = contract.VisionPostureReclined
	case "ground":
		posture = contract.VisionPostureGround
	case "ambiguous":
		posture = contract.VisionPostureAmbiguous
	}
	quality := numberMap(vision, "pose_quality", numberMap(baseVision, "pose_quality", 0))
	count := int(numberMap(vision, "pose_observation_count", 0))
	if poseAvailability == contract.VisionEvaluated && count < 1 {
		count = 1
	}
	transitionConfidence := 0.0
	if boolMap(vision, "transition_to_ground") || boolMap(baseVision, "transition_to_ground") {
		transitionConfidence = 1
	}
	immobility := numberMap(vision, "immobility_seconds", numberMap(baseVision, "immobility_seconds", 0))
	windowSeconds := math.Max(1, immobility)
	if windowSeconds > 86400 {
		windowSeconds = 86400
		immobility = 86400
	}
	start := at.UTC().Add(-time.Duration(windowSeconds * float64(time.Second)))
	if at.IsZero() {
		at = time.Now().UTC()
		start = at.Add(-time.Duration(windowSeconds * float64(time.Second)))
	}
	groundDuration := numberMap(vision, "ground_duration", numberMap(vision, "ground_duration_seconds", 0))
	riskPersistenceSeconds := numberMap(vision, "risk_persistence_seconds", 0)
	windowSeconds = math.Max(windowSeconds, math.Max(groundDuration, riskPersistenceSeconds))
	if windowSeconds > 86400 {
		return nil, fmt.Errorf("fixture aggregate support exceeds one day")
	}
	start = at.UTC().Add(-time.Duration(windowSeconds * float64(time.Second)))
	digest := sha256.Sum256([]byte(id))
	provenance := "replay"
	if simulated {
		provenance = "simulated_test"
	}
	human := measureV1(contract.VisionUnavailable, "unknown", 0, 0, 0, 0, 0, "unknown")
	if immobility > 0 {
		human = measureV1(contract.VisionEvaluated, "present", .5, .5, 1, 0, 0, "continuous")
	}
	unknownPresence := measureV1(contract.VisionUnavailable, "unknown", 0, 0, 0, 0, 0, "unknown")
	poseSupport := contract.VisionSupportV1{Continuity: "unknown"}
	poseConfidence := 0.0
	poseQuality := 0.0
	if poseAvailability == contract.VisionEvaluated {
		poseSupport = contract.VisionSupportV1{ValidEvaluations: count, Continuity: "continuous"}
		poseConfidence, poseQuality = quality, quality
	}
	fallState := stringMap(vision, "fall_state", stringMap(baseVision, "fall_state", "unknown"))
	motion := stringMap(vision, "motion_tier", "unknown")
	activityState := motion
	if activityState == "very_rapid" { /* preserved by the aggregate contract */
	}
	activityAvailability := contract.VisionUnavailable
	activitySupport := contract.VisionSupportV1{Continuity: "unknown"}
	activityQuality := numberMap(vision, "aggregate_confidence", 0)
	if motion == "still" || motion == "normal" || motion == "rapid" || motion == "very_rapid" {
		activityAvailability = contract.VisionEvaluated
		activitySupport = contract.VisionSupportV1{ValidEvaluations: maxIntValue(count, 1), Continuity: "continuous"}
	}
	if activityAvailability != contract.VisionEvaluated {
		activityState, activityQuality = "unknown", 0
	}
	riskStatus := stringMap(vision, "risk_status", stringMap(baseVision, "risk_status", "not_available"))
	riskKind := stringMap(vision, "risk_kind", stringMap(baseVision, "risk_kind", "none"))
	riskPersistence := stringMap(vision, "risk_persistence", "none")
	faceStatus := stringMap(vision, "face_status", "unavailable")
	faceProvenance := stringMap(vision, "face_qualification_provenance", "none")
	switch faceProvenance {
	case "not_qualified":
		faceProvenance = "none"
	case "controlled_test":
		faceProvenance = "test_only"
	case "labeled_consent_manifest":
		faceProvenance = "validated_dataset"
	}
	interactionState := stringMap(vision, "interaction_state", "none")
	cameraIntegrity := stringMap(vision, "camera_integrity_status", "unknown")
	runtimeAggregate := &contract.VisionRuntimeAggregateV1{
		FallState: fallState, RecoveryObserved: boolMap(vision, "recovery_observed") || boolMap(baseVision, "recovery_observed"),
		GroundDurationSeconds: groundDuration,
		RiskStatus:            riskStatus, RiskKind: riskKind, RiskConfidence: numberMap(vision, "risk_confidence", numberMap(baseVision, "risk_confidence", 0)),
		RiskPersistence: riskPersistence, RiskPersistenceSeconds: riskPersistenceSeconds,
		RiskObservationCount: int(numberMap(vision, "risk_observation_count", 0)), RiskQualitySufficient: boolMap(vision, "risk_quality_sufficient"),
		MotionTier: motion, InteractionState: interactionState, PhysicalInteractionCandidate: boolMap(vision, "physical_interaction_candidate"),
		FaceStatus: faceStatus, FaceConsensusFrames: int(numberMap(vision, "face_consensus_frames", 0)),
		FaceQuality: numberMap(vision, "face_quality", 0), FaceConfidence: numberMap(vision, "face_confidence", 0), FaceQualificationProvenance: faceProvenance,
		CameraIntegrityStatus: cameraIntegrity, CameraUncertainty: boolMap(vision, "camera_uncertainty"),
		AggregateConfidence: numberMap(vision, "aggregate_confidence", 0), EdgeTrackingOK: boolMap(vision, "edge_tracking_ok"),
		RealDetection: boolMap(vision, "real_detection"), ReplaySimulation: boolMap(vision, "replay_simulation"),
	}
	evidence := contract.VisionEvidenceV1{
		SchemaVersion: contract.EventVisionEvidenceV1, EventID: "ev_" + hex.EncodeToString(digest[:12]), EpisodeID: "ep_" + hex.EncodeToString(digest[12:24]),
		WindowStart: start, WindowEnd: at.UTC(), WindowSeconds: windowSeconds, Topology: stringMap(base, "topology", "unknown"), Provenance: provenance, SimulatedCamera: simulated,
		CameraHealth: measureV1(contract.VisionUnavailable, "unavailable", 0, 0, 0, 0, 0, "unknown"),
		Trigger:      measureV1(contract.VisionUnavailable, "unknown", 0, 0, 0, 0, 0, "unknown"),
		Presence:     contract.VisionPresenceV1{Human: human, Vehicle: unknownPresence, Animal: unknownPresence},
		Activity:     measureV1(activityAvailability, activityState, activityQuality, activityQuality, activitySupport.ValidEvaluations, activitySupport.GapCount, activitySupport.SupportedSeconds, activitySupport.Continuity),
		Pose:         contract.VisionPoseV1{Availability: poseAvailability, Posture: posture, PostureConfidence: poseConfidence, TransitionToGroundConfidence: transitionConfidence, Quality: poseQuality, Support: poseSupport, ImmobilitySeconds: immobility},
		Face:         semanticV1(contract.VisionNotRequested), Plate: semanticV1(contract.VisionNotRequested),
		Sensitive:      contract.VisionSensitiveV1{Availability: contract.VisionNotRequested, Category: "none", Support: contract.VisionSupportV1{Continuity: "unknown"}},
		Media:          contract.VisionMediaContinuityV1{Availability: contract.VisionUnavailable, EpisodeState: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}},
		ProducerHealth: "healthy", Processing: "partial",
		RuntimeAggregate: runtimeAggregate,
	}
	if simulated {
		evidence.ProducerHealth = "unavailable"
		evidence.Processing = "unavailable"
	}
	if evidence.Validate() != nil {
		return nil, evidence.Validate()
	}
	body, err := json.Marshal(evidence)
	if err != nil {
		return nil, fmt.Errorf("encode Evidence V1: %w", err)
	}
	return body, nil
}

func measureV1(a contract.VisionAvailabilityV1, state string, confidence, quality float64, evaluations, gaps int, seconds float64, continuity string) contract.VisionMeasureV1 {
	return contract.VisionMeasureV1{Availability: a, State: state, Confidence: confidence, Quality: quality, Support: contract.VisionSupportV1{ValidEvaluations: evaluations, GapCount: gaps, SupportedSeconds: seconds, Continuity: continuity}}
}
func semanticV1(a contract.VisionAvailabilityV1) contract.VisionSemanticResultV1 {
	return contract.VisionSemanticResultV1{Availability: a, Result: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}
}
func stringMap(value map[string]any, key, fallback string) string {
	if v, ok := value[key].(string); ok {
		if normalized := strings.TrimSpace(v); normalized != "" {
			return normalized
		}
	}
	return fallback
}
func numberMap(value map[string]any, key string, fallback float64) float64 {
	if v, ok := value[key].(float64); ok && v >= 0 {
		return v
	}
	return fallback
}
func boolMap(value map[string]any, key string) bool { v, _ := value[key].(bool); return v }
func maxIntValue(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
