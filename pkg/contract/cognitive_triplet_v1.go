package contract

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"time"
)

const (
	EventFrameSchemaV1       = "synora.cognitive.event-frame/v1"
	ContextFrameSchemaV1     = "synora.cognitive.context-frame/v1"
	ArbitrationFrameSchemaV1 = "synora.cognitive.arbitration-frame/v1"
	TripletBundleSchemaV1    = "synora.cognitive.triplet-bundle/v1"
	TripletCompatibilityV1   = "synora.cognitive.triplet-compatibility/v1"
)

//go:embed cognitive-event-frame-v1.schema.json cognitive-context-frame-v1.schema.json cognitive-arbitration-frame-v1.schema.json cognitive-triplet-label-taxonomy-v1.json
var tripletContractAssets embed.FS

var tripletHashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func TripletContractHashes() map[string]string {
	return map[string]string{
		"event_frame_schema_hash":       hashAsset("cognitive-event-frame-v1.schema.json"),
		"context_frame_schema_hash":     hashAsset("cognitive-context-frame-v1.schema.json"),
		"arbitration_frame_schema_hash": hashAsset("cognitive-arbitration-frame-v1.schema.json"),
		"label_taxonomy_hash":           hashAsset("cognitive-triplet-label-taxonomy-v1.json"),
	}
}

func hashAsset(name string) string {
	body, err := tripletContractAssets.ReadFile(name)
	if err != nil {
		panic("embedded cognitive triplet contract asset is missing")
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type CognitiveEnvelopeV1 struct {
	Provenance   string    `json:"provenance"`
	Availability string    `json:"availability"`
	Quality      float64   `json:"quality"`
	LogicalTime  time.Time `json:"logical_timestamp"`
}

type CognitiveSecurityV1 struct {
	Armed       bool     `json:"armed"`
	Degraded    bool     `json:"degraded"`
	AlarmState  string   `json:"alarm_state"`
	Constraints []string `json:"constraints,omitempty"`
}

type CognitivePresenceV1 struct {
	Human   string `json:"human"`
	Animal  string `json:"animal"`
	Vehicle string `json:"vehicle"`
	Unknown bool   `json:"unknown"`
}

type CognitiveTopologyV1 struct {
	ZoneType    string `json:"zone_type"`
	Environment string `json:"environment"`
	Boundary    string `json:"boundary"`
	Restriction string `json:"restriction"`
	Relation    string `json:"relation"`
}

type CognitiveEvidenceV1 struct {
	VisionAvailability string `json:"vision_availability"`
	PoseAvailability   string `json:"pose_availability"`
	FaceAvailability   string `json:"face_availability"`
	SensorState        string `json:"sensor_state"`
	AccessState        string `json:"access_state"`
	MotionState        string `json:"motion_state"`
	CameraHealth       string `json:"camera_health"`
	EnrichmentState    string `json:"enrichment_state"`
}

type CognitiveCoEvidenceV1 struct {
	ConcurrentFacts    int    `json:"concurrent_facts"`
	IndependentSources int    `json:"independent_sources"`
	Contradictions     int    `json:"contradictions"`
	Sufficiency        string `json:"sufficiency"`
}

type EventFrameV1 struct {
	SchemaVersion string                `json:"schema_version"`
	Envelope      CognitiveEnvelopeV1   `json:"envelope"`
	Security      CognitiveSecurityV1   `json:"security"`
	Presence      CognitivePresenceV1   `json:"presence"`
	Topology      CognitiveTopologyV1   `json:"topology"`
	Evidence      CognitiveEvidenceV1   `json:"evidence"`
	CoEvidence    CognitiveCoEvidenceV1 `json:"co_evidence"`
}

type ContextEpisodeV1 struct {
	Phase        string  `json:"phase"`
	AgeSeconds   float64 `json:"age_seconds"`
	SegmentCount int     `json:"segment_count"`
	FactCount    int     `json:"fact_count"`
	CalmSeconds  float64 `json:"calm_seconds"`
	GapCount     int     `json:"gap_count"`
	Expired      bool    `json:"expired"`
	Resumed      bool    `json:"resumed"`
}

type ContextTemporalV1 struct {
	Recurrence      string   `json:"recurrence"`
	Persistence     string   `json:"persistence"`
	Intensification string   `json:"intensification"`
	ShortWindow     string   `json:"short_window"`
	MediumWindow    string   `json:"medium_window"`
	LongWindow      string   `json:"long_window"`
	Stability       string   `json:"stability"`
	Sequence        []string `json:"sequence"`
}

type ContextTopologyV1 struct {
	VisitedZones         []string `json:"visited_zones"`
	Transition           string   `json:"transition"`
	Direction            string   `json:"direction"`
	Coverage             string   `json:"coverage"`
	CorrelationAmbiguous bool     `json:"correlation_ambiguous"`
}

type ContextActionV1 struct {
	LastAction   string `json:"last_action"`
	LastResult   string `json:"last_result"`
	Cooldown     bool   `json:"cooldown"`
	FailureCount int    `json:"failure_count"`
	Idempotent   bool   `json:"idempotent"`
	Pending      bool   `json:"pending"`
}

type ContextAvailabilityV1 struct {
	CameraCoverage     string   `json:"camera_coverage"`
	UnavailableModules []string `json:"unavailable_modules"`
	DegradedSources    int      `json:"degraded_sources"`
	Quality            float64  `json:"quality"`
}

type ContextFrameV1 struct {
	SchemaVersion string                `json:"schema_version"`
	Provenance    string                `json:"provenance"`
	LogicalTime   time.Time             `json:"logical_timestamp"`
	Episode       ContextEpisodeV1      `json:"episode"`
	Temporal      ContextTemporalV1     `json:"temporal"`
	Topology      ContextTopologyV1     `json:"topology"`
	Actions       ContextActionV1       `json:"actions"`
	Availability  ContextAvailabilityV1 `json:"availability"`
}

type EventBranchV1 struct {
	Embedding    []float32 `json:"event_embedding,omitempty"`
	Impact       string    `json:"local_impact"`
	Quality      string    `json:"quality"`
	Confidence   float64   `json:"confidence"`
	Availability string    `json:"availability"`
}

type ContextBranchV1 struct {
	Embedding       []float32 `json:"context_embedding,omitempty"`
	TemporalPattern string    `json:"temporal_pattern"`
	TopologyPattern string    `json:"topology_pattern"`
	Quality         string    `json:"quality"`
	Confidence      float64   `json:"confidence"`
	Availability    string    `json:"availability"`
}

type ArbitrationSystemV1 struct {
	Armed              bool     `json:"armed"`
	Degraded           bool     `json:"degraded"`
	SafetyLevel        string   `json:"safety_level"`
	DeviceCapabilities []string `json:"device_capabilities"`
	ActionPossible     bool     `json:"action_possible"`
	Cooldown           bool     `json:"cooldown"`
	ResourceBudget     string   `json:"resource_budget"`
	APIState           string   `json:"api_state"`
	StoreState         string   `json:"store_state"`
}

type PreviousDecisionV1 struct {
	Danger       string `json:"danger"`
	Transition   string `json:"transition"`
	ActionResult string `json:"action_result"`
}

type ArbitrationFrameV1 struct {
	SchemaVersion    string              `json:"schema_version"`
	LogicalTime      time.Time           `json:"logical_timestamp"`
	EventBranch      EventBranchV1       `json:"event_branch"`
	ContextBranch    ContextBranchV1     `json:"context_branch"`
	System           ArbitrationSystemV1 `json:"system"`
	PreviousDecision PreviousDecisionV1  `json:"previous_decision"`
}

type EventCorrelationOutputV1 struct {
	EventEmbedding       []float32 `json:"event_embedding,omitempty"`
	LocalImpact          string    `json:"local_impact"`
	EvidenceQuality      string    `json:"evidence_quality"`
	ConfidenceCalibrated float64   `json:"event_confidence_calibrated"`
	AvailabilityMask     string    `json:"availability_mask"`
}

type ContextCorrelationOutputV1 struct {
	ContextEmbedding     []float32 `json:"context_embedding,omitempty"`
	TemporalPattern      string    `json:"temporal_pattern"`
	TopologyPattern      string    `json:"topology_pattern"`
	ConfidenceCalibrated float64   `json:"context_confidence_calibrated"`
	AvailabilityMask     string    `json:"availability_mask"`
}

type DecisionArbiterOutputV1 struct {
	Danger               string  `json:"danger"`
	Incident             string  `json:"incident"`
	Task                 string  `json:"task"`
	Action               string  `json:"action"`
	CommunicationIntent  string  `json:"communication_intent"`
	ConfidenceCalibrated float64 `json:"decision_confidence_calibrated"`
	Availability         string  `json:"decision_availability"`
}

type TripletBundleV1 struct {
	SchemaVersion              string `json:"schema_version"`
	EventModelManifestHash     string `json:"event_model_manifest_hash"`
	ContextModelManifestHash   string `json:"context_model_manifest_hash"`
	ArbiterModelManifestHash   string `json:"arbiter_model_manifest_hash"`
	EventFrameSchemaHash       string `json:"event_frame_schema_hash"`
	ContextFrameSchemaHash     string `json:"context_frame_schema_hash"`
	ArbitrationFrameSchemaHash string `json:"arbitration_frame_schema_hash"`
	LabelTaxonomyHash          string `json:"label_taxonomy_hash"`
	CalibrationManifestHash    string `json:"calibration_manifest_hash"`
	CompatibilityVersion       string `json:"compatibility_version"`
}

func (f EventFrameV1) Validate() error {
	if f.SchemaVersion != EventFrameSchemaV1 || !validProvenance(f.Envelope.Provenance) || !oneOf(f.Envelope.Availability, "available", "degraded", "unavailable") || !tripletFiniteUnit(f.Envelope.Quality) || f.Envelope.LogicalTime.IsZero() || f.Envelope.LogicalTime.Location() != time.UTC {
		return errors.New("invalid EventFrame V1 envelope")
	}
	if !oneOf(f.Security.AlarmState, "clear", "triggered", "unknown") || len(f.Security.Constraints) > 16 {
		return errors.New("invalid EventFrame V1 security")
	}
	for _, value := range f.Security.Constraints {
		if !oneOf(value, "dry_run_only", "no_audio", "no_physical_action", "fail_closed") {
			return errors.New("invalid EventFrame V1 constraint")
		}
	}
	for _, value := range []string{f.Presence.Human, f.Presence.Animal, f.Presence.Vehicle} {
		if !oneOf(value, "unknown", "absent", "candidate", "confirmed") {
			return errors.New("invalid EventFrame V1 presence")
		}
		if (f.Evidence.VisionAvailability == "unavailable" || f.Evidence.VisionAvailability == "degraded" || f.Evidence.VisionAvailability == "not_requested") && value == "absent" {
			return errors.New("unavailable or degraded evidence cannot assert absence")
		}
	}
	if !oneOf(f.Topology.ZoneType, "public", "private", "restricted", "protected", "unknown") || !oneOf(f.Topology.Environment, "indoor", "outdoor", "unknown") || !oneOf(f.Topology.Boundary, "none", "threshold", "perimeter", "zone", "unknown") || !oneOf(f.Topology.Restriction, "open", "limited", "restricted", "unknown") || !oneOf(f.Topology.Relation, "inside", "entering", "leaving", "crossing", "unknown") {
		return errors.New("invalid EventFrame V1 topology")
	}
	if !oneOf(f.Evidence.VisionAvailability, "not_requested", "unavailable", "unknown", "available", "evaluated", "degraded") || !oneOf(f.Evidence.PoseAvailability, "not_requested", "unavailable", "unknown", "available", "evaluated", "degraded") || !oneOf(f.Evidence.FaceAvailability, "not_requested", "unavailable", "unknown", "available", "evaluated", "degraded") || !oneOf(f.Evidence.SensorState, "unavailable", "unknown", "available", "degraded") || !oneOf(f.Evidence.AccessState, "unknown", "open", "closed", "forced") || !oneOf(f.Evidence.MotionState, "unknown", "still", "moving", "candidate") || !oneOf(f.Evidence.CameraHealth, "unknown", "healthy", "degraded", "offline", "uncertain", "unavailable") || !oneOf(f.Evidence.EnrichmentState, "unknown", "complete", "partial", "unavailable", "rejected") {
		return errors.New("invalid EventFrame V1 evidence")
	}
	if !bounded(f.CoEvidence.ConcurrentFacts, 0, 64) || !bounded(f.CoEvidence.IndependentSources, 0, 16) || !bounded(f.CoEvidence.Contradictions, 0, 64) || !oneOf(f.CoEvidence.Sufficiency, "sufficient", "insufficient", "unknown", "degraded") {
		return errors.New("invalid EventFrame V1 co-evidence")
	}
	return nil
}

func (f ContextFrameV1) Validate() error {
	if f.SchemaVersion != ContextFrameSchemaV1 || !validProvenance(f.Provenance) || f.LogicalTime.IsZero() || f.LogicalTime.Location() != time.UTC {
		return errors.New("invalid ContextFrame V1 metadata")
	}
	if !oneOf(f.Episode.Phase, "initial", "candidate", "confirmed", "final", "unknown") || !tripletFiniteRange(f.Episode.AgeSeconds, 0, 86400*365) || !tripletFiniteRange(f.Episode.CalmSeconds, 0, 86400*365) || !bounded(f.Episode.SegmentCount, 0, 4096) || !bounded(f.Episode.FactCount, 0, 65536) || !bounded(f.Episode.GapCount, 0, 4096) {
		return errors.New("invalid ContextFrame V1 episode")
	}
	for _, value := range []string{f.Temporal.Recurrence, f.Temporal.Persistence, f.Temporal.Intensification, f.Temporal.ShortWindow, f.Temporal.MediumWindow, f.Temporal.LongWindow, f.Temporal.Stability} {
		if !oneOf(value, "none", "isolated", "repeated", "persistent", "calm", "escalating", "unknown", "unavailable", "stable", "unstable", "insufficient") {
			return errors.New("invalid ContextFrame V1 temporal state")
		}
	}
	if len(f.Temporal.Sequence) > 32 || len(f.Topology.VisitedZones) > 32 || len(f.Availability.UnavailableModules) > 32 {
		return errors.New("ContextFrame V1 list limit exceeded")
	}
	for _, value := range f.Temporal.Sequence {
		if !oneOf(value, "presence", "motion", "access", "alarm", "pose", "camera_health", "gap", "action", "unknown") {
			return errors.New("invalid ContextFrame V1 sequence")
		}
	}
	for _, value := range f.Topology.VisitedZones {
		if !oneOf(value, "public", "private", "restricted", "protected", "unknown") {
			return errors.New("invalid ContextFrame V1 zone")
		}
	}
	if !oneOf(f.Topology.Transition, "expected", "anomalous", "unknown") || !oneOf(f.Topology.Direction, "entering", "leaving", "within", "unknown") || !oneOf(f.Topology.Coverage, "complete", "partial", "unknown") {
		return errors.New("invalid ContextFrame V1 topology")
	}
	if !oneOf(f.Actions.LastAction, "none", "notify", "record", "announce", "unknown") || !oneOf(f.Actions.LastResult, "none", "success", "failed", "blocked", "suppressed", "unknown") || !bounded(f.Actions.FailureCount, 0, 4096) {
		return errors.New("invalid ContextFrame V1 action state")
	}
	if !oneOf(f.Availability.CameraCoverage, "complete", "partial", "unavailable", "unknown") || !bounded(f.Availability.DegradedSources, 0, 64) || !tripletFiniteUnit(f.Availability.Quality) {
		return errors.New("invalid ContextFrame V1 availability")
	}
	for _, module := range f.Availability.UnavailableModules {
		if !oneOf(module, "vision_person", "vision_pose", "vision_face", "vision_vehicle", "vision_plate", "vision_animal", "camera_health", "sensors", "store", "api") {
			return errors.New("invalid ContextFrame V1 module")
		}
	}
	return nil
}

func (f ArbitrationFrameV1) Validate() error {
	if f.SchemaVersion != ArbitrationFrameSchemaV1 || f.LogicalTime.IsZero() || f.LogicalTime.Location() != time.UTC {
		return errors.New("invalid ArbitrationFrame V1 metadata")
	}
	if !oneOf(f.EventBranch.Impact, "neutral", "attention", "threat", "uncertain") || !oneOf(f.EventBranch.Quality, "sufficient", "degraded", "unavailable") || !tripletFiniteUnit(f.EventBranch.Confidence) || !oneOf(f.EventBranch.Availability, "available", "unavailable", "degraded") || !validEmbedding(f.EventBranch.Embedding, f.EventBranch.Availability) {
		return errors.New("invalid ArbitrationFrame V1 event branch")
	}
	if !oneOf(f.ContextBranch.TemporalPattern, "calm", "persistent", "escalating", "unresolved", "unknown", "unavailable") || !oneOf(f.ContextBranch.TopologyPattern, "expected", "anomalous", "unknown", "unavailable") || !oneOf(f.ContextBranch.Quality, "sufficient", "degraded", "unavailable") || !tripletFiniteUnit(f.ContextBranch.Confidence) || !oneOf(f.ContextBranch.Availability, "available", "unavailable", "degraded") || !validEmbedding(f.ContextBranch.Embedding, f.ContextBranch.Availability) {
		return errors.New("invalid ArbitrationFrame V1 context branch")
	}
	if !oneOf(f.System.SafetyLevel, "normal", "elevated", "restricted", "unknown") || len(f.System.DeviceCapabilities) > 32 || !oneOf(f.System.ResourceBudget, "normal", "constrained", "exhausted", "unknown") || !oneOf(f.System.APIState, "available", "degraded", "unavailable") || !oneOf(f.System.StoreState, "available", "degraded", "unavailable") {
		return errors.New("invalid ArbitrationFrame V1 system")
	}
	for _, value := range f.System.DeviceCapabilities {
		if !oneOf(value, "notify", "record", "announce") {
			return errors.New("invalid ArbitrationFrame V1 capability")
		}
	}
	if !oneOf(f.PreviousDecision.Danger, "none", "low", "elevated", "critical", "unknown") || !oneOf(f.PreviousDecision.Transition, "none", "raised", "lowered", "stable", "unknown") || !oneOf(f.PreviousDecision.ActionResult, "none", "success", "failed", "blocked", "suppressed", "unknown") {
		return errors.New("invalid ArbitrationFrame V1 previous decision")
	}
	return nil
}

func (b TripletBundleV1) Validate(expected map[string]string) error {
	if b.SchemaVersion != TripletBundleSchemaV1 || b.CompatibilityVersion != TripletCompatibilityV1 {
		return errors.New("triplet bundle version mismatch")
	}
	values := map[string]string{
		"event_model_manifest_hash": b.EventModelManifestHash, "context_model_manifest_hash": b.ContextModelManifestHash,
		"arbiter_model_manifest_hash": b.ArbiterModelManifestHash, "event_frame_schema_hash": b.EventFrameSchemaHash,
		"context_frame_schema_hash": b.ContextFrameSchemaHash, "arbitration_frame_schema_hash": b.ArbitrationFrameSchemaHash,
		"label_taxonomy_hash": b.LabelTaxonomyHash, "calibration_manifest_hash": b.CalibrationManifestHash,
	}
	compiledContracts := TripletContractHashes()
	for key, value := range values {
		if !tripletHashPattern.MatchString(value) {
			return errors.New("triplet bundle hash missing or malformed")
		}
		if current, versionedContract := compiledContracts[key]; versionedContract && !equalHash(value, current) {
			return errors.New("triplet bundle does not match the compiled contract/taxonomy")
		}
		if schema, ok := expected[key]; ok && !equalHash(value, schema) {
			return errors.New("triplet bundle schema/taxonomy mismatch")
		}
	}
	return nil
}

func (o EventCorrelationOutputV1) Validate() error {
	if !oneOf(o.LocalImpact, "neutral", "attention", "threat", "uncertain") || !oneOf(o.EvidenceQuality, "sufficient", "degraded", "unavailable") || !tripletFiniteUnit(o.ConfidenceCalibrated) || !oneOf(o.AvailabilityMask, "available", "degraded", "unavailable") {
		return errors.New("invalid EventCorrelationMLP output V1")
	}
	if o.AvailabilityMask == "available" && (len(o.EventEmbedding) == 0 || len(o.EventEmbedding) > 4096) || o.AvailabilityMask != "available" && len(o.EventEmbedding) != 0 {
		return errors.New("invalid EventCorrelationMLP embedding mask")
	}
	for _, value := range o.EventEmbedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < -1 || value > 1 {
			return errors.New("invalid EventCorrelationMLP embedding value")
		}
	}
	return nil
}

func (o ContextCorrelationOutputV1) Validate() error {
	if !oneOf(o.TemporalPattern, "calm", "persistent", "escalating", "unresolved", "unknown", "unavailable") || !oneOf(o.TopologyPattern, "expected", "anomalous", "unknown", "unavailable") || !tripletFiniteUnit(o.ConfidenceCalibrated) || !oneOf(o.AvailabilityMask, "available", "degraded", "unavailable") {
		return errors.New("invalid ContextCorrelationMLP output V1")
	}
	if o.AvailabilityMask == "available" && (len(o.ContextEmbedding) == 0 || len(o.ContextEmbedding) > 4096) || o.AvailabilityMask != "available" && len(o.ContextEmbedding) != 0 {
		return errors.New("invalid ContextCorrelationMLP embedding mask")
	}
	for _, value := range o.ContextEmbedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < -1 || value > 1 {
			return errors.New("invalid ContextCorrelationMLP embedding value")
		}
	}
	return nil
}

func (o DecisionArbiterOutputV1) Validate() error {
	if !oneOf(o.Danger, "none", "low", "elevated", "critical", "unknown") || !oneOf(o.Incident, "none", "routine_presence", "safety_event", "system_event", "unknown") || !oneOf(o.Task, "none", "monitor", "notify", "review", "unknown") || !oneOf(o.Action, "no_action", "notify", "record", "announce", "unknown") || !oneOf(o.CommunicationIntent, "none", "neutral_presence_notice", "wellness_check", "safety_instruction", "unknown") || !tripletFiniteUnit(o.ConfidenceCalibrated) || !oneOf(o.Availability, "available", "degraded", "unavailable", "blocked") {
		return errors.New("invalid DecisionArbiterMLP output V1")
	}
	return nil
}

func DecodeEventFrameV1(data []byte) (EventFrameV1, error) {
	var value EventFrameV1
	err := decodeStrictTriplet(data, &value, func() error { return value.Validate() })
	return value, err
}
func DecodeContextFrameV1(data []byte) (ContextFrameV1, error) {
	var value ContextFrameV1
	err := decodeStrictTriplet(data, &value, func() error { return value.Validate() })
	return value, err
}
func DecodeArbitrationFrameV1(data []byte) (ArbitrationFrameV1, error) {
	var value ArbitrationFrameV1
	err := decodeStrictTriplet(data, &value, func() error { return value.Validate() })
	return value, err
}
func DecodeTripletBundleV1(data []byte, expected map[string]string) (TripletBundleV1, error) {
	var value TripletBundleV1
	err := decodeStrictTriplet(data, &value, func() error { return value.Validate(expected) })
	return value, err
}
func decodeStrictTriplet(data []byte, out any, validate func() error) error {
	if len(data) == 0 || len(data) > 1<<20 {
		return errors.New("cognitive frame size invalid")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("cognitive frame must contain one JSON value")
	}
	return validate()
}

func validEmbedding(values []float32, availability string) bool {
	if availability == "available" && (len(values) == 0 || len(values) > 4096) {
		return false
	}
	if availability != "available" && len(values) != 0 {
		return false
	}
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value < -1 || value > 1 {
			return false
		}
	}
	return true
}
func validProvenance(value string) bool { return oneOf(value, "real", "replay", "simulated_test") }
func oneOf(value string, values ...string) bool {
	for _, item := range values {
		if value == item {
			return true
		}
	}
	return false
}
func bounded(value, low, high int) bool    { return value >= low && value <= high }
func tripletFiniteUnit(value float64) bool { return tripletFiniteRange(value, 0, 1) }
func tripletFiniteRange(value, low, high float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}
func equalHash(a, b string) bool { return bytes.Equal([]byte(a), []byte(b)) }
