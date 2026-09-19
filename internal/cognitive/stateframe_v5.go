package cognitive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"synora/pkg/contract"
)

// State-encoder/v5 is a model-independent projection of normalized evidence.
// It is deliberately separate from the active MLP input contract.
const (
	StateEncoderV5SchemaVersion = "state-encoder/v5"
	StateFrameV5CaptureSchema   = "synora.stateframe-v5-capture/v1"
	EncoderV5ID                 = "synora-state-encoder"
	EncoderV5Version            = "5.0.0"
	EncoderV5Size               = 44
)

const (
	V5PriorityOriginCore    = "core"
	V5PriorityOriginVision  = "vision"
	V5PriorityOriginUnknown = "unknown"
)

const (
	V5PhaseInitial   = "initial"
	V5PhaseCandidate = "candidate"
	V5PhaseConfirmed = "confirmed"
	V5PhaseFinal     = "final"

	V5EnrichmentUnavailable  = "unavailable"
	V5EnrichmentNotRequested = "not_requested"
	V5EnrichmentUncertain    = "uncertain"
	V5EnrichmentRecognized   = "recognized"
	V5EnrichmentUnknown      = "unknown"

	V5AccessUnknown = "unknown"
	V5AccessClosed  = "closed"
	V5AccessOpen    = "open"
	V5AccessForced  = "forced"

	V5AlarmUnknown   = "unknown"
	V5AlarmArmed     = "armed"
	V5AlarmDisarmed  = "disarmed"
	V5AlarmTriggered = "triggered"
)

// StateFrameV5 contains only normalized, deterministic facts. It contains no
// event, device, camera, track, identity, media, or biometric data.
type StateFrameV5 struct {
	SchemaVersion  string                 `json:"schema_version"`
	CapturedAt     time.Time              `json:"captured_at"`
	Security       StateFrameV5Security   `json:"security"`
	Presence       StateFrameV5Presence   `json:"presence"`
	TopologyClass  string                 `json:"topology_class"`
	Priority       string                 `json:"priority"`
	PriorityOrigin string                 `json:"priority_origin"`
	EpisodePhase   string                 `json:"episode_phase"`
	Enrichment     string                 `json:"enrichment_status"`
	Continuity     StateFrameV5Continuity `json:"continuity"`
	Quality        StateFrameV5Quality    `json:"quality"`
	CoEvidence     StateFrameV5CoEvidence `json:"co_evidence"`
}

type StateFrameV5Security struct {
	Armed    bool `json:"armed"`
	Degraded bool `json:"degraded"`
	Known    bool `json:"known"`
}

type StateFrameV5Presence struct {
	HumanPresent   bool `json:"human_present"`
	TrackCount     int  `json:"track_count"`
	TrackConfirmed bool `json:"track_confirmed"`
}

type StateFrameV5Continuity struct {
	SecondsSinceFirstObservation float32 `json:"seconds_since_first_observation"`
	SecondsSinceLastObservation  float32 `json:"seconds_since_last_observation"`
	SegmentCount                 int     `json:"segment_count"`
	GapCount                     int     `json:"gap_count"`
	CalmSeconds                  float32 `json:"calm_seconds"`
}

type StateFrameV5Quality struct {
	RealDetection       bool    `json:"real_detection"`
	ReplaySimulation    bool    `json:"replay_simulation"`
	ObservationCount    int     `json:"observation_count"`
	AggregateConfidence float32 `json:"aggregate_confidence"`
}

type StateFrameV5CoEvidence struct {
	AccessState    string `json:"access_state"`
	Movement       bool   `json:"movement"`
	SensorEvidence bool   `json:"sensor_evidence"`
	AlarmState     string `json:"alarm_state"`
}

// EncoderV5FeatureNames is the frozen order. Do not reorder or insert a
// feature; append-only changes require a new encoder schema.
var EncoderV5FeatureNames = [...]string{
	"security.armed", "security.degraded", "security.known", "presence.human",
	"presence.track_count", "presence.track_confirmed",
	"topology.public_outdoor", "topology.private_perimeter", "topology.restricted_threshold", "topology.protected_interior", "topology.unknown",
	"priority.p0", "priority.p1", "priority.p2", "priority.p3", "priority.p4",
	"phase.initial", "phase.candidate", "phase.confirmed", "phase.final",
	"enrichment.unavailable", "enrichment.not_requested", "enrichment.uncertain", "enrichment.recognized", "enrichment.unknown",
	"continuity.seconds_since_first", "continuity.seconds_since_last", "continuity.segment_count", "continuity.gap_count", "continuity.calm_seconds",
	"quality.real_detection", "quality.replay_simulation", "quality.observation_count", "quality.aggregate_confidence",
	"co_evidence.access_unknown", "co_evidence.access_closed", "co_evidence.access_open", "co_evidence.access_forced",
	"co_evidence.movement", "co_evidence.sensor", "co_evidence.alarm_unknown", "co_evidence.alarm_armed", "co_evidence.alarm_disarmed", "co_evidence.alarm_triggered",
}

type EncodedStateV5 struct {
	SchemaVersion  string                 `json:"schema_version"`
	EncoderID      string                 `json:"encoder_id"`
	EncoderVersion string                 `json:"encoder_version"`
	DType          string                 `json:"dtype"`
	Shape          [1]int                 `json:"shape"`
	FeatureNames   [EncoderV5Size]string  `json:"feature_names"`
	Values         [EncoderV5Size]float32 `json:"values"`
	FrameChecksum  string                 `json:"frame_checksum"`
}

func (f StateFrameV5) Normalized() StateFrameV5 {
	if f.CapturedAt.IsZero() {
		f.CapturedAt = time.Unix(0, 0).UTC()
	} else {
		f.CapturedAt = f.CapturedAt.UTC()
	}
	f.SchemaVersion = StateEncoderV5SchemaVersion
	f.TopologyClass = normalizeV5Topology(f.TopologyClass)
	f.Priority = normalizeV5Priority(f.Priority)
	if f.PriorityOrigin == "" {
		f.PriorityOrigin = V5PriorityOriginUnknown
	}
	f.EpisodePhase = normalizeV5Phase(f.EpisodePhase)
	f.Enrichment = normalizeV5Enrichment(f.Enrichment)
	f.Continuity.SecondsSinceFirstObservation = boundedNonNegative(f.Continuity.SecondsSinceFirstObservation)
	f.Continuity.SecondsSinceLastObservation = boundedNonNegative(f.Continuity.SecondsSinceLastObservation)
	f.Continuity.SegmentCount = maxInt(f.Continuity.SegmentCount, 0)
	f.Continuity.GapCount = maxInt(f.Continuity.GapCount, 0)
	f.Continuity.CalmSeconds = boundedNonNegative(f.Continuity.CalmSeconds)
	f.Presence.TrackCount = maxInt(f.Presence.TrackCount, 0)
	f.Quality.ObservationCount = maxInt(f.Quality.ObservationCount, 0)
	f.Quality.AggregateConfidence = clampV5(f.Quality.AggregateConfidence)
	f.CoEvidence.AccessState = normalizeV5Access(f.CoEvidence.AccessState)
	f.CoEvidence.AlarmState = normalizeV5Alarm(f.CoEvidence.AlarmState)
	return f
}

func (f StateFrameV5) Validate() error {
	if f.SchemaVersion != StateEncoderV5SchemaVersion {
		return fmt.Errorf("invalid stateframe v5 schema %q", f.SchemaVersion)
	}
	if f.CapturedAt.IsZero() || f.CapturedAt.Location() != time.UTC {
		return fmt.Errorf("stateframe v5 captured_at must be UTC")
	}
	if !validV5Topology(f.TopologyClass) || !validV5Priority(f.Priority) || !validV5Phase(f.EpisodePhase) || !validV5Enrichment(f.Enrichment) || !validV5Access(f.CoEvidence.AccessState) || !validV5Alarm(f.CoEvidence.AlarmState) {
		return fmt.Errorf("stateframe v5 contains an invalid categorical value")
	}
	if f.PriorityOrigin != V5PriorityOriginCore && f.PriorityOrigin != V5PriorityOriginVision && f.PriorityOrigin != V5PriorityOriginUnknown {
		return fmt.Errorf("invalid stateframe v5 priority origin %q", f.PriorityOrigin)
	}
	if f.Priority == contract.VisionPriorityP0 && f.PriorityOrigin != V5PriorityOriginCore {
		return fmt.Errorf("P0 is Core-only and cannot be created by Vision")
	}
	for _, value := range []float32{f.Continuity.SecondsSinceFirstObservation, f.Continuity.SecondsSinceLastObservation, f.Continuity.CalmSeconds, f.Quality.AggregateConfidence} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
			return fmt.Errorf("invalid stateframe v5 numeric fact")
		}
	}
	if f.Quality.AggregateConfidence > 1 || f.Presence.TrackCount < 0 || f.Continuity.SegmentCount < 0 || f.Continuity.GapCount < 0 || f.Quality.ObservationCount < 0 {
		return fmt.Errorf("invalid stateframe v5 bounded fact")
	}
	return nil
}

func (f StateFrameV5) CanonicalJSON() ([]byte, error) {
	return json.Marshal(f.Normalized())
}

func (f StateFrameV5) Fingerprint() (string, error) {
	body, err := f.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

type V5StateEncoder struct{}

func (V5StateEncoder) ID() string      { return EncoderV5ID }
func (V5StateEncoder) Version() string { return EncoderV5Version }

// Encode computes only the fixed numeric vector. It does not serialize the
// frame or allocate a variable-length Values slice. Fingerprint is separate so
// normal execution can avoid canonical JSON work.
func (e V5StateEncoder) Encode(ctx context.Context, frame StateFrameV5) (EncodedStateV5, error) {
	if err := ctx.Err(); err != nil {
		return EncodedStateV5{}, err
	}
	frame = frame.Normalized()
	if err := frame.Validate(); err != nil {
		return EncodedStateV5{}, err
	}
	encoded := EncodedStateV5{SchemaVersion: StateEncoderV5SchemaVersion, EncoderID: e.ID(), EncoderVersion: e.Version(), DType: "float32", Shape: [1]int{EncoderV5Size}}
	encoded.FeatureNames = EncoderV5FeatureNames
	values := &encoded.Values
	values[0] = boolV5(frame.Security.Armed)
	values[1] = boolV5(frame.Security.Degraded)
	values[2] = boolV5(frame.Security.Known)
	values[3] = boolV5(frame.Presence.HumanPresent)
	values[4] = normalizeCount(frame.Presence.TrackCount, 16)
	values[5] = boolV5(frame.Presence.TrackConfirmed)
	setOneHot(values[6:11], frame.TopologyClass, []string{contract.VisionTopologyPublicOutdoor, contract.VisionTopologyPrivatePerimeter, contract.VisionTopologyRestrictedThreshold, contract.VisionTopologyProtectedInterior, contract.VisionTopologyUnknown})
	setOneHot(values[11:16], frame.Priority, []string{contract.VisionPriorityP0, contract.VisionPriorityP1, contract.VisionPriorityP2, contract.VisionPriorityP3, contract.VisionPriorityP4})
	setOneHot(values[16:20], frame.EpisodePhase, []string{V5PhaseInitial, V5PhaseCandidate, V5PhaseConfirmed, V5PhaseFinal})
	setOneHot(values[20:25], frame.Enrichment, []string{V5EnrichmentUnavailable, V5EnrichmentNotRequested, V5EnrichmentUncertain, V5EnrichmentRecognized, V5EnrichmentUnknown})
	values[25] = normalizeSeconds(frame.Continuity.SecondsSinceFirstObservation, 300)
	values[26] = normalizeSeconds(frame.Continuity.SecondsSinceLastObservation, 300)
	values[27] = normalizeCount(frame.Continuity.SegmentCount, 32)
	values[28] = normalizeCount(frame.Continuity.GapCount, 16)
	values[29] = normalizeSeconds(frame.Continuity.CalmSeconds, 300)
	values[30] = boolV5(frame.Quality.RealDetection)
	values[31] = boolV5(frame.Quality.ReplaySimulation)
	values[32] = normalizeCount(frame.Quality.ObservationCount, 32)
	values[33] = frame.Quality.AggregateConfidence
	setOneHot(values[34:38], frame.CoEvidence.AccessState, []string{V5AccessUnknown, V5AccessClosed, V5AccessOpen, V5AccessForced})
	values[38] = boolV5(frame.CoEvidence.Movement)
	values[39] = boolV5(frame.CoEvidence.SensorEvidence)
	setOneHot(values[40:44], frame.CoEvidence.AlarmState, []string{V5AlarmUnknown, V5AlarmArmed, V5AlarmDisarmed, V5AlarmTriggered})
	return encoded, nil
}

func (e EncodedStateV5) Validate() error {
	if e.SchemaVersion != StateEncoderV5SchemaVersion || e.EncoderID != EncoderV5ID || e.EncoderVersion != EncoderV5Version || e.DType != "float32" || e.Shape[0] != EncoderV5Size {
		return fmt.Errorf("invalid encoded stateframe v5 metadata")
	}
	for i, name := range e.FeatureNames {
		if name != EncoderV5FeatureNames[i] {
			return fmt.Errorf("stateframe v5 feature order changed at %d", i)
		}
	}
	return nil
}

func (e EncodedStateV5) ValuesSlice() []float32 { return e.Values[:] }

func boolV5(value bool) float32 {
	if value {
		return 1
	}
	return 0
}

func boundedNonNegative(value float32) float32 {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
		return 0
	}
	return value
}

func clampV5(value float32) float32 {
	value = boundedNonNegative(value)
	if value > 1 {
		return 1
	}
	return value
}

func normalizeSeconds(value, maximum float32) float32 {
	value = boundedNonNegative(value)
	if value > maximum {
		return 1
	}
	return value / maximum
}

func normalizeCount(value, maximum int) float32 {
	if value <= 0 {
		return 0
	}
	if value >= maximum {
		return 1
	}
	return float32(value) / float32(maximum)
}

func setOneHot(target []float32, value string, vocabulary []string) {
	for index := range target {
		if index < len(vocabulary) && value == vocabulary[index] {
			target[index] = 1
			return
		}
	}
}

func normalizeV5Topology(value string) string {
	if !validV5Topology(value) {
		return contract.VisionTopologyUnknown
	}
	return value
}

func normalizeV5Priority(value string) string {
	if value == "" || value == "none" {
		return contract.VisionPriorityP4
	}
	if value == "P0" {
		return contract.VisionPriorityP0
	}
	if value == "P1" {
		return contract.VisionPriorityP1
	}
	if value == "P2" {
		return contract.VisionPriorityP2
	}
	if value == "P3" {
		return contract.VisionPriorityP3
	}
	if value == "P4" {
		return contract.VisionPriorityP4
	}
	if !validV5Priority(value) {
		return contract.VisionPriorityP4
	}
	return value
}

func normalizeV5Phase(value string) string {
	if !validV5Phase(value) {
		return V5PhaseInitial
	}
	return value
}

func normalizeV5Enrichment(value string) string {
	if !validV5Enrichment(value) {
		return V5EnrichmentUnavailable
	}
	return value
}

func normalizeV5Access(value string) string {
	if !validV5Access(value) {
		return V5AccessUnknown
	}
	return value
}

func normalizeV5Alarm(value string) string {
	if !validV5Alarm(value) {
		return V5AlarmUnknown
	}
	return value
}

func validV5Topology(value string) bool {
	return contract.ValidVisionTopologyClass(value)
}

func validV5Priority(value string) bool {
	return contract.ValidVisionPriorityHint(value)
}

func validV5Phase(value string) bool {
	return value == V5PhaseInitial || value == V5PhaseCandidate || value == V5PhaseConfirmed || value == V5PhaseFinal
}

func validV5Enrichment(value string) bool {
	return value == V5EnrichmentUnavailable || value == V5EnrichmentNotRequested || value == V5EnrichmentUncertain || value == V5EnrichmentRecognized || value == V5EnrichmentUnknown
}

func validV5Access(value string) bool {
	return value == V5AccessUnknown || value == V5AccessClosed || value == V5AccessOpen || value == V5AccessForced
}

func validV5Alarm(value string) bool {
	return value == V5AlarmUnknown || value == V5AlarmArmed || value == V5AlarmDisarmed || value == V5AlarmTriggered
}

func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
