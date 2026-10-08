package cognitivecore

import (
	"context"
	"fmt"
	"math"
	"time"

	"synora/pkg/contract"
)

// V3 is a candidate contract. Its first 64 values are produced by the
// immutable V2 encoder; the extension is intentionally appended so the V1
// nominal bundle and its feature positions cannot be silently reinterpreted.
const (
	SnapshotSchemaVersionV3 = "cognitive-snapshot/v3"
	EncoderSchemaVersionV3  = "cognitive-encoder/v3"
	CognitiveVectorSizeV3   = 86
)

const (
	PoseV3Unavailable  = "unavailable"
	PoseV3NotRequested = "not_requested"
	PoseV3LowQuality   = "low_quality"
	PoseV3Available    = "available"

	PostureV3Unknown  = "unknown"
	PostureV3Upright  = "upright"
	PostureV3Seated   = "seated"
	PostureV3Reclined = "reclined"
	PostureV3Ground   = "ground"

	FallV3None      = "none"
	FallV3Candidate = "candidate"
	FallV3Confirmed = "confirmed"
	FallV3Unknown   = "unknown"

	MotionV3Unknown          = "unknown"
	MotionV3Still            = "still"
	MotionV3Normal           = "normal"
	MotionV3Rapid            = "rapid"
	MotionV3VeryRapid        = "very_rapid"
	InteractionV3None        = "none"
	InteractionV3Candidate   = "candidate"
	InteractionV3Unavailable = "unavailable"

	FaceV3NotRequested = "not_requested"
	FaceV3Unavailable  = "unavailable"
	FaceV3LowQuality   = "low_quality"
	FaceV3Uncertain    = "uncertain"
	FaceV3Recognized   = "recognized"
	FaceV3Unknown      = "unknown"

	CameraHealthV3Unknown   = "unknown"
	CameraHealthV3Healthy   = "healthy"
	CameraHealthV3Degraded  = "degraded"
	CameraHealthV3Offline   = "offline"
	CameraHealthV3Uncertain = "uncertain"

	RiskPersistenceNone       = "none"
	RiskPersistenceIsolated   = "isolated"
	RiskPersistenceRepeated   = "repeated"
	RiskPersistencePersistent = "persistent"
	RiskPersistenceConfirmed  = "confirmed"
)

// VisionSignalsV3 carries aggregate-only, edge-derived observations. The V3
// adapter keeps immobility_seconds semantically identical to the V2 prefix:
// it is the normalized duration of immobility of a confirmed human,
// irrespective of posture. PostureV3Ground carries ground state; it is not a
// reinterpretation of immobility_seconds and there is no ground-duration
// feature in V3.
type VisionSignalsV3 struct {
	PoseStatus             string  `json:"pose_status"`
	PoseQuality            float32 `json:"pose_quality"`
	Posture                string  `json:"posture"`
	FallState              string  `json:"fall_state"`
	RiskStatus             string  `json:"risk_status"`
	RiskKind               string  `json:"risk_kind"`
	RiskConfidence         float32 `json:"risk_confidence"`
	RiskPersistence        string  `json:"risk_persistence"`
	RiskPersistenceSeconds float32 `json:"risk_persistence_seconds"`
	RiskObservationCount   int     `json:"risk_observation_count"`
	RiskQualitySufficient  bool    `json:"risk_quality_sufficient"`
	PoseObservationCount   int     `json:"pose_observation_count"`
	PoseSampled            bool    `json:"pose_sampled"`
	TransitionToGround     bool    `json:"transition_to_ground"`
	ImmobilitySeconds      float32 `json:"immobility_seconds"`
	RecoveryObserved       bool    `json:"recovery_observed"`
	RealDetection          bool    `json:"real_detection"`
	ReplaySimulation       bool    `json:"replay_simulation"`
	AggregateConfidence    float32 `json:"aggregate_confidence"`
	EdgeTrackingOK         bool    `json:"edge_tracking_ok"`
	CentralRetracking      int     `json:"central_retracking_invocations"`
	// The fields below are aggregate-only contract facts. They deliberately
	// stay outside the immutable 86D encoder because V3 has no reserved
	// offsets for them; adding an offset requires a new encoder contract.
	GroundDurationSeconds        float32   `json:"ground_duration_seconds"`
	MotionTier                   string    `json:"motion_tier"`
	InteractionState             string    `json:"interaction_state"`
	PhysicalInteractionCandidate bool      `json:"physical_interaction_candidate"`
	FaceStatus                   string    `json:"face_status"`
	FaceConsensusFrames          int       `json:"face_consensus_frames"`
	FaceQuality                  float32   `json:"face_quality"`
	FaceConfidence               float32   `json:"face_confidence"`
	FaceExpiresAt                time.Time `json:"face_expires_at,omitempty"`
	FaceQualificationProvenance  string    `json:"face_qualification_provenance"`
	CameraHealthStatus           string    `json:"camera_health_status"`
	CameraIntegrityStatus        string    `json:"camera_integrity_status"`
	CameraUncertainty            bool      `json:"camera_uncertainty"`
}

// CognitiveSnapshotV3 keeps V2 as a nested, immutable base and adds only the
// signals that proved impossible to represent without changing V2 semantics.
type CognitiveSnapshotV3 struct {
	SchemaVersion            string                     `json:"schema_version"`
	Revision                 uint64                     `json:"revision"`
	CapturedAt               time.Time                  `json:"captured_at"`
	BaseV2                   CognitiveSnapshotV2        `json:"base_v2"`
	Vision                   VisionSignalsV3            `json:"vision"`
	SimulatedCamera          bool                       `json:"simulated_camera"`
	VisionStatus             string                     `json:"vision_status,omitempty"`
	VisionEvidenceSource     string                     `json:"vision_evidence_source,omitempty"`
	InferenceExecuted        bool                       `json:"inference_executed"`
	VisionEvidence           *contract.VisionEvidenceV1 `json:"vision_evidence,omitempty"`
	VisionEvidenceProjection VisionEvidenceProjectionV3 `json:"vision_evidence_projection"`
}

// VisionEvidenceProjectionV3 makes lossless-store versus frozen-vector
// projection explicit. Evidence remains authoritative even when V3 has no
// semantically equivalent learned offset for a fact.
type VisionEvidenceProjectionV3 struct {
	NotEncodedInSnapshotV3 bool     `json:"not_encoded_in_snapshot_v3"`
	Facts                  []string `json:"facts,omitempty"`
}

// ApplyVisionEvidenceV1 maps only semantics with a documented, existing V3
// feature slot. The full validated aggregate remains attached for Store/API
// observability; unmapped families are never squeezed into unrelated offsets.
func (s CognitiveSnapshotV3) ApplyVisionEvidenceV1(e contract.VisionEvidenceV1) (CognitiveSnapshotV3, error) {
	if err := e.Validate(); err != nil {
		return CognitiveSnapshotV3{}, err
	}
	if s.SimulatedCamera != e.SimulatedCamera && (s.SimulatedCamera || e.SimulatedCamera) {
		return CognitiveSnapshotV3{}, fmt.Errorf("snapshot and Vision Evidence V1 simulation markers disagree")
	}
	s.VisionEvidence = &e
	s.VisionEvidenceProjection = visionEvidenceProjectionV3(e)
	pose := e.Pose
	switch pose.Availability {
	case contract.VisionNotRequested:
		s.Vision.PoseStatus = PoseV3NotRequested
	case contract.VisionUnavailable:
		s.Vision.PoseStatus = PoseV3Unavailable
	case contract.VisionEvaluated:
		if pose.Quality == 0 {
			s.Vision.PoseStatus = PoseV3LowQuality
		} else {
			s.Vision.PoseStatus = PoseV3Available
		}
	}
	switch pose.Posture {
	case contract.VisionPostureUpright:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Upright, PostureStanding
	case contract.VisionPostureSeated:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Seated, PostureSitting
	case contract.VisionPostureReclined:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Reclined, PostureUnknown
	case contract.VisionPostureGround:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Ground, PostureLying
	case contract.VisionPostureAmbiguous:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Unknown, PostureUnknown
	default:
		s.Vision.Posture, s.BaseV2.Vision.Posture = PostureV3Unknown, PostureUnknown
	}
	quality := float32(pose.Quality)
	s.Vision.PoseQuality, s.BaseV2.Vision.PoseQuality = quality, quality
	s.Vision.PoseObservationCount = pose.Support.ValidEvaluations
	s.Vision.PoseSampled = pose.Availability == contract.VisionEvaluated && pose.Support.ValidEvaluations > 0
	immobility := float32(pose.ImmobilitySeconds)
	s.Vision.ImmobilitySeconds, s.BaseV2.Vision.ImmobilitySeconds = immobility, immobility
	if a := e.RuntimeAggregate; a != nil {
		s.Vision.FallState = a.FallState
		s.Vision.RecoveryObserved = a.RecoveryObserved
		s.Vision.GroundDurationSeconds = float32(a.GroundDurationSeconds)
		s.Vision.RiskStatus, s.BaseV2.Vision.RiskStatus = a.RiskStatus, a.RiskStatus
		s.Vision.RiskKind, s.BaseV2.Vision.RiskKind = a.RiskKind, a.RiskKind
		s.Vision.RiskConfidence, s.BaseV2.Vision.RiskConfidence = float32(a.RiskConfidence), float32(a.RiskConfidence)
		s.Vision.RiskPersistence = a.RiskPersistence
		s.Vision.RiskPersistenceSeconds = float32(a.RiskPersistenceSeconds)
		s.Vision.RiskObservationCount = a.RiskObservationCount
		s.Vision.RiskQualitySufficient = a.RiskQualitySufficient
		s.Vision.MotionTier = a.MotionTier
		s.Vision.InteractionState = a.InteractionState
		s.Vision.PhysicalInteractionCandidate = a.PhysicalInteractionCandidate
		s.Vision.FaceStatus = a.FaceStatus
		s.Vision.FaceConsensusFrames = a.FaceConsensusFrames
		s.Vision.FaceQuality = float32(a.FaceQuality)
		s.Vision.FaceConfidence = float32(a.FaceConfidence)
		switch a.FaceQualificationProvenance {
		case "validated_dataset":
			s.Vision.FaceQualificationProvenance = "labeled_consent_manifest"
		case "test_only":
			s.Vision.FaceQualificationProvenance = "controlled_test"
		case "not_qualified":
			s.Vision.FaceQualificationProvenance = "not_qualified"
		default:
			s.Vision.FaceQualificationProvenance = ""
		}
		s.Vision.CameraIntegrityStatus = a.CameraIntegrityStatus
		s.Vision.CameraUncertainty = a.CameraUncertainty
		s.Vision.AggregateConfidence = float32(a.AggregateConfidence)
		s.Vision.EdgeTrackingOK = a.EdgeTrackingOK
		s.Vision.RealDetection = a.RealDetection
		s.Vision.ReplaySimulation = a.ReplaySimulation
		s.BaseV2.Vision.FallState = a.FallState
		s.BaseV2.Vision.RecoveryObserved = a.RecoveryObserved
		s.BaseV2.Vision.RealDetection = a.RealDetection
		s.BaseV2.Vision.ReplaySimulation = a.ReplaySimulation
		s.BaseV2.Vision.AggregateConfidence = float32(a.AggregateConfidence)
		s.BaseV2.Vision.EdgeTrackingOK = a.EdgeTrackingOK
	}
	if e.SimulatedCamera {
		s.SimulatedCamera = true
		s.VisionStatus, s.VisionEvidenceSource, s.InferenceExecuted = "unavailable", "simulated_test_worker", false
	}
	return s, nil
}

func visionEvidenceProjectionV3(e contract.VisionEvidenceV1) VisionEvidenceProjectionV3 {
	facts := []string{
		"camera_health", "trigger", "presence.vehicle", "presence.animal", "activity",
		"pose.posture_confidence", "pose.transition_to_ground_confidence", "pose.temporal_support",
		"face", "plate", "sensitive_object", "media", "producer_health", "processing_status",
		"provenance", "window_start", "window_end", "window_seconds",
	}
	if e.RuntimeAggregate != nil {
		facts = append(facts, "runtime_aggregate.face", "runtime_aggregate.camera_integrity", "runtime_aggregate.interaction", "runtime_aggregate.motion", "runtime_aggregate.risk")
	}
	if e.Pose.Posture == contract.VisionPostureReclined {
		facts = append(facts, "pose.posture.reclined")
	}
	return VisionEvidenceProjectionV3{NotEncodedInSnapshotV3: len(facts) > 0, Facts: facts}
}

func (s CognitiveSnapshotV3) Normalized() CognitiveSnapshotV3 {
	s.SchemaVersion = SnapshotSchemaVersionV3
	if s.CapturedAt.IsZero() {
		s.CapturedAt = s.BaseV2.CapturedAt
	}
	s.CapturedAt = s.CapturedAt.UTC()
	s.BaseV2 = s.BaseV2.Normalized()
	s.BaseV2.SchemaVersion = SnapshotSchemaVersionV2
	if s.SimulatedCamera {
		s.VisionStatus = "unavailable"
		s.VisionEvidenceSource = "simulated_test_worker"
		s.InferenceExecuted = false
		s.Vision.PoseStatus = PoseV3Unavailable
		s.Vision.Posture = PostureV3Unknown
		s.Vision.FallState = FallV3Unknown
		s.Vision.PoseQuality = 0
		s.Vision.PoseObservationCount = 0
		s.Vision.PoseSampled = false
		s.Vision.RealDetection = false
		s.BaseV2.Vision.PoseStatus = PoseV3Unavailable
		s.BaseV2.Vision.Posture = PostureV3Unknown
		s.BaseV2.Vision.FallState = FallV3Unknown
		s.BaseV2.Vision.PoseQuality = 0
	}
	// This is the one authoritative duration. It occupies the existing V2
	// prefix position without changing the V1 nominal bundle or any offset.
	s.Vision.ImmobilitySeconds = nonNegativeV2(s.Vision.ImmobilitySeconds)
	if s.Vision.ImmobilitySeconds > 0 || s.BaseV2.Vision.ImmobilitySeconds == 0 {
		s.BaseV2.Vision.ImmobilitySeconds = s.Vision.ImmobilitySeconds
	} else {
		s.Vision.ImmobilitySeconds = s.BaseV2.Vision.ImmobilitySeconds
	}
	s.Vision.PoseStatus = normalizeV3(s.Vision.PoseStatus, []string{PoseV3Unavailable, PoseV3NotRequested, PoseV3LowQuality, PoseV3Available}, PoseV3Unavailable)
	s.Vision.Posture = normalizeV3(s.Vision.Posture, []string{PostureV3Unknown, PostureV3Upright, PostureV3Seated, PostureV3Reclined, PostureV3Ground}, PostureV3Unknown)
	s.Vision.FallState = normalizeV3(s.Vision.FallState, []string{FallV3None, FallV3Candidate, FallV3Confirmed, FallV3Unknown}, FallV3Unknown)
	s.Vision.MotionTier = normalizeV3(s.Vision.MotionTier, []string{MotionV3Unknown, MotionV3Still, MotionV3Normal, MotionV3Rapid, MotionV3VeryRapid}, MotionV3Unknown)
	s.Vision.InteractionState = normalizeV3(s.Vision.InteractionState, []string{InteractionV3None, InteractionV3Candidate, InteractionV3Unavailable}, InteractionV3None)
	s.Vision.FaceStatus = normalizeV3(s.Vision.FaceStatus, []string{FaceV3NotRequested, FaceV3Unavailable, FaceV3LowQuality, FaceV3Uncertain, FaceV3Recognized, FaceV3Unknown}, FaceV3Unavailable)
	s.Vision.CameraHealthStatus = normalizeV3(s.Vision.CameraHealthStatus, []string{CameraHealthV3Unknown, CameraHealthV3Healthy, CameraHealthV3Degraded, CameraHealthV3Offline, CameraHealthV3Uncertain}, CameraHealthV3Unknown)
	s.Vision.CameraIntegrityStatus = normalizeV3(s.Vision.CameraIntegrityStatus, []string{CameraHealthV3Unknown, CameraHealthV3Healthy, CameraHealthV3Degraded, CameraHealthV3Offline, CameraHealthV3Uncertain}, CameraHealthV3Unknown)
	s.Vision.RiskPersistence = normalizeV3(s.Vision.RiskPersistence, []string{RiskPersistenceNone, RiskPersistenceIsolated, RiskPersistenceRepeated, RiskPersistencePersistent, RiskPersistenceConfirmed}, RiskPersistenceNone)
	s.Vision.PoseQuality = clamp01(s.Vision.PoseQuality)
	s.Vision.RiskConfidence = clamp01(s.Vision.RiskConfidence)
	s.Vision.AggregateConfidence = clamp01(s.Vision.AggregateConfidence)
	s.Vision.RiskPersistenceSeconds = nonNegativeV2(s.Vision.RiskPersistenceSeconds)
	s.Vision.RiskObservationCount = maxInt(s.Vision.RiskObservationCount, 0)
	s.Vision.PoseObservationCount = maxInt(s.Vision.PoseObservationCount, 0)
	s.Vision.FaceConsensusFrames = maxInt(s.Vision.FaceConsensusFrames, 0)
	s.Vision.GroundDurationSeconds = nonNegativeV2(s.Vision.GroundDurationSeconds)
	s.Vision.FaceQuality = clamp01(s.Vision.FaceQuality)
	s.Vision.FaceConfidence = clamp01(s.Vision.FaceConfidence)
	s.Vision.FaceExpiresAt = s.Vision.FaceExpiresAt.UTC()
	return s
}

func (s CognitiveSnapshotV3) Validate() error {
	s = s.Normalized()
	if s.SchemaVersion != SnapshotSchemaVersionV3 || s.CapturedAt.IsZero() {
		return fmt.Errorf("invalid cognitive snapshot V3 metadata")
	}
	if err := s.BaseV2.Validate(); err != nil {
		return fmt.Errorf("invalid V2 base in V3 snapshot: %w", err)
	}
	if s.VisionEvidence != nil {
		if err := s.VisionEvidence.Validate(); err != nil {
			return fmt.Errorf("invalid attached Vision Evidence V1: %w", err)
		}
		if s.VisionEvidence.SimulatedCamera != s.SimulatedCamera {
			return fmt.Errorf("snapshot simulation marker disagrees with Vision Evidence V1")
		}
	}
	if s.Vision.CentralRetracking != 0 {
		return fmt.Errorf("central visual retracking is forbidden in V3")
	}
	if s.Vision.FallState == FallV3Confirmed {
		return fmt.Errorf("confirmed fall is not produced by V3")
	}
	if s.Vision.PhysicalInteractionCandidate && s.Vision.InteractionState != InteractionV3Candidate {
		return fmt.Errorf("physical interaction candidate requires candidate interaction state")
	}
	if s.Vision.FaceStatus == FaceV3Recognized && s.Vision.FaceConfidence <= 0 {
		return fmt.Errorf("recognized face aggregate requires bounded confidence")
	}
	if s.Vision.FaceQualificationProvenance != "" && s.Vision.FaceQualificationProvenance != "not_qualified" && s.Vision.FaceQualificationProvenance != "controlled_test" && s.Vision.FaceQualificationProvenance != "labeled_consent_manifest" {
		return fmt.Errorf("invalid face qualification provenance")
	}
	if (s.Vision.FallState == FallV3Candidate || s.Vision.FallState == FallV3Confirmed) && s.Vision.PoseStatus != PoseV3Available {
		return fmt.Errorf("V3 fall signal requires an available pose backend")
	}
	return nil
}

type EncodedSnapshotV3 struct {
	SchemaVersion  string                         `json:"schema_version"`
	EncoderVersion string                         `json:"encoder_version"`
	Shape          [1]int                         `json:"shape"`
	FeatureNames   [CognitiveVectorSizeV3]string  `json:"feature_names"`
	Values         [CognitiveVectorSizeV3]float32 `json:"values"`
}

func (e EncodedSnapshotV3) Validate() error {
	if e.SchemaVersion != EncoderSchemaVersionV3 || e.EncoderVersion != "3.0.0" || e.Shape[0] != CognitiveVectorSizeV3 || e.FeatureNames != CognitiveFeatureNamesV3 {
		return fmt.Errorf("invalid cognitive encoder V3 metadata")
	}
	for _, value := range e.Values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("non-finite V3 encoder value")
		}
	}
	return nil
}

type SnapshotEncoderV3 struct{}

func (SnapshotEncoderV3) Encode(ctx context.Context, snapshot CognitiveSnapshotV3) (EncodedSnapshotV3, error) {
	if err := ctx.Err(); err != nil {
		return EncodedSnapshotV3{}, err
	}
	snapshot = snapshot.Normalized()
	if err := snapshot.Validate(); err != nil {
		return EncodedSnapshotV3{}, err
	}
	base, err := (SnapshotEncoderV2{}).Encode(ctx, snapshot.BaseV2)
	if err != nil {
		return EncodedSnapshotV3{}, err
	}
	encoded := EncodedSnapshotV3{SchemaVersion: EncoderSchemaVersionV3, EncoderVersion: "3.0.0", Shape: [1]int{CognitiveVectorSizeV3}, FeatureNames: CognitiveFeatureNamesV3}
	copy(encoded.Values[:CognitiveVectorSizeV2], base.Values[:])
	v := &encoded.Values
	setOneHot(v[64:68], snapshot.Vision.PoseStatus, []string{PoseV3Unavailable, PoseV3NotRequested, PoseV3LowQuality, PoseV3Available})
	postureForFrozenVector := snapshot.Vision.Posture
	if postureForFrozenVector == PostureV3Reclined {
		// Preserve reclined in Evidence V1 and the cognitive snapshot. The frozen
		// 86D candidate has no reclined feature; encode only its conservative
		// unknown slot rather than conflating it with seated or ground.
		postureForFrozenVector = PostureV3Unknown
	}
	setOneHot(v[68:72], postureForFrozenVector, []string{PostureV3Unknown, PostureV3Upright, PostureV3Seated, PostureV3Ground})
	setOneHot(v[72:76], snapshot.Vision.FallState, []string{FallV3None, FallV3Candidate, FallV3Confirmed, FallV3Unknown})
	setOneHot(v[76:81], snapshot.Vision.RiskPersistence, []string{RiskPersistenceNone, RiskPersistenceIsolated, RiskPersistenceRepeated, RiskPersistencePersistent, RiskPersistenceConfirmed})
	v[81] = normalizeSeconds(snapshot.Vision.RiskPersistenceSeconds, 300)
	v[82] = normalizeCount(snapshot.Vision.RiskObservationCount, 32)
	v[83] = boolFloat(snapshot.Vision.RiskQualitySufficient)
	v[84] = normalizeCount(snapshot.Vision.PoseObservationCount, 32)
	v[85] = boolFloat(snapshot.Vision.PoseSampled)
	return encoded, nil
}

var CognitiveFeatureNamesV3 = cognitiveFeatureNamesV3()

func cognitiveFeatureNamesV3() [CognitiveVectorSizeV3]string {
	var values [CognitiveVectorSizeV3]string
	copy(values[:CognitiveVectorSizeV2], CognitiveFeatureNamesV2[:])
	ext := [...]string{
		"vision_v3.pose.unavailable", "vision_v3.pose.not_requested", "vision_v3.pose.low_quality", "vision_v3.pose.available",
		"vision_v3.posture.unknown", "vision_v3.posture.upright", "vision_v3.posture.seated", "vision_v3.posture.ground",
		"vision_v3.fall.none", "vision_v3.fall.candidate", "vision_v3.fall.confirmed", "vision_v3.fall.unknown",
		"vision_v3.risk_persistence.none", "vision_v3.risk_persistence.isolated", "vision_v3.risk_persistence.repeated", "vision_v3.risk_persistence.persistent", "vision_v3.risk_persistence.confirmed",
		"vision_v3.risk_persistence_seconds", "vision_v3.risk_observation_count", "vision_v3.risk_quality_sufficient", "vision_v3.pose_observation_count", "vision_v3.pose_sampled",
	}
	copy(values[CognitiveVectorSizeV2:], ext[:])
	return values
}

// projectV3ToV1 keeps the nominal V1 store view available while the V3
// candidate is evaluated. It projects only the immutable aggregate base; it
// never changes the V1 bundle or selects a V3 model for the V1 runtime.
func projectV3ToV1(value CognitiveSnapshotV3) CognitiveSnapshot {
	return projectV2ToV1(value.Normalized().BaseV2)
}

func normalizeV3(value string, allowed []string, fallback string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return value
		}
	}
	return fallback
}
