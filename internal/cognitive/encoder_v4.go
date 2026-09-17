package cognitive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// EncoderV4Frame is the model-independent frame used by the training bundle.
// It is intentionally made of observed facts and temporal context only.
type EncoderV4Frame struct {
	At                    time.Time                `json:"at"`
	SecurityMode          string                   `json:"security_mode"`
	KnownResidentsPresent bool                     `json:"known_residents_present"`
	KnownResidentCount    int                      `json:"known_resident_count"`
	ContextFacts          []string                 `json:"context_facts"`
	AvailableCapabilities []string                 `json:"observed_capabilities"`
	AvailableActionIDs    []string                 `json:"available_action_ids"`
	Observations          []EncoderV4Observation   `json:"observations"`
	Temporal              EncoderV4TemporalContext `json:"temporal"`
}

type EncoderV4Observation struct {
	OccurredAt     time.Time `json:"occurred_at"`
	TopologyTokens []string  `json:"topology_tokens"`
	Kind           string    `json:"kind"`
	Phase          string    `json:"phase"`
	SubjectClass   string    `json:"subject_class"`
	IdentityState  string    `json:"identity_state"`
}

type EncoderV4TemporalContext struct {
	EvaluationTrigger       string  `json:"evaluation_trigger"`
	QuietSeconds            float32 `json:"quiet_seconds"`
	SecondsSinceLastEvent   float32 `json:"seconds_since_last_event"`
	EventCount              int     `json:"event_count"`
	SequenceDurationSeconds float32 `json:"sequence_duration_seconds"`
}

const (
	EncoderV4ID      = "synora-state-encoder"
	EncoderV4Version = "4.0.0"
	EncoderV4Size    = 477
)

// V4StateEncoder is the runtime implementation of the exact fixed-width
// state-encoder/v4 contract shipped with the training bundle.
type V4StateEncoder struct{}

func (V4StateEncoder) ID() string      { return EncoderV4ID }
func (V4StateEncoder) Version() string { return EncoderV4Version }

func (e V4StateEncoder) Encode(ctx context.Context, frame StateFrame) (EncodedState, error) {
	if err := ctx.Err(); err != nil {
		return EncodedState{}, err
	}
	if err := frame.Validate(); err != nil {
		return EncodedState{}, err
	}
	frame = frame.Normalized()
	values := EncodeV4Frame(FrameV4FromStateFrame(frame))
	if len(values) != EncoderV4Size {
		return EncodedState{}, fmt.Errorf("state encoder v4 emitted %d features, want %d", len(values), EncoderV4Size)
	}
	canonical, err := frame.CanonicalJSON()
	if err != nil {
		return EncodedState{}, err
	}
	checksum := sha256.Sum256(canonical)
	names := make([]string, len(values))
	for i := range names {
		names[i] = fmt.Sprintf("v4.%03d", i)
	}
	return EncodedState{
		SchemaVersion: StateEncoderSchemaVersion,
		EncoderID:     EncoderV4ID, EncoderVersion: EncoderV4Version,
		DType: "float32", Shape: []int{len(values)}, FeatureNames: names,
		Values: values, FrameChecksum: hex.EncodeToString(checksum[:]),
	}, nil
}

// FrameV4FromStateFrame is the deterministic bridge from Core's frozen
// snapshot to the training contract. It never consults a model or policy.
func FrameV4FromStateFrame(frame StateFrame) EncoderV4Frame {
	securityMode := frame.SecurityMode
	if securityMode == "" {
		if frame.System.Armed {
			securityMode = "armed_away"
		} else {
			securityMode = "disarmed"
		}
	}
	observations := make([]EncoderV4Observation, 0, len(frame.RecentEvents)+1)
	if frame.CurrentEvent != nil {
		observations = append(observations, v4ObservationFromEvent(*frame.CurrentEvent, frame.CapturedAt))
	}
	for _, event := range frame.RecentEvents {
		if frame.CurrentEvent != nil && event.ID == frame.CurrentEvent.ID {
			continue
		}
		observations = append(observations, v4ObservationFromEvent(event, frame.CapturedAt))
	}
	sort.SliceStable(observations, func(i, j int) bool { return observations[i].OccurredAt.After(observations[j].OccurredAt) })
	if len(observations) > 8 {
		observations = observations[:8]
	}
	residentCount := frame.KnownResidentCount
	if residentCount == 0 {
		residentCount = len(frame.Residents)
	}
	known := frame.KnownResidentsPresent
	if !known {
		for _, resident := range frame.Residents {
			if resident.Enabled && resident.State != "" && resident.State != "absent" {
				known = true
				break
			}
		}
	}
	facts := append([]string(nil), frame.ContextFacts...)
	if frame.System.Armed {
		facts = append(facts, "access.outside_only")
	}
	if frame.System.DangerLevel == "high" || frame.System.DangerLevel == "critical" {
		facts = append(facts, "safety.immediate")
	}
	return EncoderV4Frame{
		At: frame.CapturedAt, SecurityMode: securityMode,
		KnownResidentsPresent: known, KnownResidentCount: residentCount,
		ContextFacts: facts, AvailableCapabilities: append([]string(nil), frame.AvailableCapabilities...),
		AvailableActionIDs: append([]string(nil), frame.AvailableActionIDs...),
		Observations:       observations, Temporal: frame.Temporal,
	}
}

func v4ObservationFromEvent(event StateEvent, at time.Time) EncoderV4Observation {
	kind := strings.ToLower(event.Type)
	observationKind := "system.sensor"
	for _, candidate := range []struct{ token, value string }{
		{"door", "access.door"}, {"window", "access.window"}, {"gate", "access.gate"}, {"lock", "access.lock"}, {"glass", "access.glass"},
		{"contact", "access.contact"}, {"motion", "presence.motion"}, {"person", "presence.person"}, {"vehicle", "presence.vehicle"},
		{"animal", "presence.animal"}, {"delivery", "presence.delivery"}, {"smoke", "safety.smoke"}, {"co", "safety.co"},
		{"fire", "safety.fire"}, {"gas", "safety.gas"}, {"panic", "safety.panic"}, {"fall", "safety.fall"},
		{"tamper", "security.tamper"}, {"weapon", "security.weapon"}, {"camera", "system.camera"}, {"weather", "environment.weather"},
		{"power", "environment.power"}, {"appliance", "device.appliance"},
	} {
		if strings.Contains(kind, candidate.token) {
			observationKind = candidate.value
			break
		}
	}
	phase := "updated"
	for _, candidate := range []struct{ token, value string }{
		{"open", "opened"}, {"detect", "detected"}, {"close", "closed"}, {"active", "active"}, {"resolve", "resolved"},
		{"clear", "cleared"}, {"trigger", "triggered"}, {"restore", "restored"}, {"offline", "offline"}, {"fault", "fault"},
		{"secure", "secure"},
	} {
		if strings.Contains(kind, candidate.token) {
			phase = candidate.value
			break
		}
	}
	tokens := []string{}
	zone := strings.ToLower(event.NodeID)
	if strings.Contains(zone, "interior") || strings.Contains(zone, "room") || strings.Contains(zone, "inside") {
		tokens = append(tokens, "interior")
	} else if strings.Contains(zone, "perimeter") || strings.Contains(zone, "outside") || strings.Contains(zone, "exterior") {
		tokens = append(tokens, "exterior", "perimeter")
	} else {
		tokens = append(tokens, "transition")
	}
	identity := "unclassified"
	if event.ResidentID != "" {
		identity = "resident"
	} else if event.Confidence > 0 {
		identity = "unknown"
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = at
	}
	return EncoderV4Observation{OccurredAt: event.Timestamp.UTC(), TopologyTokens: tokens, Kind: observationKind, Phase: phase, SubjectClass: subjectClass(observationKind), IdentityState: identity}
}

func subjectClass(kind string) string {
	switch {
	case strings.HasPrefix(kind, "presence.person"):
		return "person"
	case strings.HasPrefix(kind, "presence.vehicle"):
		return "vehicle"
	case strings.HasPrefix(kind, "presence.animal"):
		return "animal"
	case strings.HasPrefix(kind, "safety."):
		return "hazard"
	case strings.HasPrefix(kind, "system.") || strings.HasPrefix(kind, "environment."):
		return "system"
	default:
		return "access"
	}
}

// EncodeV4Frame mirrors state-encoder-go/stateencoder/encoder.go. Keep this
// function deliberately boring: any policy belongs outside this encoder.
func EncodeV4Frame(frame EncoderV4Frame) []float32 {
	features := make([]float32, 0, EncoderV4Size)
	features = append(features, v4SecurityFeatures(frame.SecurityMode)...)
	hour := float64(frame.At.Hour()) + float64(frame.At.Minute())/60
	angle := 2 * math.Pi * hour / 24
	features = append(features, float32(math.Sin(angle)), float32(math.Cos(angle)), v4Bool(hour < 6 || hour >= 22))
	features = append(features, v4Bool(frame.KnownResidentsPresent), v4Clamp01(float32(frame.KnownResidentCount)/8))
	features = append(features, v4ExactTokens(frame.ContextFacts, []string{
		"time.night", "time.daylight", "presence.routine_confirmed", "identity.known_confirmed", "identity.authorized_confirmed", "presence.animal_confirmed", "delivery.confirmed", "visit.scheduled", "environment.weather_confirmed", "access.outside_only", "access.no_entry_proof", "access.entry_confirmed", "presence.interior_signal", "evidence.verification_missing", "access.forced", "safety.life_safety", "safety.immediate", "observation.resolved",
	})...)
	features = append(features, v4TemporalFeatures(frame.Temporal)...)
	features = append(features, v4CapabilityFeatures(frame.AvailableCapabilities, frame.AvailableActionIDs)...)
	for i := 0; i < 8; i++ {
		if i < len(frame.Observations) {
			features = append(features, v4ObservationFeatures(frame.At, frame.Observations[i])...)
		} else {
			features = append(features, make([]float32, 55)...)
		}
	}
	return features
}

func v4TemporalFeatures(value EncoderV4TemporalContext) []float32 {
	return []float32{v4Clamp01(value.QuietSeconds / 300), v4Clamp01(value.SecondsSinceLastEvent / 300), v4Clamp01(value.SequenceDurationSeconds / 300), v4Clamp01(float32(value.EventCount) / 8), v4Bool(value.EvaluationTrigger == "quiet_tick"), v4Bool(value.EvaluationTrigger == "event")}
}

func v4CapabilityFeatures(capabilities, actions []string) []float32 {
	return []float32{v4Clamp01(float32(len(capabilities)) / 16), v4Clamp01(float32(len(actions)) / 16), v4Bool(v4HasExact(actions, "notify.emergency")), v4Bool(v4HasExact(actions, "security.arm_response"))}
}

func v4ObservationFeatures(at time.Time, observation EncoderV4Observation) []float32 {
	values := make([]float32, 55)
	offset := 0
	v4SetExact(values[offset:offset+24], observation.Kind, []string{"access.door", "access.window", "access.gate", "access.lock", "access.glass", "access.contact", "presence.motion", "presence.person", "presence.vehicle", "presence.animal", "presence.delivery", "safety.smoke", "safety.co", "safety.fire", "safety.gas", "safety.panic", "safety.fall", "security.tamper", "security.weapon", "system.camera", "system.sensor", "environment.weather", "environment.power", "device.appliance"})
	offset += 24
	v4SetExact(values[offset:offset+13], observation.Phase, []string{"detected", "opened", "closed", "active", "updated", "resolved", "cleared", "triggered", "restored", "offline", "transient", "secure", "fault"})
	offset += 13
	v4SetExact(values[offset:offset+6], observation.SubjectClass, []string{"person", "vehicle", "animal", "access", "hazard", "system"})
	offset += 6
	v4SetExact(values[offset:offset+4], observation.IdentityState, []string{"resident", "authorized", "unknown", "unclassified"})
	offset += 4
	v4SetExact(values[offset:offset+3], v4FirstTopology(observation.TopologyTokens), []string{"interior", "exterior", "transition"})
	offset += 3
	v4SetExact(values[offset:offset+3], v4TopologyScope(observation.TopologyTokens), []string{"protected", "perimeter", "outbuilding"})
	offset += 3
	age := float32(at.Sub(observation.OccurredAt).Seconds())
	values[offset] = v4Clamp01(age / 300)
	values[offset+1] = v4Bool(observation.Phase == "opened" || observation.Phase == "detected" || observation.Phase == "triggered" || observation.Phase == "active")
	return values
}

func v4ExactTokens(tokens, vocabulary []string) []float32 {
	values := make([]float32, len(vocabulary))
	for i, token := range vocabulary {
		values[i] = v4Bool(v4HasExact(tokens, token))
	}
	return values
}
func v4SetExact(values []float32, token string, vocabulary []string) {
	for i, candidate := range vocabulary {
		if token == candidate {
			values[i] = 1
			return
		}
	}
}
func v4HasExact(tokens []string, wanted string) bool {
	for _, token := range tokens {
		if token == wanted {
			return true
		}
	}
	return false
}
func v4FirstTopology(tokens []string) string {
	for _, token := range []string{"interior", "exterior", "transition"} {
		if v4HasExact(tokens, token) {
			return token
		}
	}
	return ""
}
func v4TopologyScope(tokens []string) string {
	for _, token := range []string{"protected", "perimeter", "outbuilding"} {
		if v4HasExact(tokens, token) {
			return token
		}
	}
	return ""
}
func v4SecurityFeatures(mode string) []float32 {
	values := make([]float32, 4)
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "disarmed":
		values[0] = 1
	case "armed_home":
		values[1] = 1
	case "armed_away":
		values[2] = 1
	default:
		values[3] = 1
	}
	return values
}
func v4Clamp01(value float32) float32 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
func v4Bool(value bool) float32 {
	if value {
		return 1
	}
	return 0
}

// V4FrameJSON is used by the export/parity harness and keeps the exact frame
// contract inspectable without introducing a Python dependency into Core.
func V4FrameJSON(frame EncoderV4Frame) ([]byte, error) { return json.Marshal(frame) }
