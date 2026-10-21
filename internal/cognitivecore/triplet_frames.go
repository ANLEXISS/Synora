package cognitivecore

import (
	"errors"
	"sort"

	"synora/pkg/contract"
)

// TripletFramesV1 is a deterministic, read-only projection for fixtures and
// future corpus work. It is not consumed by Core's active V1/V3 decision path.
type TripletFramesV1 struct {
	Event       contract.EventFrameV1       `json:"event_frame"`
	Context     contract.ContextFrameV1     `json:"context_frame"`
	Arbitration contract.ArbitrationFrameV1 `json:"arbitration_frame"`
}

// BuildEventFrameV1 projects only aggregate snapshot and store facts. Missing
// evidence remains unknown/unavailable rather than becoming negative evidence.
func BuildEventFrameV1(snapshot CognitiveSnapshotV3) (contract.EventFrameV1, error) {
	snapshot = snapshot.Normalized()
	if err := snapshot.Validate(); err != nil {
		return contract.EventFrameV1{}, err
	}
	at := snapshot.CapturedAt.UTC()
	provenance := "simulated_test"
	if snapshot.SimulatedCamera {
		provenance = "simulated_test"
	} else if snapshot.Vision.ReplaySimulation {
		provenance = "replay"
	} else if snapshot.Vision.RealDetection {
		provenance = "real"
	}
	availability, pose := "unavailable", snapshot.Vision.PoseStatus
	if pose == PoseV3NotRequested {
		availability, pose = "not_requested", "not_requested"
	}
	if pose == PoseV3Available {
		availability, pose = "available", "evaluated"
	}
	if pose == PoseV3LowQuality {
		availability, pose = "degraded", "evaluated"
	}
	quality := float64(snapshot.Vision.PoseQuality)
	if availability == "unavailable" || availability == "not_requested" {
		quality = 0
	}
	human := "unknown"
	if snapshot.BaseV2.Presence.HumanPresent || snapshot.BaseV2.Vision.HumanConfirmed {
		human = "confirmed"
	}
	animal, vehicle := "unknown", "unknown"
	if e := snapshot.VisionEvidence; e != nil {
		if err := e.Validate(); err != nil {
			return contract.EventFrameV1{}, err
		}
		if e.Provenance == "simulated_test" {
			provenance = "simulated_test"
		} else if e.Provenance == "replay" {
			provenance = "replay"
		} else if e.Provenance == "real" {
			provenance = "real"
		}
		availability = string(e.Presence.Human.Availability)
		if e.Presence.Human.State == "present" {
			human = "confirmed"
		} else if e.Presence.Human.State == "candidate" {
			human = "candidate"
		}
		if e.Presence.Animal.State == "present" {
			animal = "confirmed"
		} else if e.Presence.Animal.State == "candidate" {
			animal = "candidate"
		}
		if e.Presence.Vehicle.State == "present" {
			vehicle = "confirmed"
		} else if e.Presence.Vehicle.State == "candidate" {
			vehicle = "candidate"
		}
	}
	zone, environment, boundary, restriction := topologyProjection(snapshot.BaseV2.Topology)
	sensorState := "unavailable"
	if snapshot.BaseV2.Sensors.SensorEvidence {
		sensorState = "available"
	}
	access := snapshot.BaseV2.Sensors.AccessState
	if access != "open" && access != "closed" && access != "forced" {
		access = "unknown"
	}
	motion := "still"
	if snapshot.BaseV2.Sensors.Movement {
		motion = "moving"
	}
	visionState := availability
	faceState := "not_requested"
	if snapshot.VisionEvidence != nil {
		faceState = string(snapshot.VisionEvidence.Face.Availability)
	}
	cameraHealth := snapshot.Vision.CameraHealthStatus
	if cameraHealth == "" {
		cameraHealth = "unknown"
	}
	enrichment := "unavailable"
	if snapshot.VisionEvidence != nil {
		enrichment = string(snapshot.VisionEvidence.Processing)
	}
	sufficiency := "insufficient"
	if availability == "available" && quality > 0 {
		sufficiency = "sufficient"
	} else if availability == "degraded" {
		sufficiency = "degraded"
	} else if availability == "not_requested" {
		sufficiency = "unknown"
	}
	independent := 0
	if availability == "available" || availability == "degraded" {
		independent++
	}
	if sensorState == "available" {
		independent++
	}
	frame := contract.EventFrameV1{
		SchemaVersion: contract.EventFrameSchemaV1,
		Envelope:      contract.CognitiveEnvelopeV1{Provenance: provenance, Availability: availabilityForFrame(availability), Quality: quality, LogicalTime: at},
		Security:      contract.CognitiveSecurityV1{Armed: snapshot.BaseV2.Security.Armed, Degraded: snapshot.BaseV2.Security.Degraded, AlarmState: alarmState(snapshot.BaseV2.Sensors.AlarmState), Constraints: []string{"dry_run_only", "no_audio", "no_physical_action", "fail_closed"}},
		Presence:      contract.CognitivePresenceV1{Human: human, Animal: animal, Vehicle: vehicle, Unknown: human == "unknown" && animal == "unknown" && vehicle == "unknown"},
		Topology:      contract.CognitiveTopologyV1{ZoneType: zone, Environment: environment, Boundary: boundary, Restriction: restriction, Relation: "unknown"},
		Evidence:      contract.CognitiveEvidenceV1{VisionAvailability: visionState, PoseAvailability: pose, FaceAvailability: faceState, SensorState: sensorState, AccessState: access, MotionState: motion, CameraHealth: cameraHealth, EnrichmentState: enrichment},
		CoEvidence:    contract.CognitiveCoEvidenceV1{ConcurrentFacts: minInt(64, snapshot.BaseV2.Presence.TrackCount+snapshot.BaseV2.Sensors.ObservationCount), IndependentSources: independent, Sufficiency: sufficiency},
	}
	if err := frame.Validate(); err != nil {
		return contract.EventFrameV1{}, err
	}
	return frame, nil
}

// BuildContextFrameV1 derives temporal context from the persisted Store ledger;
// it keeps no implicit recurrent state inside a future neural model.
func BuildContextFrameV1(snapshot CognitiveSnapshot, history []Commit, visionSnapshots []CognitiveSnapshotV3, pendingActions ...bool) (contract.ContextFrameV1, error) {
	snapshot = snapshot.Normalized()
	if err := snapshot.Validate(); err != nil {
		return contract.ContextFrameV1{}, err
	}
	at := snapshot.CapturedAt.UTC()
	provenance := "simulated_test"
	for _, commit := range history {
		if commit.SnapshotV3 != nil {
			if commit.SnapshotV3.SimulatedCamera {
				provenance = "simulated_test"
			} else if commit.SnapshotV3.Vision.ReplaySimulation {
				provenance = "replay"
			} else if commit.SnapshotV3.Vision.RealDetection {
				provenance = "real"
			}
		}
	}
	segments := snapshot.Episode.SegmentCount
	recurrence, persistence := "isolated", "none"
	if segments > 1 {
		recurrence, persistence = "repeated", "repeated"
	}
	if segments > 3 {
		persistence = "persistent"
	}
	intensification := "unknown"
	if snapshot.Episode.Phase == VisionPhaseFinal {
		intensification = "calm"
	} else if snapshot.Episode.Phase == VisionPhaseConfirmed {
		intensification = "escalating"
	} else if snapshot.Episode.Phase == VisionPhaseCandidate {
		intensification = "persistent"
	}
	sequence := make([]string, 0, minInt(32, len(history)))
	zones := map[string]bool{}
	for _, commit := range history {
		if len(sequence) < 32 {
			sequence = append(sequence, sequenceKind(commit.Event.Type))
		}
		if value := zoneName(commit.Snapshot.Topology); value != "unknown" {
			zones[value] = true
		}
	}
	visited := make([]string, 0, len(zones))
	for zone := range zones {
		visited = append(visited, zone)
	}
	sort.Strings(visited)
	if len(sequence) == 0 {
		sequence = []string{"unknown"}
	}
	lastAction, lastResult := "none", "none"
	failures := 0
	pending := len(pendingActions) > 0 && pendingActions[0]
	if n := len(snapshot.ActionResults); n > 0 {
		last := snapshot.ActionResults[n-1]
		lastAction = "unknown"
		switch {
		case last.Successful:
			lastResult = "success"
		case last.Failed:
			lastResult = "failed"
			failures = 1
		case last.Unavailable:
			lastResult = "blocked"
		default:
			lastResult = "unknown"
		}
	}
	quality := float64(snapshot.Sensors.Confidence)
	coverage := "unknown"
	unavailableModules := []string{}
	cooldown := false
	if len(visionSnapshots) > 0 {
		v := visionSnapshots[len(visionSnapshots)-1]
		cooldown = v.BaseV2.Communication.CooldownActive
		switch v.Vision.CameraHealthStatus {
		case CameraHealthV3Healthy:
			coverage = "complete"
		case CameraHealthV3Degraded, CameraHealthV3Uncertain:
			coverage = "partial"
		case CameraHealthV3Offline:
			coverage = "unavailable"
			unavailableModules = append(unavailableModules, "camera_health")
		}
		if v.Vision.PoseStatus == PoseV3Unavailable || v.Vision.PoseStatus == PoseV3NotRequested {
			unavailableModules = append(unavailableModules, "vision_pose")
		}
		quality = maxUnit(quality, float64(v.Vision.PoseQuality))
	}
	if snapshot.Sensors.SensorEvidence {
		if coverage == "unknown" || coverage == "unavailable" {
			coverage = "partial"
		}
		quality = maxUnit(quality, 0.25)
	}
	frame := contract.ContextFrameV1{
		SchemaVersion: contract.ContextFrameSchemaV1, Provenance: provenance, LogicalTime: at,
		Episode:      contract.ContextEpisodeV1{Phase: phaseName(snapshot.Episode.Phase), AgeSeconds: float64(maxFloat32(snapshot.Episode.SecondsSinceFirst, 0)), SegmentCount: segments, FactCount: len(history), CalmSeconds: float64(maxFloat32(snapshot.Episode.CalmSeconds, 0)), GapCount: snapshot.Episode.GapCount, Expired: snapshot.Episode.Phase == VisionPhaseFinal, Resumed: false},
		Temporal:     contract.ContextTemporalV1{Recurrence: recurrence, Persistence: persistence, Intensification: intensification, ShortWindow: windowState(snapshot.Episode.SecondsSinceLast, 30), MediumWindow: windowState(snapshot.Episode.SecondsSinceLast, 300), LongWindow: windowState(snapshot.Episode.SecondsSinceLast, 3600), Stability: "unknown", Sequence: sequence},
		Topology:     contract.ContextTopologyV1{VisitedZones: visited, Transition: "unknown", Direction: "unknown", Coverage: "unknown", CorrelationAmbiguous: snapshot.Topology == contract.VisionTopologyUnknown},
		Actions:      contract.ContextActionV1{LastAction: lastAction, LastResult: lastResult, Cooldown: cooldown, FailureCount: failures, Idempotent: true, Pending: pending},
		Availability: contract.ContextAvailabilityV1{CameraCoverage: coverage, UnavailableModules: unavailableModules, DegradedSources: 0, Quality: quality},
	}
	if err := frame.Validate(); err != nil {
		return contract.ContextFrameV1{}, err
	}
	return frame, nil
}

// BuildArbitrationFrameV1 creates the arbiter's only input. This foundation
// deliberately emits explicit unavailable branch masks; it does not run any
// of the future MLPs or borrow V3's feature vector.
func BuildArbitrationFrameV1(event contract.EventFrameV1, context contract.ContextFrameV1, snapshot CognitiveSnapshot) (contract.ArbitrationFrameV1, error) {
	if err := event.Validate(); err != nil {
		return contract.ArbitrationFrameV1{}, err
	}
	if err := context.Validate(); err != nil {
		return contract.ArbitrationFrameV1{}, err
	}
	snapshot = snapshot.Normalized()
	at := event.Envelope.LogicalTime.UTC()
	capabilities := capabilityNames(snapshot.Capabilities)
	actionPossible := len(capabilities) > 0 && !snapshot.Security.Degraded && !context.Actions.Cooldown
	frame := contract.ArbitrationFrameV1{
		SchemaVersion: contract.ArbitrationFrameSchemaV1, LogicalTime: at,
		EventBranch:      contract.EventBranchV1{Impact: "uncertain", Quality: qualityBand(event.Envelope.Availability), Confidence: 0, Availability: "unavailable"},
		ContextBranch:    contract.ContextBranchV1{TemporalPattern: "unavailable", TopologyPattern: "unavailable", Quality: "unavailable", Confidence: 0, Availability: "unavailable"},
		System:           contract.ArbitrationSystemV1{Armed: snapshot.Security.Armed, Degraded: snapshot.Security.Degraded, SafetyLevel: safetyLevel(snapshot), DeviceCapabilities: capabilities, ActionPossible: actionPossible, Cooldown: context.Actions.Cooldown, ResourceBudget: "unknown", APIState: "available", StoreState: "available"},
		PreviousDecision: contract.PreviousDecisionV1{Danger: "unknown", Transition: "unknown", ActionResult: lastResultName(snapshot.ActionResults)},
	}
	if err := frame.Validate(); err != nil {
		return contract.ArbitrationFrameV1{}, err
	}
	return frame, nil
}

func BuildTripletFramesV1(store *UniversalStore) (TripletFramesV1, error) {
	if store == nil {
		return TripletFramesV1{}, errors.New("triplet builder requires the Universal Store")
	}
	v3, ok := store.SnapshotV3()
	if !ok {
		return TripletFramesV1{}, errors.New("triplet builder requires a persisted V3 snapshot")
	}
	history, err := store.History()
	if err != nil {
		return TripletFramesV1{}, err
	}
	base := store.Snapshot()
	event, err := BuildEventFrameV1(v3)
	if err != nil {
		return TripletFramesV1{}, err
	}
	context, err := BuildContextFrameV1(base, history, []CognitiveSnapshotV3{v3}, len(store.ActionOutbox()) > 0)
	if err != nil {
		return TripletFramesV1{}, err
	}
	arbitration, err := BuildArbitrationFrameV1(event, context, base)
	if err != nil {
		return TripletFramesV1{}, err
	}
	return TripletFramesV1{Event: event, Context: context, Arbitration: arbitration}, nil
}

func topologyProjection(value string) (string, string, string, string) {
	switch value {
	case contract.VisionTopologyPublicOutdoor:
		return "public", "outdoor", "none", "open"
	case contract.VisionTopologyPrivatePerimeter:
		return "private", "unknown", "perimeter", "limited"
	case contract.VisionTopologyRestrictedThreshold:
		return "restricted", "unknown", "threshold", "restricted"
	case contract.VisionTopologyProtectedInterior:
		return "protected", "indoor", "zone", "restricted"
	default:
		return "unknown", "unknown", "unknown", "unknown"
	}
}
func zoneName(value string) string { zone, _, _, _ := topologyProjection(value); return zone }
func availabilityForFrame(value string) string {
	if value == "available" {
		return "available"
	}
	if value == "degraded" {
		return "degraded"
	}
	return "unavailable"
}
func alarmState(value string) string {
	if value == "triggered" {
		return "triggered"
	}
	if value == "clear" || value == "normal" {
		return "clear"
	}
	return "unknown"
}
func sequenceKind(value string) string {
	switch value {
	case contract.EventVisionEvidenceV1, contract.EventVisionEnrichmentV3:
		return "presence"
	case contract.EventActionResult:
		return "action"
	case contract.EventSensorAnomaly:
		return "access"
	case contract.EventDeviceOffline:
		return "camera_health"
	default:
		return "unknown"
	}
}
func phaseName(value string) string {
	if value == "initial" || value == "candidate" || value == "confirmed" || value == "final" {
		return value
	}
	return "unknown"
}
func windowState(seconds float32, limit float32) string {
	if seconds < 0 {
		return "unknown"
	}
	if seconds <= limit {
		return "stable"
	}
	return "insufficient"
}
func qualityBand(availability string) string {
	if availability == "available" {
		return "sufficient"
	}
	if availability == "degraded" {
		return "degraded"
	}
	return "unavailable"
}
func safetyLevel(snapshot CognitiveSnapshot) string {
	if snapshot.Security.Degraded {
		return "restricted"
	}
	if !snapshot.Security.Known {
		return "unknown"
	}
	if snapshot.Security.Armed {
		return "elevated"
	}
	return "normal"
}
func capabilityNames(values []CapabilityFact) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value.Available && (value.Capability == "notify" || value.Capability == "record" || value.Capability == "announce") {
			result = append(result, value.Capability)
		}
	}
	sort.Strings(result)
	return uniqueStrings(result)
}
func lastResultName(values []ActionResultFact) string {
	if len(values) == 0 {
		return "none"
	}
	v := values[len(values)-1]
	if v.Successful {
		return "success"
	}
	if v.Failed {
		return "failed"
	}
	if v.Unavailable {
		return "blocked"
	}
	return "unknown"
}
func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func maxUnit(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
