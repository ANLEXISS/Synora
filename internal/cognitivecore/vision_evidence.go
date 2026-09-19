package cognitivecore

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

// Vision evidence V1 is a model-independent projection of normalized facts.
// It is deliberately separate from the active MLP input contract.
const (
	VisionEvidenceSchemaVersion  = "vision-evidence/v1"
	VisionEvidenceCaptureSchema  = "synora.vision-evidence-capture/v1"
	VisionEvidenceEncoderID      = "synora-state-encoder"
	VisionEvidenceEncoderVersion = "1.0.0"
	VisionEvidenceSize           = 44
)

const (
	VisionPriorityOriginCore    = "core"
	VisionPriorityOriginVision  = "vision"
	VisionPriorityOriginUnknown = "unknown"
)

const (
	VisionPhaseInitial   = "initial"
	VisionPhaseCandidate = "candidate"
	VisionPhaseConfirmed = "confirmed"
	VisionPhaseFinal     = "final"

	VisionEnrichmentUnavailable  = "unavailable"
	VisionEnrichmentNotRequested = "not_requested"
	VisionEnrichmentUncertain    = "uncertain"
	VisionEnrichmentRecognized   = "recognized"
	VisionEnrichmentUnknown      = "unknown"

	VisionAccessUnknown = "unknown"
	VisionAccessClosed  = "closed"
	VisionAccessOpen    = "open"
	VisionAccessForced  = "forced"

	VisionAlarmUnknown   = "unknown"
	VisionAlarmArmed     = "armed"
	VisionAlarmDisarmed  = "disarmed"
	VisionAlarmTriggered = "triggered"
)

// VisionEvidenceFrame contains only normalized, deterministic facts. It contains no
// event, device, camera, track, identity, media, or biometric data.
type VisionEvidenceFrame struct {
	SchemaVersion  string                        `json:"schema_version"`
	CapturedAt     time.Time                     `json:"captured_at"`
	Security       VisionEvidenceFrameSecurity   `json:"security"`
	Presence       VisionEvidenceFramePresence   `json:"presence"`
	TopologyClass  string                        `json:"topology_class"`
	Priority       string                        `json:"priority"`
	PriorityOrigin string                        `json:"priority_origin"`
	EpisodePhase   string                        `json:"episode_phase"`
	Enrichment     string                        `json:"enrichment_status"`
	Continuity     VisionEvidenceFrameContinuity `json:"continuity"`
	Quality        VisionEvidenceFrameQuality    `json:"quality"`
	CoEvidence     VisionEvidenceFrameCoEvidence `json:"co_evidence"`
}

type VisionEvidenceFrameSecurity struct {
	Armed    bool `json:"armed"`
	Degraded bool `json:"degraded"`
	Known    bool `json:"known"`
}

type VisionEvidenceFramePresence struct {
	HumanPresent   bool `json:"human_present"`
	TrackCount     int  `json:"track_count"`
	TrackConfirmed bool `json:"track_confirmed"`
}

type VisionEvidenceFrameContinuity struct {
	SecondsSinceFirstObservation float32 `json:"seconds_since_first_observation"`
	SecondsSinceLastObservation  float32 `json:"seconds_since_last_observation"`
	SegmentCount                 int     `json:"segment_count"`
	GapCount                     int     `json:"gap_count"`
	CalmSeconds                  float32 `json:"calm_seconds"`
}

type VisionEvidenceFrameQuality struct {
	RealDetection       bool    `json:"real_detection"`
	ReplaySimulation    bool    `json:"replay_simulation"`
	ObservationCount    int     `json:"observation_count"`
	AggregateConfidence float32 `json:"aggregate_confidence"`
}

type VisionEvidenceFrameCoEvidence struct {
	AccessState    string `json:"access_state"`
	Movement       bool   `json:"movement"`
	SensorEvidence bool   `json:"sensor_evidence"`
	AlarmState     string `json:"alarm_state"`
}

// VisionEvidenceFeatureNames is the frozen order. Do not reorder or insert a
// feature; append-only changes require a new encoder schema.
var VisionEvidenceFeatureNames = [...]string{
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

type EncodedVisionEvidence struct {
	SchemaVersion  string                      `json:"schema_version"`
	EncoderID      string                      `json:"encoder_id"`
	EncoderVersion string                      `json:"encoder_version"`
	DType          string                      `json:"dtype"`
	Shape          [1]int                      `json:"shape"`
	FeatureNames   [VisionEvidenceSize]string  `json:"feature_names"`
	Values         [VisionEvidenceSize]float32 `json:"values"`
	FrameChecksum  string                      `json:"frame_checksum"`
}

func (f VisionEvidenceFrame) Normalized() VisionEvidenceFrame {
	if f.CapturedAt.IsZero() {
		f.CapturedAt = time.Unix(0, 0).UTC()
	} else {
		f.CapturedAt = f.CapturedAt.UTC()
	}
	f.SchemaVersion = VisionEvidenceSchemaVersion
	f.TopologyClass = normalizeVisionTopology(f.TopologyClass)
	f.Priority = normalizeVisionPriority(f.Priority)
	if f.PriorityOrigin == "" {
		f.PriorityOrigin = VisionPriorityOriginUnknown
	}
	f.EpisodePhase = normalizeVisionPhase(f.EpisodePhase)
	f.Enrichment = normalizeVisionEnrichment(f.Enrichment)
	f.Continuity.SecondsSinceFirstObservation = boundedNonNegative(f.Continuity.SecondsSinceFirstObservation)
	f.Continuity.SecondsSinceLastObservation = boundedNonNegative(f.Continuity.SecondsSinceLastObservation)
	f.Continuity.SegmentCount = maxVisionInt(f.Continuity.SegmentCount, 0)
	f.Continuity.GapCount = maxVisionInt(f.Continuity.GapCount, 0)
	f.Continuity.CalmSeconds = boundedNonNegative(f.Continuity.CalmSeconds)
	f.Presence.TrackCount = maxVisionInt(f.Presence.TrackCount, 0)
	f.Quality.ObservationCount = maxVisionInt(f.Quality.ObservationCount, 0)
	f.Quality.AggregateConfidence = clampVision(f.Quality.AggregateConfidence)
	f.CoEvidence.AccessState = normalizeVisionAccess(f.CoEvidence.AccessState)
	f.CoEvidence.AlarmState = normalizeVisionAlarm(f.CoEvidence.AlarmState)
	return f
}

func (f VisionEvidenceFrame) Validate() error {
	if f.SchemaVersion != VisionEvidenceSchemaVersion {
		return fmt.Errorf("invalid vision evidence schema %q", f.SchemaVersion)
	}
	if f.CapturedAt.IsZero() || f.CapturedAt.Location() != time.UTC {
		return fmt.Errorf("vision evidence captured_at must be UTC")
	}
	if !validVisionTopology(f.TopologyClass) || !validVisionPriority(f.Priority) || !validVisionPhase(f.EpisodePhase) || !validVisionEnrichment(f.Enrichment) || !validVisionAccess(f.CoEvidence.AccessState) || !validVisionAlarm(f.CoEvidence.AlarmState) {
		return fmt.Errorf("vision evidence contains an invalid categorical value")
	}
	if f.PriorityOrigin != VisionPriorityOriginCore && f.PriorityOrigin != VisionPriorityOriginVision && f.PriorityOrigin != VisionPriorityOriginUnknown {
		return fmt.Errorf("invalid vision evidence priority origin %q", f.PriorityOrigin)
	}
	if f.Priority == contract.VisionPriorityP0 && f.PriorityOrigin != VisionPriorityOriginCore {
		return fmt.Errorf("P0 is Core-only and cannot be created by Vision")
	}
	for _, value := range []float32{f.Continuity.SecondsSinceFirstObservation, f.Continuity.SecondsSinceLastObservation, f.Continuity.CalmSeconds, f.Quality.AggregateConfidence} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
			return fmt.Errorf("invalid vision evidence numeric fact")
		}
	}
	if f.Quality.AggregateConfidence > 1 || f.Presence.TrackCount < 0 || f.Continuity.SegmentCount < 0 || f.Continuity.GapCount < 0 || f.Quality.ObservationCount < 0 {
		return fmt.Errorf("invalid vision evidence bounded fact")
	}
	return nil
}

func (f VisionEvidenceFrame) CanonicalJSON() ([]byte, error) {
	return json.Marshal(f.Normalized())
}

func (f VisionEvidenceFrame) Fingerprint() (string, error) {
	body, err := f.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

type VisionEvidenceEncoder struct{}

func (VisionEvidenceEncoder) ID() string      { return VisionEvidenceEncoderID }
func (VisionEvidenceEncoder) Version() string { return VisionEvidenceEncoderVersion }

// Encode computes only the fixed numeric vector. It does not serialize the
// frame or allocate a variable-length Values slice. Fingerprint is separate so
// normal execution can avoid canonical JSON work.
func (e VisionEvidenceEncoder) Encode(ctx context.Context, frame VisionEvidenceFrame) (EncodedVisionEvidence, error) {
	if err := ctx.Err(); err != nil {
		return EncodedVisionEvidence{}, err
	}
	frame = frame.Normalized()
	if err := frame.Validate(); err != nil {
		return EncodedVisionEvidence{}, err
	}
	encoded := EncodedVisionEvidence{SchemaVersion: VisionEvidenceSchemaVersion, EncoderID: e.ID(), EncoderVersion: e.Version(), DType: "float32", Shape: [1]int{VisionEvidenceSize}}
	encoded.FeatureNames = VisionEvidenceFeatureNames
	values := &encoded.Values
	values[0] = boolVision(frame.Security.Armed)
	values[1] = boolVision(frame.Security.Degraded)
	values[2] = boolVision(frame.Security.Known)
	values[3] = boolVision(frame.Presence.HumanPresent)
	values[4] = normalizeVisionCount(frame.Presence.TrackCount, 16)
	values[5] = boolVision(frame.Presence.TrackConfirmed)
	setVisionOneHot(values[6:11], frame.TopologyClass, []string{contract.VisionTopologyPublicOutdoor, contract.VisionTopologyPrivatePerimeter, contract.VisionTopologyRestrictedThreshold, contract.VisionTopologyProtectedInterior, contract.VisionTopologyUnknown})
	setVisionOneHot(values[11:16], frame.Priority, []string{contract.VisionPriorityP0, contract.VisionPriorityP1, contract.VisionPriorityP2, contract.VisionPriorityP3, contract.VisionPriorityP4})
	setVisionOneHot(values[16:20], frame.EpisodePhase, []string{VisionPhaseInitial, VisionPhaseCandidate, VisionPhaseConfirmed, VisionPhaseFinal})
	setVisionOneHot(values[20:25], frame.Enrichment, []string{VisionEnrichmentUnavailable, VisionEnrichmentNotRequested, VisionEnrichmentUncertain, VisionEnrichmentRecognized, VisionEnrichmentUnknown})
	values[25] = normalizeVisionSeconds(frame.Continuity.SecondsSinceFirstObservation, 300)
	values[26] = normalizeVisionSeconds(frame.Continuity.SecondsSinceLastObservation, 300)
	values[27] = normalizeVisionCount(frame.Continuity.SegmentCount, 32)
	values[28] = normalizeVisionCount(frame.Continuity.GapCount, 16)
	values[29] = normalizeVisionSeconds(frame.Continuity.CalmSeconds, 300)
	values[30] = boolVision(frame.Quality.RealDetection)
	values[31] = boolVision(frame.Quality.ReplaySimulation)
	values[32] = normalizeVisionCount(frame.Quality.ObservationCount, 32)
	values[33] = frame.Quality.AggregateConfidence
	setVisionOneHot(values[34:38], frame.CoEvidence.AccessState, []string{VisionAccessUnknown, VisionAccessClosed, VisionAccessOpen, VisionAccessForced})
	values[38] = boolVision(frame.CoEvidence.Movement)
	values[39] = boolVision(frame.CoEvidence.SensorEvidence)
	setVisionOneHot(values[40:44], frame.CoEvidence.AlarmState, []string{VisionAlarmUnknown, VisionAlarmArmed, VisionAlarmDisarmed, VisionAlarmTriggered})
	return encoded, nil
}

func (e EncodedVisionEvidence) Validate() error {
	if e.SchemaVersion != VisionEvidenceSchemaVersion || e.EncoderID != VisionEvidenceEncoderID || e.EncoderVersion != VisionEvidenceEncoderVersion || e.DType != "float32" || e.Shape[0] != VisionEvidenceSize {
		return fmt.Errorf("invalid encoded vision evidence metadata")
	}
	for i, name := range e.FeatureNames {
		if name != VisionEvidenceFeatureNames[i] {
			return fmt.Errorf("vision evidence feature order changed at %d", i)
		}
	}
	return nil
}

func (e EncodedVisionEvidence) ValuesSlice() []float32 { return e.Values[:] }

func boolVision(value bool) float32 {
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

func clampVision(value float32) float32 {
	value = boundedNonNegative(value)
	if value > 1 {
		return 1
	}
	return value
}

func normalizeVisionSeconds(value, maximum float32) float32 {
	value = boundedNonNegative(value)
	if value > maximum {
		return 1
	}
	return value / maximum
}

func normalizeVisionCount(value, maximum int) float32 {
	if value <= 0 {
		return 0
	}
	if value >= maximum {
		return 1
	}
	return float32(value) / float32(maximum)
}

func setVisionOneHot(target []float32, value string, vocabulary []string) {
	for index := range target {
		if index < len(vocabulary) && value == vocabulary[index] {
			target[index] = 1
			return
		}
	}
}

func normalizeVisionTopology(value string) string {
	if !validVisionTopology(value) {
		return contract.VisionTopologyUnknown
	}
	return value
}

func normalizeVisionPriority(value string) string {
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
	if !validVisionPriority(value) {
		return contract.VisionPriorityP4
	}
	return value
}

func normalizeVisionPhase(value string) string {
	if !validVisionPhase(value) {
		return VisionPhaseInitial
	}
	return value
}

func normalizeVisionEnrichment(value string) string {
	if !validVisionEnrichment(value) {
		return VisionEnrichmentUnavailable
	}
	return value
}

func normalizeVisionAccess(value string) string {
	if !validVisionAccess(value) {
		return VisionAccessUnknown
	}
	return value
}

func normalizeVisionAlarm(value string) string {
	if !validVisionAlarm(value) {
		return VisionAlarmUnknown
	}
	return value
}

func validVisionTopology(value string) bool {
	return contract.ValidVisionTopologyClass(value)
}

func validVisionPriority(value string) bool {
	return contract.ValidVisionPriorityHint(value)
}

func validVisionPhase(value string) bool {
	return value == VisionPhaseInitial || value == VisionPhaseCandidate || value == VisionPhaseConfirmed || value == VisionPhaseFinal
}

func validVisionEnrichment(value string) bool {
	return value == VisionEnrichmentUnavailable || value == VisionEnrichmentNotRequested || value == VisionEnrichmentUncertain || value == VisionEnrichmentRecognized || value == VisionEnrichmentUnknown
}

func validVisionAccess(value string) bool {
	return value == VisionAccessUnknown || value == VisionAccessClosed || value == VisionAccessOpen || value == VisionAccessForced
}

func validVisionAlarm(value string) bool {
	return value == VisionAlarmUnknown || value == VisionAlarmArmed || value == VisionAlarmDisarmed || value == VisionAlarmTriggered
}

func maxVisionInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
