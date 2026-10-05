package cognitivecore

import (
	"context"
	"fmt"
	"math"
	"time"

	"synora/pkg/contract"
)

// V2 is intentionally additive.  The V1 snapshot/encoder and bundle remain
// the nominal runtime until this independent contract is qualified.
const (
	SnapshotSchemaVersionV2 = "cognitive-snapshot/v2"
	EncoderSchemaVersionV2  = "cognitive-encoder/v2"
	CognitiveVectorSizeV2   = 64
)

const (
	PoseNotAvailable = "not_available"
	PoseNotRequested = "not_requested"
	PoseAvailable    = "available"

	PostureUnknown  = "unknown"
	PostureStanding = "standing"
	PostureSitting  = "sitting"
	PostureLying    = "lying"

	FallNone     = "none"
	FallPossible = "possible"
	FallProbable = "probable"

	RiskNotAvailable = "not_available"
	RiskNotRequested = "not_requested"
	RiskUncertain    = "uncertain"
	RiskSuspected    = "suspected"
	RiskConfirmed    = "confirmed"

	RiskKindNone    = "none"
	RiskKindFirearm = "firearm"
	RiskKindOther   = "other"
	RiskKindUnknown = "unknown"

	IdentityKnown        = "known"
	IdentityUncertain    = "uncertain"
	IdentityUnknown      = "unknown"
	IdentityNotAvailable = "not_available"

	TTSAvailable    = "available"
	TTSUnavailable  = "unavailable"
	TTSNotRequested = "not_requested"
)

// VisionSignalsV2 contains aggregate facts only.  It deliberately excludes
// media, boxes, crops, embeddings, identities and process-local track IDs.
// ImmobilitySeconds is the normalized duration for which the confirmed human
// has remained immobile, irrespective of posture.  A ground posture is carried
// separately by Posture and must never be inferred from this duration.
type VisionSignalsV2 struct {
	HumanConfirmed               bool    `json:"human_confirmed"`
	PoseStatus                   string  `json:"pose_status"`
	PoseQuality                  float32 `json:"pose_quality"`
	Posture                      string  `json:"posture"`
	TransitionToGround           bool    `json:"transition_to_ground"`
	ImmobilitySeconds            float32 `json:"immobility_seconds"`
	RecoveryObserved             bool    `json:"recovery_observed"`
	FallState                    string  `json:"fall_state"`
	FallQualitySufficient        bool    `json:"fall_quality_sufficient"`
	RiskStatus                   string  `json:"risk_status"`
	RiskKind                     string  `json:"risk_kind"`
	RiskConfidence               float32 `json:"risk_confidence"`
	IdentityStatus               string  `json:"identity_status"`
	RealDetection                bool    `json:"real_detection"`
	ReplaySimulation             bool    `json:"replay_simulation"`
	AggregateConfidence          float32 `json:"aggregate_confidence"`
	ObservationCount             int     `json:"observation_count"`
	EdgeTrackingOK               bool    `json:"edge_tracking_ok"`
	CentralRetrackingInvocations int     `json:"central_retracking_invocations"`
}

type CommunicationCapabilitiesV2 struct {
	AnnounceAvailable        bool    `json:"announce_available"`
	TTSStatus                string  `json:"tts_status"`
	CooldownActive           bool    `json:"cooldown_active"`
	CooldownRemainingSeconds float32 `json:"cooldown_remaining_seconds"`
}

// CognitiveSnapshotV2 is the fixed, documented input to the V2 shared MLP.
type CognitiveSnapshotV2 struct {
	SchemaVersion       string                      `json:"schema_version"`
	Revision            uint64                      `json:"revision"`
	CapturedAt          time.Time                   `json:"captured_at"`
	Security            SecurityFacts               `json:"security"`
	Presence            PresenceFacts               `json:"presence"`
	Topology            string                      `json:"topology"`
	Episode             EpisodeFacts                `json:"episode"`
	Sensors             SensorFacts                 `json:"sensors"`
	PreviousDanger      float32                     `json:"previous_danger"`
	PreviousDangerKnown bool                        `json:"previous_danger_known"`
	Vision              VisionSignalsV2             `json:"vision"`
	Communication       CommunicationCapabilitiesV2 `json:"communication"`
}

var CognitiveFeatureNamesV2 = cognitiveFeatureNamesV2()

func (s CognitiveSnapshotV2) Normalized() CognitiveSnapshotV2 {
	s.SchemaVersion = SnapshotSchemaVersionV2
	if s.CapturedAt.IsZero() {
		s.CapturedAt = time.Unix(0, 0).UTC()
	} else {
		s.CapturedAt = s.CapturedAt.UTC()
	}
	s.Topology = normalizeTopology(s.Topology)
	s.Presence.KnownResidentCount = maxInt(s.Presence.KnownResidentCount, 0)
	s.Presence.TrackCount = maxInt(s.Presence.TrackCount, 0)
	s.Episode.SegmentCount = maxInt(s.Episode.SegmentCount, 0)
	s.Episode.GapCount = maxInt(s.Episode.GapCount, 0)
	s.Sensors.ObservationCount = maxInt(s.Sensors.ObservationCount, 0)
	s.Vision.PoseStatus = normalizeV2PoseStatus(s.Vision.PoseStatus)
	s.Vision.Posture = normalizeV2Posture(s.Vision.Posture)
	s.Vision.FallState = normalizeV2Fall(s.Vision.FallState)
	s.Vision.RiskStatus = normalizeV2RiskStatus(s.Vision.RiskStatus)
	s.Vision.RiskKind = normalizeV2RiskKind(s.Vision.RiskKind)
	s.Vision.IdentityStatus = normalizeV2Identity(s.Vision.IdentityStatus)
	s.Vision.PoseQuality = clamp01(s.Vision.PoseQuality)
	s.Vision.ImmobilitySeconds = nonNegativeV2(s.Vision.ImmobilitySeconds)
	s.Vision.RiskConfidence = clamp01(s.Vision.RiskConfidence)
	s.Vision.AggregateConfidence = clamp01(s.Vision.AggregateConfidence)
	s.Vision.ObservationCount = maxInt(s.Vision.ObservationCount, 0)
	s.Vision.CentralRetrackingInvocations = maxInt(s.Vision.CentralRetrackingInvocations, 0)
	s.Communication.TTSStatus = normalizeV2TTS(s.Communication.TTSStatus)
	s.Communication.CooldownRemainingSeconds = nonNegativeV2(s.Communication.CooldownRemainingSeconds)
	if s.Communication.CooldownRemainingSeconds > 0 {
		s.Communication.CooldownActive = true
	}
	s.PreviousDanger = clamp01(s.PreviousDanger)
	return s
}

func (s CognitiveSnapshotV2) Validate() error {
	s = s.Normalized()
	if s.SchemaVersion != SnapshotSchemaVersionV2 || s.CapturedAt.IsZero() || !contract.ValidVisionTopologyClass(s.Topology) {
		return fmt.Errorf("invalid cognitive snapshot V2 metadata")
	}
	if s.Vision.CentralRetrackingInvocations != 0 {
		return fmt.Errorf("central visual retracking is forbidden in V2")
	}
	if s.Vision.FallState != FallNone && !s.Vision.FallQualitySufficient {
		return fmt.Errorf("fall signal requires sufficient pose quality")
	}
	if s.Vision.RiskStatus == RiskConfirmed && s.Vision.RiskKind == RiskKindNone {
		return fmt.Errorf("confirmed risk requires a risk kind")
	}
	if s.Vision.PoseQuality < 0 || s.Vision.PoseQuality > 1 || s.Vision.RiskConfidence < 0 || s.Vision.RiskConfidence > 1 {
		return fmt.Errorf("invalid V2 vision confidence")
	}
	return nil
}

type EncodedSnapshotV2 struct {
	SchemaVersion  string                         `json:"schema_version"`
	EncoderVersion string                         `json:"encoder_version"`
	Shape          [1]int                         `json:"shape"`
	FeatureNames   [CognitiveVectorSizeV2]string  `json:"feature_names"`
	Values         [CognitiveVectorSizeV2]float32 `json:"values"`
}

func (e EncodedSnapshotV2) Validate() error {
	if e.SchemaVersion != EncoderSchemaVersionV2 || e.EncoderVersion != "2.0.0" || e.Shape[0] != CognitiveVectorSizeV2 || e.FeatureNames != CognitiveFeatureNamesV2 {
		return fmt.Errorf("invalid cognitive encoder V2 metadata")
	}
	for _, value := range e.Values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("non-finite V2 encoder value")
		}
	}
	return nil
}

type SnapshotEncoderV2 struct{}

func (SnapshotEncoderV2) Encode(ctx context.Context, snapshot CognitiveSnapshotV2) (EncodedSnapshotV2, error) {
	if err := ctx.Err(); err != nil {
		return EncodedSnapshotV2{}, err
	}
	snapshot = snapshot.Normalized()
	if err := snapshot.Validate(); err != nil {
		return EncodedSnapshotV2{}, err
	}
	encoded := EncodedSnapshotV2{SchemaVersion: EncoderSchemaVersionV2, EncoderVersion: "2.0.0", Shape: [1]int{CognitiveVectorSizeV2}, FeatureNames: CognitiveFeatureNamesV2}
	v := &encoded.Values
	v[0] = boolFloat(snapshot.Security.Armed)
	v[1] = boolFloat(snapshot.Security.Degraded)
	v[2] = boolFloat(snapshot.Security.Known)
	v[3] = boolFloat(snapshot.Presence.HumanPresent)
	v[4] = boolFloat(snapshot.Presence.KnownResidentsPresent)
	v[5] = normalizeCount(snapshot.Presence.TrackCount, 16)
	v[6] = boolFloat(snapshot.Presence.TrackConfirmed)
	setOneHot(v[7:12], snapshot.Topology, []string{contract.VisionTopologyPublicOutdoor, contract.VisionTopologyPrivatePerimeter, contract.VisionTopologyRestrictedThreshold, contract.VisionTopologyProtectedInterior, contract.VisionTopologyUnknown})
	setOneHot(v[12:16], snapshot.Episode.Phase, []string{VisionPhaseInitial, VisionPhaseCandidate, VisionPhaseConfirmed, VisionPhaseFinal})
	v[16] = normalizeCount(snapshot.Episode.SegmentCount, 32)
	v[17] = normalizeCount(snapshot.Episode.GapCount, 16)
	v[18] = normalizeSeconds(snapshot.Episode.SecondsSinceFirst, 300)
	v[19] = normalizeSeconds(snapshot.Episode.SecondsSinceLast, 300)
	v[20] = normalizeSeconds(snapshot.Episode.CalmSeconds, 300)
	v[21] = boolFloat(snapshot.Sensors.Movement)
	v[22] = boolFloat(snapshot.Sensors.AccessState == VisionAccessOpen)
	v[23] = boolFloat(snapshot.Sensors.AccessState == VisionAccessForced)
	v[24] = boolFloat(snapshot.Sensors.AlarmState == VisionAlarmTriggered)
	v[25] = boolFloat(snapshot.Vision.HumanConfirmed)
	v[26] = boolFloat(snapshot.Vision.PoseStatus == PoseAvailable)
	v[27] = snapshot.Vision.PoseQuality
	setOneHot(v[28:32], snapshot.Vision.Posture, []string{PostureStanding, PostureSitting, PostureLying, PostureUnknown})
	v[32] = boolFloat(snapshot.Vision.TransitionToGround)
	v[33] = normalizeSeconds(snapshot.Vision.ImmobilitySeconds, 120)
	v[34] = boolFloat(snapshot.Vision.RecoveryObserved)
	setOneHot(v[35:38], snapshot.Vision.FallState, []string{FallNone, FallPossible, FallProbable})
	v[38] = boolFloat(snapshot.Vision.FallQualitySufficient)
	setOneHot(v[39:44], snapshot.Vision.RiskStatus, []string{RiskNotAvailable, RiskNotRequested, RiskUncertain, RiskSuspected, RiskConfirmed})
	setOneHot(v[44:48], snapshot.Vision.RiskKind, []string{RiskKindNone, RiskKindFirearm, RiskKindOther, RiskKindUnknown})
	setOneHot(v[48:52], snapshot.Vision.IdentityStatus, []string{IdentityKnown, IdentityUncertain, IdentityUnknown, IdentityNotAvailable})
	v[52] = boolFloat(snapshot.Communication.AnnounceAvailable)
	v[53] = boolFloat(snapshot.Communication.TTSStatus == TTSAvailable)
	v[54] = boolFloat(snapshot.Communication.CooldownActive)
	v[55] = normalizeSeconds(snapshot.Communication.CooldownRemainingSeconds, 120)
	v[56] = boolFloat(snapshot.Vision.RealDetection)
	v[57] = boolFloat(snapshot.Vision.ReplaySimulation)
	v[58] = snapshot.Vision.AggregateConfidence
	v[59] = normalizeCount(snapshot.Vision.ObservationCount, 32)
	v[60] = boolFloat(snapshot.Vision.EdgeTrackingOK)
	v[61] = normalizeCount(snapshot.Presence.KnownResidentCount, 16)
	v[62] = snapshot.PreviousDanger
	v[63] = boolFloat(snapshot.PreviousDangerKnown)
	return encoded, nil
}

func cognitiveFeatureNamesV2() [CognitiveVectorSizeV2]string {
	values := [...]string{
		"security.armed", "security.degraded", "security.known", "presence.human", "presence.known_residents", "presence.track_count", "presence.track_confirmed",
		"topology.public_outdoor", "topology.private_perimeter", "topology.restricted_threshold", "topology.protected_interior", "topology.unknown",
		"episode.initial", "episode.candidate", "episode.confirmed", "episode.final", "episode.segment_count", "episode.gap_count", "episode.seconds_since_first", "episode.seconds_since_last", "episode.calm_seconds",
		"sensor.movement", "sensor.access_open", "sensor.access_forced", "sensor.alarm_triggered", "vision.human_confirmed", "vision.pose.available", "vision.pose.quality",
		"vision.posture.standing", "vision.posture.sitting", "vision.posture.lying", "vision.posture.unknown", "vision.transition_to_ground", "vision.immobility_seconds", "vision.recovery_observed",
		"vision.fall.none", "vision.fall.possible", "vision.fall.probable", "vision.fall.quality_sufficient", "vision.risk.not_available", "vision.risk.not_requested", "vision.risk.uncertain", "vision.risk.suspected", "vision.risk.confirmed",
		"vision.risk_kind.none", "vision.risk_kind.firearm", "vision.risk_kind.other", "vision.risk_kind.unknown", "vision.identity.known", "vision.identity.uncertain", "vision.identity.unknown", "vision.identity.not_available",
		"communication.announce_available", "communication.tts_available", "communication.cooldown_active", "communication.cooldown_remaining_seconds", "vision.real_detection", "vision.replay_simulation", "vision.aggregate_confidence", "vision.observation_count", "edge.tracking_ok", "presence.known_resident_count", "danger.previous", "danger.previous_known",
	}
	return values
}

func normalizeV2PoseStatus(value string) string {
	if value == PoseAvailable || value == PoseNotRequested || value == PoseNotAvailable {
		return value
	}
	return PoseNotAvailable
}
func normalizeV2Posture(value string) string {
	if value == PostureStanding || value == PostureSitting || value == PostureLying || value == PostureUnknown {
		return value
	}
	return PostureUnknown
}
func normalizeV2Fall(value string) string {
	if value == FallPossible || value == FallProbable || value == FallNone {
		return value
	}
	return FallNone
}
func normalizeV2RiskStatus(value string) string {
	if value == RiskNotAvailable || value == RiskNotRequested || value == RiskUncertain || value == RiskSuspected || value == RiskConfirmed {
		return value
	}
	return RiskNotAvailable
}
func normalizeV2RiskKind(value string) string {
	if value == RiskKindFirearm || value == RiskKindOther || value == RiskKindUnknown || value == RiskKindNone {
		return value
	}
	return RiskKindNone
}
func normalizeV2Identity(value string) string {
	if value == IdentityKnown || value == IdentityUncertain || value == IdentityUnknown || value == IdentityNotAvailable {
		return value
	}
	return IdentityNotAvailable
}
func normalizeV2TTS(value string) string {
	if value == TTSAvailable || value == TTSUnavailable || value == TTSNotRequested {
		return value
	}
	return TTSUnavailable
}
func nonNegativeV2(value float32) float32 {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
		return 0
	}
	return value
}

// projectV2ToV1 keeps the existing Universal Store and V1 readers coherent.
func projectV2ToV1(value CognitiveSnapshotV2) CognitiveSnapshot {
	value = value.Normalized()
	return CognitiveSnapshot{SchemaVersion: SnapshotSchemaVersion, CapturedAt: value.CapturedAt, Security: value.Security, Presence: value.Presence, Topology: value.Topology, Sensors: value.Sensors, Episode: value.Episode, PreviousDanger: value.PreviousDanger, PreviousDangerKnown: value.PreviousDangerKnown}
}
