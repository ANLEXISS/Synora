package cognitivecore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"synora/pkg/contract"
)

type ProcessResult struct {
	Commit  Commit
	Result  CommitResult
	Encoded EncodedSnapshot
}

type Core struct {
	Store   *UniversalStore
	Encoder SnapshotEncoder
	MLP     MLPBackend
	Gate    SafetyGate
	Now     func() time.Time
}

// ProcessTest runs the same snapshot encoder, MLP backend, safety gate and
// redaction path as production inference, but never commits business state or
// creates an action request. It is callable only for the explicit API test
// envelope after Service normalizes its catalogued event type.
func (c *Core) ProcessTest(ctx context.Context, event contract.Event) (ProcessResult, error) {
	if !isTestHarnessEvent(event) {
		return ProcessResult{}, errors.New("test-harness provenance is required")
	}
	if !c.Gate.DryRun {
		return ProcessResult{}, errors.New("test-harness requires active_dry_run")
	}
	if inference, ok := payloadBool(event.Payload, "test_inference"); ok && !inference {
		return c.processWithoutInference(event)
	}
	return c.process(ctx, event, false)
}

func (c *Core) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Core) Process(ctx context.Context, event contract.Event) (ProcessResult, error) {
	return c.process(ctx, event, true)
}

func (c *Core) process(ctx context.Context, event contract.Event, persist bool) (ProcessResult, error) {
	if c == nil || c.Store == nil {
		return ProcessResult{}, errors.New("cognitive core is not configured")
	}
	if event.ID == "" || event.Type == "" || event.Source == "" {
		return ProcessResult{}, errors.New("event id, type and source are required")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = c.now()
	}
	previous := c.Store.Snapshot()
	snapshot, err := c.composeSnapshot(previous, event)
	if err != nil {
		return ProcessResult{}, err
	}
	encoded, err := c.Encoder.Encode(ctx, snapshot)
	if err != nil {
		return ProcessResult{}, err
	}
	decision := Decision{SchemaVersion: DecisionSchemaVersion, Status: "unavailable", Mode: "active_dry_run", Source: "mlp", HeadOrder: append([]string(nil), HeadOrder[:]...), InputDimension: CognitiveVectorSize, Action: ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", PhysicalActionExecuted: false}, GeneratedAt: c.now()}
	if isTestHarnessEvent(event) {
		decision.Provenance = "test-harness"
		decision.Test = true
	}
	var output MLPOutput
	if c.MLP == nil {
		decision.Error = ErrModelUnavailable.Error()
	} else {
		var latencies map[string]float64
		output, latencies, err = c.MLP.Run(ctx, encoded, snapshot)
		decision.HeadLatencyMS = latencies
		if err == nil {
			if output.Trace != nil {
				output.Trace.InferenceID = event.ID
				output.Trace.Proposed = output.Action.Action
				if isTestHarnessEvent(event) {
					output.Trace.Provenance = "test-harness"
					output.Trace.Test = true
				}
				decision.Trace = output.Trace
			}
			mode := "active"
			if c.Gate.DryRun {
				mode = "active_dry_run"
			}
			decision.Status, decision.Mode, decision.DangerLabel, decision.DangerScore = "available", mode, output.DangerLabel, clamp01(output.DangerScore)
			decision.IncidentConfidence = clamp01(output.IncidentConfidence)
			decision.TaskConfidence = clamp01(output.TaskConfidence)
			decision.ActionConfidence = clamp01(output.ActionConfidence)
			decision.Incidents, decision.Task = append([]string(nil), output.Incidents...), output.Task
			decision.Action = c.Gate.Apply(output, snapshot, c.now())
			if event.Type == contract.EventActionResult || event.Type == "discovery.action.result" {
				// An action result is a fact to fold into the next snapshot, not
				// a fresh trigger. This prevents a real bus loop from repeatedly
				// re-emitting the same dry-run action request.
				decision.Action = ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", Reasons: []string{"action_result_observed"}, PhysicalActionExecuted: false}
			}
		} else {
			decision.Error = err.Error()
		}
	}
	var action *ActionRequest
	if persist && decision.Status == "available" && (decision.Action.Status == "allowed" || decision.Action.Status == "allowed_dry_run") && decision.Action.Proposed.Action != "no_action" {
		action = &ActionRequest{SchemaVersion: "action-request/v1", RequestID: event.ID, EpisodeID: episodeID(event), Action: decision.Action.Proposed, DryRun: true}
	}
	snapshot.Revision = c.Store.Revision() + 1
	commit := Commit{Event: event, Snapshot: snapshot, Decision: decision, Action: action, CommittedAt: c.now()}
	if !persist {
		commit.Snapshot.Revision = c.Store.Revision()
		return ProcessResult{Commit: commit, Result: CommitResult{Revision: c.Store.Revision()}, Encoded: encoded}, nil
	}
	result, err := c.Store.Commit(commit)
	if err != nil {
		return ProcessResult{}, err
	}
	commit.Snapshot.Revision = result.Revision
	return ProcessResult{Commit: commit, Result: result, Encoded: encoded}, nil
}

func (c *Core) processWithoutInference(event contract.Event) (ProcessResult, error) {
	if c == nil || c.Store == nil {
		return ProcessResult{}, errors.New("cognitive core is not configured")
	}
	if event.ID == "" || event.Type == "" || event.Source == "" {
		return ProcessResult{}, errors.New("event id, type and source are required")
	}
	decision := Decision{
		SchemaVersion: DecisionSchemaVersion, Status: "not_requested", Mode: "active_dry_run", Source: "mlp",
		Provenance: "test-harness", Test: true, HeadOrder: append([]string(nil), HeadOrder[:]...), InputDimension: CognitiveVectorSize,
		Action: ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", PhysicalActionExecuted: false}, GeneratedAt: c.now(),
	}
	return ProcessResult{Commit: Commit{Event: event, Snapshot: c.Store.Snapshot(), Decision: decision, CommittedAt: c.now()}, Result: CommitResult{Revision: c.Store.Revision()}}, nil
}

func isTestHarnessEvent(event contract.Event) bool {
	return event.Source == "api" && payloadBoolDefault(event.Payload, "test", false) && payloadString(event.Payload, "provenance") == "test-harness"
}

func (c *Core) composeSnapshot(previous CognitiveSnapshot, event contract.Event) (CognitiveSnapshot, error) {
	now := event.Timestamp.UTC()
	if now.IsZero() {
		now = c.now()
	}
	snapshot := previous.Normalized()
	snapshot.CapturedAt = now
	snapshot.SchemaVersion = SnapshotSchemaVersion
	snapshot.Episode.SecondsSinceLast = float32(maxDuration(now, previous.CapturedAt))
	if previous.CapturedAt.IsZero() || previous.CapturedAt.Unix() == 0 {
		snapshot.Episode.SecondsSinceFirst = 0
	} else {
		snapshot.Episode.SecondsSinceFirst += snapshot.Episode.SecondsSinceLast
	}
	payload := event.Payload
	if event.Type == contract.EventActionResult || event.Type == "discovery.action.result" {
		snapshot.ActionResults = append(snapshot.ActionResults, actionResultFromPayload(payload))
	}
	if contract.IsVisionEvent(event.Type) || strings.HasPrefix(event.Type, "vision") || strings.HasPrefix(event.Type, "synora.vision") {
		frame, err := frameFromVisionEvent(snapshot, event)
		if err != nil {
			return CognitiveSnapshot{}, err
		}
		evidence, err := VisionEvidenceFromFrame(context.Background(), frame)
		if err != nil {
			return CognitiveSnapshot{}, err
		}
		snapshot.VisionEvidence = evidence
		snapshot.Topology = frame.TopologyClass
		snapshot.Presence.HumanPresent = frame.Presence.HumanPresent
		snapshot.Presence.TrackCount = frame.Presence.TrackCount
		snapshot.Presence.TrackConfirmed = frame.Presence.TrackConfirmed
		snapshot.Episode.Phase = frame.EpisodePhase
		snapshot.Episode.SegmentCount = frame.Continuity.SegmentCount
		snapshot.Episode.GapCount = frame.Continuity.GapCount
		snapshot.Episode.CalmSeconds = frame.Continuity.CalmSeconds
		snapshot.Sensors.Movement = frame.CoEvidence.Movement
		snapshot.Sensors.AccessState = frame.CoEvidence.AccessState
		snapshot.Sensors.AlarmState = frame.CoEvidence.AlarmState
		snapshot.Sensors.SensorEvidence = frame.CoEvidence.SensorEvidence
		snapshot.Sensors.ObservationCount = frame.Quality.ObservationCount
		snapshot.Sensors.Confidence = frame.Quality.AggregateConfidence
	}
	if value, ok := payloadBool(payload, "armed"); ok {
		snapshot.Security.Armed = value
		snapshot.Security.Known = true
	}
	if value, ok := payloadBool(payload, "degraded"); ok {
		snapshot.Security.Degraded = value
	}
	if value, ok := payloadBool(payload, "movement"); ok {
		snapshot.Sensors.Movement = value
	}
	if snapshot.Topology == "" {
		snapshot.Topology = "unknown"
	}
	return snapshot.Normalized(), nil
}

func frameFromVisionEvent(previous CognitiveSnapshot, event contract.Event) (VisionEvidenceFrame, error) {
	p := event.Payload
	priority := payloadString(p, "priority")
	if priority == "" {
		priority = payloadString(p, "priority_hint")
	}
	if priority == "" {
		priority = contract.VisionPriorityP4
	}
	if priority == cognitiveContractP0() {
		priority = contract.VisionPriorityP4
	}
	phase := payloadString(p, "episode_phase")
	if phase == "" {
		if payloadBoolDefault(p, "is_final", false) {
			phase = VisionPhaseFinal
		} else if previous.Presence.HumanPresent {
			phase = VisionPhaseConfirmed
		} else {
			phase = VisionPhaseCandidate
		}
	}
	topology := payloadString(p, "topology")
	if topology == "" {
		topology = payloadStringDefault(p, "topology_class", previous.Topology)
	}
	humanPresent, trackCount, trackConfirmed := visionTrackFacts(p, previous.Presence)
	frame := VisionEvidenceFrame{SchemaVersion: VisionEvidenceSchemaVersion, CapturedAt: event.Timestamp.UTC(), Security: VisionEvidenceFrameSecurity{Armed: previous.Security.Armed, Degraded: previous.Security.Degraded, Known: previous.Security.Known}, Presence: VisionEvidenceFramePresence{HumanPresent: humanPresent, TrackCount: trackCount, TrackConfirmed: trackConfirmed}, TopologyClass: topology, Priority: priority, PriorityOrigin: VisionPriorityOriginVision, EpisodePhase: phase, Enrichment: payloadStringDefault(p, "enrichment_status", VisionEnrichmentUnavailable), Continuity: VisionEvidenceFrameContinuity{SecondsSinceFirstObservation: payloadFloatDefault(p, "seconds_since_first", previous.Episode.SecondsSinceFirst), SecondsSinceLastObservation: payloadFloatDefault(p, "seconds_since_last", previous.Episode.SecondsSinceLast), SegmentCount: payloadIntDefault(p, "segment_count", previous.Episode.SegmentCount+1), GapCount: payloadIntDefault(p, "gap_count", previous.Episode.GapCount), CalmSeconds: payloadFloatDefault(p, "calm_seconds", 0)}, Quality: VisionEvidenceFrameQuality{RealDetection: payloadBoolDefault(p, "real_detection", true), ReplaySimulation: payloadBoolDefault(p, "replay_simulation", false), ObservationCount: payloadIntDefault(p, "observation_count", previous.Sensors.ObservationCount+1), AggregateConfidence: payloadFloatDefault(p, "confidence", previous.Sensors.Confidence)}, CoEvidence: VisionEvidenceFrameCoEvidence{AccessState: payloadStringDefault(p, "access_state", previous.Sensors.AccessState), Movement: payloadBoolDefault(p, "movement", true), SensorEvidence: payloadBoolDefault(p, "sensor_evidence", true), AlarmState: payloadStringDefault(p, "alarm_state", previous.Sensors.AlarmState)}}
	return frame, frame.Validate()
}

func visionTrackFacts(payload map[string]any, previous PresenceFacts) (bool, int, bool) {
	humanPresent, hasHuman := payloadBool(payload, "human_present")
	trackCount, hasCount := payloadInt(payload, "track_count")
	trackConfirmed, hasConfirmed := payloadBool(payload, "track_confirmed")
	tracks, ok := payload["tracks"].([]any)
	if ok {
		if !hasCount {
			trackCount = len(tracks)
		}
		for _, raw := range tracks {
			track, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !hasHuman && payloadString(track, "subject_type") == "human" {
				humanPresent = true
			}
			if !hasConfirmed && payloadString(track, "state") == "confirmed" {
				trackConfirmed = true
			}
		}
	}
	if !hasHuman && !ok {
		humanPresent = previous.HumanPresent
	}
	if !hasCount && !ok {
		trackCount = previous.TrackCount
	}
	if !hasConfirmed && !ok {
		trackConfirmed = previous.TrackConfirmed
	}
	return humanPresent, maxInt(trackCount, 0), trackConfirmed
}

func (c *Core) Validate() error {
	if c == nil || c.Store == nil {
		return errors.New("core store unavailable")
	}
	if c.Gate.DryRun == false {
		return errors.New("V1 requires dry_run until physical promotion")
	}
	return c.Store.ValidateBounds()
}
func maxDuration(at, previous time.Time) time.Duration {
	if previous.IsZero() || at.Before(previous) {
		return 0
	}
	return at.Sub(previous)
}
func episodeID(event contract.Event) string {
	if event.ActivationID != "" {
		return event.ActivationID
	}
	if event.GroupKey != "" {
		return event.GroupKey
	}
	return event.ID
}
func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}
func payloadStringDefault(payload map[string]any, key, fallback string) string {
	if value := payloadString(payload, key); value != "" {
		return value
	}
	return fallback
}
func payloadBool(payload map[string]any, key string) (bool, bool) {
	if payload == nil {
		return false, false
	}
	value, ok := payload[key].(bool)
	return value, ok
}
func payloadBoolDefault(payload map[string]any, key string, fallback bool) bool {
	if value, ok := payloadBool(payload, key); ok {
		return value
	}
	return fallback
}
func payloadIntDefault(payload map[string]any, key string, fallback int) int {
	if payload == nil {
		return fallback
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	}
	return fallback
}
func payloadInt(payload map[string]any, key string) (int, bool) {
	if payload == nil {
		return 0, false
	}
	switch value := payload[key].(type) {
	case int:
		return value, true
	case float64:
		return int(value), true
	case json.Number:
		n, err := value.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}
func payloadFloatDefault(payload map[string]any, key string, fallback float32) float32 {
	if payload == nil {
		return fallback
	}
	switch value := payload[key].(type) {
	case float64:
		return float32(value)
	case float32:
		return value
	case int:
		return float32(value)
	}
	return fallback
}
func actionResultFromPayload(payload map[string]any) ActionResultFact {
	status := strings.ToLower(payloadString(payload, "status"))
	return ActionResultFact{Status: status, Successful: status == "success" || status == "executed", Failed: status == "failed" || status == "timeout" || status == "blocked", Unavailable: status == "unavailable"}
}
func cognitiveContractP0() string { return contract.VisionPriorityP0 }
