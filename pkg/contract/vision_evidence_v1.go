package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"time"
)

const EventVisionEvidenceV1 = "synora.vision.evidence/v1"

var opaqueVisionID = regexp.MustCompile(`^(ev|ep)_[a-f0-9]{24,64}$`)
var visionReasonCode = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type VisionAvailabilityV1 string

const (
	VisionNotRequested VisionAvailabilityV1 = "not_requested"
	VisionUnavailable  VisionAvailabilityV1 = "unavailable"
	VisionEvaluated    VisionAvailabilityV1 = "evaluated"
)

const (
	VisionPostureUpright   = "upright"
	VisionPostureSeated    = "seated"
	VisionPostureReclined  = "reclined"
	VisionPostureGround    = "ground"
	VisionPostureAmbiguous = "ambiguous"
	VisionPostureUnknown   = "unknown"
)

type VisionSupportV1 struct {
	ValidEvaluations int     `json:"valid_evaluations"`
	Continuity       string  `json:"continuity"`
	SupportedSeconds float64 `json:"supported_seconds"`
	GapCount         int     `json:"gap_count"`
}

type VisionMeasureV1 struct {
	Availability VisionAvailabilityV1 `json:"availability"`
	State        string               `json:"state"`
	Confidence   float64              `json:"confidence"`
	Quality      float64              `json:"quality"`
	Support      VisionSupportV1      `json:"support"`
}

type VisionPresenceV1 struct {
	Human                VisionMeasureV1 `json:"human"`
	HumanTrackCount      int             `json:"human_track_count,omitempty"`
	ConfirmedHumanTracks int             `json:"confirmed_human_tracks,omitempty"`
	VehicleTrackCount    int             `json:"vehicle_track_count,omitempty"`
	AnimalTrackCount     int             `json:"animal_track_count,omitempty"`
	Vehicle              VisionMeasureV1 `json:"vehicle"`
	Animal               VisionMeasureV1 `json:"animal"`
}

type VisionPoseV1 struct {
	Availability                 VisionAvailabilityV1 `json:"availability"`
	Posture                      string               `json:"posture"`
	PostureConfidence            float64              `json:"posture_confidence"`
	TransitionToGroundConfidence float64              `json:"transition_to_ground_confidence"`
	Quality                      float64              `json:"quality"`
	Support                      VisionSupportV1      `json:"support"`
	ImmobilitySeconds            float64              `json:"immobility_seconds"`
}

type VisionSemanticResultV1 struct {
	Availability VisionAvailabilityV1 `json:"availability"`
	Result       string               `json:"result"`
	Confidence   float64              `json:"confidence"`
	Quality      float64              `json:"quality"`
	Support      VisionSupportV1      `json:"support"`
}

type VisionEvidenceV1 struct {
	SchemaVersion    string                    `json:"schema_version"`
	EventID          string                    `json:"event_id"`
	EpisodeID        string                    `json:"episode_id"`
	WindowStart      time.Time                 `json:"window_start"`
	WindowEnd        time.Time                 `json:"window_end"`
	WindowSeconds    float64                   `json:"window_seconds"`
	Topology         string                    `json:"topology"`
	Provenance       string                    `json:"provenance"`
	SimulatedCamera  bool                      `json:"simulated_camera"`
	CameraHealth     VisionMeasureV1           `json:"camera_health"`
	Trigger          VisionMeasureV1           `json:"trigger"`
	Presence         VisionPresenceV1          `json:"presence"`
	Activity         VisionMeasureV1           `json:"activity"`
	Pose             VisionPoseV1              `json:"pose"`
	Face             VisionSemanticResultV1    `json:"face"`
	Plate            VisionSemanticResultV1    `json:"plate"`
	Sensitive        VisionSensitiveV1         `json:"sensitive_object"`
	Media            VisionMediaContinuityV1   `json:"media"`
	RuntimeAggregate *VisionRuntimeAggregateV1 `json:"runtime_aggregate,omitempty"`
	ProducerHealth   string                    `json:"producer_health"`
	Processing       string                    `json:"processing_status"`
	ErrorCode        string                    `json:"error_code,omitempty"`
	PriorityReasons  []string                  `json:"priority_reasons,omitempty"`
}

// VisionRuntimeAggregateV1 preserves aggregate-only V2/V3 facts that do not
// have dedicated family fields in the original Evidence V1 shape. It is a
// projection/diagnostic payload, never an instruction to change MLP offsets.
type VisionRuntimeAggregateV1 struct {
	FallState                    string  `json:"fall_state"`
	RecoveryObserved             bool    `json:"recovery_observed"`
	GroundDurationSeconds        float64 `json:"ground_duration_seconds"`
	RiskStatus                   string  `json:"risk_status"`
	RiskKind                     string  `json:"risk_kind"`
	RiskConfidence               float64 `json:"risk_confidence"`
	RiskPersistence              string  `json:"risk_persistence"`
	RiskPersistenceSeconds       float64 `json:"risk_persistence_seconds"`
	RiskObservationCount         int     `json:"risk_observation_count"`
	RiskQualitySufficient        bool    `json:"risk_quality_sufficient"`
	MotionTier                   string  `json:"motion_tier"`
	InteractionState             string  `json:"interaction_state"`
	PhysicalInteractionCandidate bool    `json:"physical_interaction_candidate"`
	FaceStatus                   string  `json:"face_status"`
	FaceConsensusFrames          int     `json:"face_consensus_frames"`
	FaceQuality                  float64 `json:"face_quality"`
	FaceConfidence               float64 `json:"face_confidence"`
	FaceQualificationProvenance  string  `json:"face_qualification_provenance"`
	CameraIntegrityStatus        string  `json:"camera_integrity_status"`
	CameraUncertainty            bool    `json:"camera_uncertainty"`
	AggregateConfidence          float64 `json:"aggregate_confidence"`
	EdgeTrackingOK               bool    `json:"edge_tracking_ok"`
	RealDetection                bool    `json:"real_detection"`
	ReplaySimulation             bool    `json:"replay_simulation"`
}

type VisionSensitiveV1 struct {
	Availability VisionAvailabilityV1 `json:"availability"`
	Category     string               `json:"category"`
	Confidence   float64              `json:"confidence"`
	Quality      float64              `json:"quality"`
	Support      VisionSupportV1      `json:"support"`
}

type VisionMediaContinuityV1 struct {
	Availability VisionAvailabilityV1 `json:"availability"`
	EpisodeState string               `json:"episode_state"`
	Support      VisionSupportV1      `json:"support"`
}

func (e VisionEvidenceV1) Validate() error {
	if e.SchemaVersion != EventVisionEvidenceV1 || !opaqueVisionID.MatchString(e.EventID) || !opaqueVisionID.MatchString(e.EpisodeID) || e.EventID[:3] != "ev_" || e.EpisodeID[:3] != "ep_" {
		return fmt.Errorf("invalid Vision Evidence V1 identity")
	}
	_, startOffset := e.WindowStart.Zone()
	_, endOffset := e.WindowEnd.Zone()
	if e.WindowStart.IsZero() || e.WindowEnd.IsZero() || startOffset != 0 || endOffset != 0 || !e.WindowEnd.After(e.WindowStart) {
		return fmt.Errorf("invalid Vision Evidence V1 time window")
	}
	duration := e.WindowEnd.Sub(e.WindowStart).Seconds()
	if !finiteRange(e.WindowSeconds, 0, 86400) || math.Abs(duration-e.WindowSeconds) > 0.001 {
		return fmt.Errorf("Vision Evidence V1 window duration is inconsistent")
	}
	if !ValidVisionTopologyClass(e.Topology) {
		return fmt.Errorf("invalid Vision Evidence V1 topology")
	}
	switch e.Provenance {
	case "real", "replay":
		if e.SimulatedCamera {
			return fmt.Errorf("real/replay provenance cannot be simulated_camera")
		}
	case "simulated_test":
		if !e.SimulatedCamera {
			return fmt.Errorf("simulated_test provenance requires simulated_camera")
		}
	default:
		return fmt.Errorf("invalid Vision Evidence V1 provenance")
	}
	if e.ProducerHealth != "healthy" && e.ProducerHealth != "degraded" && e.ProducerHealth != "unavailable" && e.ProducerHealth != "tamper_suspected" {
		return fmt.Errorf("invalid Vision Evidence V1 producer health")
	}
	if e.Processing != "complete" && e.Processing != "partial" && e.Processing != "unavailable" && e.Processing != "rejected" {
		return fmt.Errorf("invalid Vision Evidence V1 processing status")
	}
	if e.ErrorCode != "" && !validScalar(e.ErrorCode) {
		return fmt.Errorf("invalid Vision Evidence V1 error code")
	}
	if err := validateEvidenceMeasure(e.WindowSeconds, e.CameraHealth, []string{"healthy", "degraded", "unavailable", "tamper_suspected"}); err != nil {
		return fmt.Errorf("camera_health: %w", err)
	}
	if err := validateEvidenceMeasure(e.WindowSeconds, e.Trigger, []string{"motion", "human_probable", "vehicle_probable", "animal_probable", "tamper", "unknown"}); err != nil {
		return fmt.Errorf("trigger: %w", err)
	}
	for name, measure := range map[string]VisionMeasureV1{"human": e.Presence.Human, "vehicle": e.Presence.Vehicle, "animal": e.Presence.Animal} {
		if err := validateEvidenceMeasure(e.WindowSeconds, measure, []string{"absent", "present", "ambiguous", "unknown"}); err != nil {
			return fmt.Errorf("presence.%s: %w", name, err)
		}
	}
	if e.Presence.HumanTrackCount < 0 || e.Presence.ConfirmedHumanTracks < 0 || e.Presence.ConfirmedHumanTracks > e.Presence.HumanTrackCount || e.Presence.HumanTrackCount > 1000000 || e.Presence.VehicleTrackCount < 0 || e.Presence.VehicleTrackCount > 1000000 || e.Presence.AnimalTrackCount < 0 || e.Presence.AnimalTrackCount > 1000000 {
		return fmt.Errorf("invalid aggregate human track counts")
	}
	if len(e.PriorityReasons) > 32 {
		return fmt.Errorf("too many aggregate priority reasons")
	}
	if e.RuntimeAggregate != nil {
		if err := validateRuntimeAggregate(e.WindowSeconds, *e.RuntimeAggregate); err != nil {
			return fmt.Errorf("runtime_aggregate: %w", err)
		}
	}
	for _, reason := range e.PriorityReasons {
		if !visionReasonCode.MatchString(reason) {
			return fmt.Errorf("invalid aggregate priority reason")
		}
	}
	if err := validateEvidenceMeasure(e.WindowSeconds, e.Activity, []string{"still", "normal", "rapid", "very_rapid", "unknown"}); err != nil {
		return fmt.Errorf("activity: %w", err)
	}
	if err := validateEvidenceSupport(e.WindowSeconds, e.Pose.Availability, e.Pose.Posture, e.Pose.PostureConfidence, e.Pose.Quality, e.Pose.Support, []string{VisionPostureUpright, VisionPostureSeated, VisionPostureReclined, VisionPostureGround, VisionPostureAmbiguous, VisionPostureUnknown}); err != nil {
		return fmt.Errorf("pose: %w", err)
	}
	if !finiteRange(e.Pose.TransitionToGroundConfidence, 0, 1) || !finiteRange(e.Pose.ImmobilitySeconds, 0, e.WindowSeconds) {
		return fmt.Errorf("invalid Vision Evidence V1 pose continuous value")
	}
	if e.Pose.ImmobilitySeconds > 0 && e.Presence.Human.State != "present" {
		return fmt.Errorf("immobility requires a present human")
	}
	if err := validateSemanticResult(e.WindowSeconds, e.Face, true); err != nil {
		return fmt.Errorf("face: %w", err)
	}
	if err := validateSemanticResult(e.WindowSeconds, e.Plate, false); err != nil {
		return fmt.Errorf("plate: %w", err)
	}
	if err := validateEvidenceSupport(e.WindowSeconds, e.Sensitive.Availability, e.Sensitive.Category, e.Sensitive.Confidence, e.Sensitive.Quality, e.Sensitive.Support, []string{"none", "generic", "unknown"}); err != nil {
		return fmt.Errorf("sensitive_object: %w", err)
	}
	if err := validateEvidenceSupport(e.WindowSeconds, e.Media.Availability, e.Media.EpisodeState, 0, 0, e.Media.Support, []string{"continuous", "gapped", "recovered", "unknown"}); err != nil {
		return fmt.Errorf("media: %w", err)
	}
	if e.Media.Availability == VisionEvaluated {
		switch e.Media.EpisodeState {
		case "continuous":
			if e.Media.Support.GapCount != 0 || e.Media.Support.Continuity != "continuous" {
				return fmt.Errorf("continuous media state conflicts with support")
			}
		case "gapped":
			if e.Media.Support.GapCount == 0 || (e.Media.Support.Continuity != "gapped" && e.Media.Support.Continuity != "intermittent") {
				return fmt.Errorf("gapped media state conflicts with support")
			}
		case "recovered":
			if e.Media.Support.GapCount == 0 {
				return fmt.Errorf("recovered media state requires a prior gap")
			}
		}
	}
	return nil
}

func validateRuntimeAggregate(window float64, a VisionRuntimeAggregateV1) error {
	valid := func(value string, values ...string) bool {
		for _, candidate := range values {
			if value == candidate {
				return true
			}
		}
		return false
	}
	for field, ok := range map[string]bool{
		"fall_state":                    valid(a.FallState, "none", "candidate", "unknown"),
		"risk_status":                   valid(a.RiskStatus, "not_available", "not_requested", "uncertain", "suspected", "confirmed"),
		"risk_kind":                     valid(a.RiskKind, "none", "firearm", "other", "unknown"),
		"risk_persistence":              valid(a.RiskPersistence, "none", "isolated", "repeated", "persistent", "confirmed"),
		"motion_tier":                   valid(a.MotionTier, "unknown", "still", "normal", "rapid", "very_rapid"),
		"interaction_state":             valid(a.InteractionState, "none", "candidate", "unavailable"),
		"face_status":                   valid(a.FaceStatus, "not_requested", "unavailable", "low_quality", "uncertain", "recognized", "unknown"),
		"face_qualification_provenance": valid(a.FaceQualificationProvenance, "none", "unknown", "validated_dataset", "test_only"),
		"camera_integrity_status":       valid(a.CameraIntegrityStatus, "unknown", "healthy", "degraded", "offline", "uncertain"),
	} {
		if !ok {
			return fmt.Errorf("unsupported %s %q", field, map[string]string{"fall_state": a.FallState, "risk_status": a.RiskStatus, "risk_kind": a.RiskKind, "risk_persistence": a.RiskPersistence, "motion_tier": a.MotionTier, "interaction_state": a.InteractionState, "face_status": a.FaceStatus, "face_qualification_provenance": a.FaceQualificationProvenance, "camera_integrity_status": a.CameraIntegrityStatus}[field])
		}
	}
	if !finiteRange(a.GroundDurationSeconds, 0, window) || !finiteRange(a.RiskConfidence, 0, 1) ||
		!finiteRange(a.RiskPersistenceSeconds, 0, window) || a.RiskObservationCount < 0 || a.RiskObservationCount > 1000000 ||
		!finiteRange(a.FaceQuality, 0, 1) || !finiteRange(a.FaceConfidence, 0, 1) || a.FaceConsensusFrames < 0 || a.FaceConsensusFrames > 1000000 ||
		!finiteRange(a.AggregateConfidence, 0, 1) {
		return fmt.Errorf("invalid aggregate continuous value")
	}
	if a.PhysicalInteractionCandidate && a.InteractionState != "candidate" {
		return fmt.Errorf("physical interaction candidate requires candidate state")
	}
	if a.RiskStatus == "confirmed" && a.RiskKind == "none" {
		return fmt.Errorf("confirmed risk requires a risk kind")
	}
	if a.FaceStatus == "recognized" && a.FaceConfidence == 0 {
		return fmt.Errorf("recognized face aggregate requires confidence")
	}
	if a.RealDetection && a.ReplaySimulation {
		return fmt.Errorf("real detection cannot be replay simulation")
	}
	return nil
}

func validateEvidenceMeasure(window float64, value VisionMeasureV1, states []string) error {
	return validateEvidenceSupport(window, value.Availability, value.State, value.Confidence, value.Quality, value.Support, states)
}

func validateSemanticResult(window float64, value VisionSemanticResultV1, face bool) error {
	states := []string{"recognized", "unknown", "ambiguous"}
	if face {
		states = append(states, "candidate")
	}
	return validateEvidenceSupport(window, value.Availability, value.Result, value.Confidence, value.Quality, value.Support, states)
}

func validateEvidenceSupport(window float64, availability VisionAvailabilityV1, state string, confidence, quality float64, support VisionSupportV1, states []string) error {
	if availability != VisionNotRequested && availability != VisionUnavailable && availability != VisionEvaluated {
		return fmt.Errorf("invalid availability")
	}
	valid := false
	for _, allowed := range states {
		if state == allowed {
			valid = true
			break
		}
	}
	if !valid || !finiteRange(confidence, 0, 1) || !finiteRange(quality, 0, 1) {
		return fmt.Errorf("invalid state or normalized value")
	}
	if support.ValidEvaluations < 0 || support.ValidEvaluations > 1000000 || support.GapCount < 0 || support.GapCount > 1000000 || !finiteRange(support.SupportedSeconds, 0, window) {
		return fmt.Errorf("invalid temporal support")
	}
	switch support.Continuity {
	case "continuous", "intermittent", "gapped", "unknown":
	default:
		return fmt.Errorf("invalid continuity")
	}
	if availability != VisionEvaluated && support.ValidEvaluations != 0 {
		return fmt.Errorf("non-evaluated family cannot have valid evaluations")
	}
	if availability != VisionEvaluated && (confidence != 0 || quality != 0 || support.SupportedSeconds != 0 || support.GapCount != 0 || support.Continuity != "unknown") {
		return fmt.Errorf("non-evaluated family cannot carry confidence, quality, or temporal support")
	}
	if availability != VisionEvaluated && state != "unknown" && !(availability == VisionNotRequested && state == "none") && state != "unavailable" {
		return fmt.Errorf("non-evaluated family must use unknown state")
	}
	if availability == VisionEvaluated && support.ValidEvaluations == 0 {
		return fmt.Errorf("evaluated family requires valid evaluations")
	}
	if support.GapCount > support.ValidEvaluations && support.ValidEvaluations > 0 {
		return fmt.Errorf("gap count exceeds valid evaluations")
	}
	if availability == VisionEvaluated {
		if support.Continuity == "continuous" && support.GapCount != 0 {
			return fmt.Errorf("continuous support cannot contain gaps")
		}
		if (support.Continuity == "gapped" || support.Continuity == "intermittent") && support.GapCount == 0 {
			return fmt.Errorf("gapped/intermittent support requires gaps")
		}
	}
	return nil
}

func finiteRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func DecodeVisionEvidenceV1(data []byte) (VisionEvidenceV1, error) {
	var evidence VisionEvidenceV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return VisionEvidenceV1{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return VisionEvidenceV1{}, fmt.Errorf("trailing Vision Evidence V1 data")
	}
	if err := evidence.Validate(); err != nil {
		return VisionEvidenceV1{}, err
	}
	return evidence, nil
}
