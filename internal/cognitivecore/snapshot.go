// Package cognitivecore contains the only V1 decision runtime. It deliberately
// has no dependency on the historical CGE or decision Engine packages.
package cognitivecore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
	"synora/pkg/contract"
)

const (
	SnapshotSchemaVersion = "cognitive-snapshot/v1"
	EncoderSchemaVersion  = "cognitive-encoder/v1"
	CognitiveVectorSize   = 80
)

var CognitiveFeatureNames = cognitiveFeatureNames()

type SecurityFacts struct {
	Armed    bool `json:"armed"`
	Degraded bool `json:"degraded"`
	Known    bool `json:"known"`
}

type PresenceFacts struct {
	HumanPresent          bool `json:"human_present"`
	KnownResidentsPresent bool `json:"known_residents_present"`
	KnownResidentCount    int  `json:"known_resident_count"`
	TrackCount            int  `json:"track_count"`
	TrackConfirmed        bool `json:"track_confirmed"`
}

type CapabilityFact struct {
	Capability string `json:"capability"`
	Topology   string `json:"topology,omitempty"`
	Available  bool   `json:"available"`
}

type SensorFacts struct {
	Movement         bool    `json:"movement"`
	AccessState      string  `json:"access_state"`
	SensorEvidence   bool    `json:"sensor_evidence"`
	AlarmState       string  `json:"alarm_state"`
	ObservationCount int     `json:"observation_count"`
	Confidence       float32 `json:"confidence"`
}

type EpisodeFacts struct {
	Phase             string  `json:"phase"`
	SegmentCount      int     `json:"segment_count"`
	GapCount          int     `json:"gap_count"`
	SecondsSinceFirst float32 `json:"seconds_since_first"`
	SecondsSinceLast  float32 `json:"seconds_since_last"`
	CalmSeconds       float32 `json:"calm_seconds"`
}

type ActionResultFact struct {
	Status      string `json:"status"`
	Successful  bool   `json:"successful"`
	Failed      bool   `json:"failed"`
	Unavailable bool   `json:"unavailable"`
}

// CognitiveSnapshot contains normalized facts only. In particular it has no
// frame, bbox, crop, identity, embedding, media or hardware identifier.
type CognitiveSnapshot struct {
	SchemaVersion       string                           `json:"schema_version"`
	Revision            uint64                           `json:"revision"`
	CapturedAt          time.Time                        `json:"captured_at"`
	Security            SecurityFacts                    `json:"security"`
	Presence            PresenceFacts                    `json:"presence"`
	Topology            string                           `json:"topology"`
	Capabilities        []CapabilityFact                 `json:"capabilities,omitempty"`
	Sensors             SensorFacts                      `json:"sensors"`
	Episode             EpisodeFacts                     `json:"episode"`
	PreviousDanger      float32                          `json:"previous_danger"`
	PreviousDangerKnown bool                             `json:"previous_danger_known"`
	ActionResults       []ActionResultFact               `json:"action_results,omitempty"`
	VisionEvidence      [cognitive.EncoderV5Size]float32 `json:"vision_evidence"`
}

func (s CognitiveSnapshot) Normalized() CognitiveSnapshot {
	s.SchemaVersion = SnapshotSchemaVersion
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
	s.Sensors.AccessState = normalizeAccess(s.Sensors.AccessState)
	s.Sensors.AlarmState = normalizeAlarm(s.Sensors.AlarmState)
	s.Episode.Phase = normalizePhase(s.Episode.Phase)
	s.PreviousDanger = clamp01(s.PreviousDanger)
	s.Sensors.Confidence = clamp01(s.Sensors.Confidence)
	for i := range s.VisionEvidence {
		s.VisionEvidence[i] = clamp01(s.VisionEvidence[i])
	}
	s.Capabilities = append([]CapabilityFact(nil), s.Capabilities...)
	s.ActionResults = append([]ActionResultFact(nil), s.ActionResults...)
	sort.Slice(s.Capabilities, func(i, j int) bool {
		if s.Capabilities[i].Capability != s.Capabilities[j].Capability {
			return s.Capabilities[i].Capability < s.Capabilities[j].Capability
		}
		return s.Capabilities[i].Topology < s.Capabilities[j].Topology
	})
	if len(s.Capabilities) > 32 {
		s.Capabilities = s.Capabilities[:32]
	}
	if len(s.ActionResults) > 16 {
		s.ActionResults = s.ActionResults[len(s.ActionResults)-16:]
	}
	return s
}

func (s CognitiveSnapshot) Validate() error {
	s = s.Normalized()
	if s.SchemaVersion != SnapshotSchemaVersion || s.CapturedAt.IsZero() {
		return fmt.Errorf("invalid cognitive snapshot metadata")
	}
	if !contract.ValidVisionTopologyClass(s.Topology) {
		return fmt.Errorf("invalid cognitive topology")
	}
	for _, value := range s.VisionEvidence {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("non-finite vision evidence")
		}
	}
	return nil
}

func (s CognitiveSnapshot) CanonicalJSON() ([]byte, error) { return json.Marshal(s.Normalized()) }

type EncodedSnapshot struct {
	SchemaVersion  string                       `json:"schema_version"`
	EncoderVersion string                       `json:"encoder_version"`
	Shape          [1]int                       `json:"shape"`
	FeatureNames   [CognitiveVectorSize]string  `json:"feature_names"`
	Values         [CognitiveVectorSize]float32 `json:"values"`
}

func (e EncodedSnapshot) Validate() error {
	if e.SchemaVersion != EncoderSchemaVersion || e.EncoderVersion != "1.0.0" || e.Shape[0] != CognitiveVectorSize {
		return fmt.Errorf("invalid cognitive encoder metadata")
	}
	if e.FeatureNames != CognitiveFeatureNames {
		return fmt.Errorf("cognitive feature order changed")
	}
	return nil
}

type SnapshotEncoder struct{}

func (SnapshotEncoder) Encode(ctx context.Context, snapshot CognitiveSnapshot) (EncodedSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return EncodedSnapshot{}, err
	}
	snapshot = snapshot.Normalized()
	if err := snapshot.Validate(); err != nil {
		return EncodedSnapshot{}, err
	}
	encoded := EncodedSnapshot{SchemaVersion: EncoderSchemaVersion, EncoderVersion: "1.0.0", Shape: [1]int{CognitiveVectorSize}, FeatureNames: CognitiveFeatureNames}
	v := &encoded.Values
	v[0] = boolFloat(snapshot.Security.Armed)
	v[1] = boolFloat(snapshot.Security.Degraded)
	v[2] = boolFloat(snapshot.Security.Known)
	v[3] = boolFloat(snapshot.Presence.HumanPresent)
	v[4] = boolFloat(snapshot.Presence.KnownResidentsPresent)
	v[5] = normalizeCount(snapshot.Presence.KnownResidentCount, 16)
	v[6] = normalizeCount(snapshot.Presence.TrackCount, 16)
	v[7] = boolFloat(snapshot.Presence.TrackConfirmed)
	setOneHot(v[8:13], snapshot.Topology, []string{contract.VisionTopologyPublicOutdoor, contract.VisionTopologyPrivatePerimeter, contract.VisionTopologyRestrictedThreshold, contract.VisionTopologyProtectedInterior, contract.VisionTopologyUnknown})
	setOneHot(v[13:17], snapshot.Episode.Phase, []string{cognitive.V5PhaseInitial, cognitive.V5PhaseCandidate, cognitive.V5PhaseConfirmed, cognitive.V5PhaseFinal})
	v[17] = normalizeCount(snapshot.Episode.SegmentCount, 32)
	v[18] = normalizeCount(snapshot.Episode.GapCount, 16)
	v[19] = normalizeSeconds(snapshot.Episode.SecondsSinceFirst, 300)
	v[20] = normalizeSeconds(snapshot.Episode.SecondsSinceLast, 300)
	v[21] = normalizeSeconds(snapshot.Episode.CalmSeconds, 300)
	v[22] = snapshot.PreviousDanger
	v[23] = boolFloat(snapshot.PreviousDangerKnown)
	v[24] = boolFloat(snapshot.Sensors.Movement)
	setOneHot(v[25:29], snapshot.Sensors.AccessState, []string{"unknown", "closed", "open", "forced"})
	v[29] = boolFloat(snapshot.Sensors.SensorEvidence)
	setOneHot(v[30:34], snapshot.Sensors.AlarmState, []string{"unknown", "armed", "disarmed", "triggered"})
	v[34] = normalizeCount(snapshot.Sensors.ObservationCount, 32)
	v[35] = snapshot.Sensors.Confidence
	v[36] = normalizeCount(len(snapshot.Capabilities), 32)
	v[37] = normalizeCount(countAvailable(snapshot.Capabilities), 32)
	v[38] = normalizeCount(len(snapshot.ActionResults), 16)
	v[39] = normalizeCount(countAction(snapshot.ActionResults, "success"), 16)
	v[40] = normalizeCount(countAction(snapshot.ActionResults, "failed"), 16)
	v[41] = normalizeCount(countAction(snapshot.ActionResults, "unavailable"), 16)
	copy(v[42:], snapshot.VisionEvidence[:])
	return encoded, nil
}

func cognitiveFeatureNames() [CognitiveVectorSize]string {
	var names [CognitiveVectorSize]string
	base := []string{"security.armed", "security.degraded", "security.known", "presence.human", "presence.known_residents", "presence.known_resident_count", "presence.track_count", "presence.track_confirmed", "topology.public_outdoor", "topology.private_perimeter", "topology.restricted_threshold", "topology.protected_interior", "topology.unknown", "episode.initial", "episode.candidate", "episode.confirmed", "episode.final", "episode.segment_count", "episode.gap_count", "episode.seconds_since_first", "episode.seconds_since_last", "episode.calm_seconds", "danger.previous", "danger.previous_known", "sensor.movement", "sensor.access_unknown", "sensor.access_closed", "sensor.access_open", "sensor.access_forced", "sensor.evidence", "sensor.alarm_unknown", "sensor.alarm_armed", "sensor.alarm_disarmed", "sensor.alarm_triggered", "sensor.observation_count", "sensor.confidence", "capability.count", "capability.available_count", "action_result.count", "action_result.success_count", "action_result.failed_count", "action_result.unavailable_count"}
	copy(names[:], base)
	for i, name := range cognitive.EncoderV5FeatureNames {
		names[42+i] = "vision." + name
	}
	return names
}

func VisionEvidenceFromFrame(ctx context.Context, frame cognitive.StateFrameV5) ([cognitive.EncoderV5Size]float32, error) {
	encoded, err := (cognitive.V5StateEncoder{}).Encode(ctx, frame)
	if err != nil {
		return [cognitive.EncoderV5Size]float32{}, err
	}
	return encoded.Values, nil
}
func boolFloat(value bool) float32 {
	if value {
		return 1
	}
	return 0
}
func clamp01(value float32) float32 {
	if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
func normalizeCount(value, max int) float32 {
	if value <= 0 {
		return 0
	}
	if value >= max {
		return 1
	}
	return float32(value) / float32(max)
}
func normalizeSeconds(value float32, max float32) float32 {
	if value <= 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
		return 0
	}
	if value >= max {
		return 1
	}
	return value / max
}
func setOneHot(target []float32, value string, vocabulary []string) {
	for i := range target {
		if i < len(vocabulary) && value == vocabulary[i] {
			target[i] = 1
			return
		}
	}
}
func countAvailable(values []CapabilityFact) int {
	n := 0
	for _, value := range values {
		if value.Available {
			n++
		}
	}
	return n
}
func countAction(values []ActionResultFact, status string) int {
	n := 0
	for _, value := range values {
		if value.Status == status || (status == "success" && value.Successful) || (status == "failed" && value.Failed) || (status == "unavailable" && value.Unavailable) {
			n++
		}
	}
	return n
}
func maxInt(value, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}
func normalizeTopology(value string) string {
	if !contract.ValidVisionTopologyClass(value) {
		return contract.VisionTopologyUnknown
	}
	return value
}
func normalizeAccess(value string) string {
	switch value {
	case "closed", "open", "forced":
		return value
	default:
		return "unknown"
	}
}
func normalizeAlarm(value string) string {
	switch value {
	case "armed", "disarmed", "triggered":
		return value
	default:
		return "unknown"
	}
}
func normalizePhase(value string) string {
	switch value {
	case cognitive.V5PhaseCandidate, cognitive.V5PhaseConfirmed, cognitive.V5PhaseFinal:
		return value
	default:
		return cognitive.V5PhaseInitial
	}
}
func (s CognitiveSnapshot) String() string { return strings.TrimSpace(s.Topology) }
