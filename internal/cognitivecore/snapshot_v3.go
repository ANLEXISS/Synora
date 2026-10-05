package cognitivecore

import (
	"context"
	"fmt"
	"math"
	"time"
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

	PostureV3Unknown = "unknown"
	PostureV3Upright = "upright"
	PostureV3Seated  = "seated"
	PostureV3Ground  = "ground"

	FallV3None      = "none"
	FallV3Candidate = "candidate"
	FallV3Confirmed = "confirmed"
	FallV3Unknown   = "unknown"

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
}

// CognitiveSnapshotV3 keeps V2 as a nested, immutable base and adds only the
// signals that proved impossible to represent without changing V2 semantics.
type CognitiveSnapshotV3 struct {
	SchemaVersion string              `json:"schema_version"`
	Revision      uint64              `json:"revision"`
	CapturedAt    time.Time           `json:"captured_at"`
	BaseV2        CognitiveSnapshotV2 `json:"base_v2"`
	Vision        VisionSignalsV3     `json:"vision"`
}

func (s CognitiveSnapshotV3) Normalized() CognitiveSnapshotV3 {
	s.SchemaVersion = SnapshotSchemaVersionV3
	if s.CapturedAt.IsZero() {
		s.CapturedAt = s.BaseV2.CapturedAt
	}
	s.CapturedAt = s.CapturedAt.UTC()
	s.BaseV2 = s.BaseV2.Normalized()
	s.BaseV2.SchemaVersion = SnapshotSchemaVersionV2
	// This is the one authoritative duration. It occupies the existing V2
	// prefix position without changing the V1 nominal bundle or any offset.
	s.Vision.ImmobilitySeconds = nonNegativeV2(s.Vision.ImmobilitySeconds)
	if s.Vision.ImmobilitySeconds > 0 || s.BaseV2.Vision.ImmobilitySeconds == 0 {
		s.BaseV2.Vision.ImmobilitySeconds = s.Vision.ImmobilitySeconds
	} else {
		s.Vision.ImmobilitySeconds = s.BaseV2.Vision.ImmobilitySeconds
	}
	s.Vision.PoseStatus = normalizeV3(s.Vision.PoseStatus, []string{PoseV3Unavailable, PoseV3NotRequested, PoseV3LowQuality, PoseV3Available}, PoseV3Unavailable)
	s.Vision.Posture = normalizeV3(s.Vision.Posture, []string{PostureV3Unknown, PostureV3Upright, PostureV3Seated, PostureV3Ground}, PostureV3Unknown)
	s.Vision.FallState = normalizeV3(s.Vision.FallState, []string{FallV3None, FallV3Candidate, FallV3Confirmed, FallV3Unknown}, FallV3Unknown)
	s.Vision.RiskPersistence = normalizeV3(s.Vision.RiskPersistence, []string{RiskPersistenceNone, RiskPersistenceIsolated, RiskPersistenceRepeated, RiskPersistencePersistent, RiskPersistenceConfirmed}, RiskPersistenceNone)
	s.Vision.PoseQuality = clamp01(s.Vision.PoseQuality)
	s.Vision.RiskConfidence = clamp01(s.Vision.RiskConfidence)
	s.Vision.AggregateConfidence = clamp01(s.Vision.AggregateConfidence)
	s.Vision.RiskPersistenceSeconds = nonNegativeV2(s.Vision.RiskPersistenceSeconds)
	s.Vision.RiskObservationCount = maxInt(s.Vision.RiskObservationCount, 0)
	s.Vision.PoseObservationCount = maxInt(s.Vision.PoseObservationCount, 0)
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
	if s.Vision.CentralRetracking != 0 {
		return fmt.Errorf("central visual retracking is forbidden in V3")
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
	setOneHot(v[68:72], snapshot.Vision.Posture, []string{PostureV3Unknown, PostureV3Upright, PostureV3Seated, PostureV3Ground})
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
