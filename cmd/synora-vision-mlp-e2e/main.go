package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
	"synora/pkg/contract"
)

const (
	transitionInitial   = "initial"
	transitionCandidate = "p1_candidate"
	transitionConfirmed = "p1_confirmed"
	transitionFinal     = "final"
	fixtureSchema       = "synora.vision-mlp-e2e-fixture/v1"
	traceSchema         = "synora.vision-mlp-e2e-trace/v1"
	forbiddenTraceToken = "cognitive trace contains raw visual or biometric data"
)

type visionObservation struct {
	EpisodeID     string                                `json:"episode_id"`
	TopologyClass string                                `json:"topology_class"`
	Trigger       string                                `json:"trigger"`
	ObservedAt    time.Time                             `json:"observed_at"`
	Sequence      int                                   `json:"sequence"`
	PriorityHint  string                                `json:"priority_hint"`
	PriorityState string                                `json:"priority_state"`
	ReasonCodes   []string                              `json:"reason_codes"`
	Tracks        []contract.VisionClipObservationTrack `json:"tracks"`
	Backend       contract.VisionClipObservationBackend `json:"backend"`
}

type transitionInput struct {
	Name          string
	EpisodeID     string
	At            time.Time
	Priority      string
	PriorityState string
	Topology      string
	Trigger       string
}

type fixture struct {
	Schema      string   `json:"schema"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Expected    []string `json:"expected"`
	TeacherOnly bool     `json:"teacher_only"`
	FailClosed  bool     `json:"fail_closed"`
}

type options struct {
	bundle          string
	runtimeManifest string
	modelDir        string
	clip            string
	out             string
	observations    string
	summaryContract string
	segmentManifest string
	parity          string
	fixtures        string
}

func main() {
	var o options
	flag.StringVar(&o.bundle, "bundle", "/home/rock/synora-cognitive-mlp-v1", "verified cognitive bundle")
	flag.StringVar(&o.runtimeManifest, "runtime-manifest", "models/cognitive/MANIFEST.runtime.json", "runtime manifest")
	flag.StringVar(&o.modelDir, "model-dir", "", "exported CPU/ONNX model directory")
	flag.StringVar(&o.clip, "clip", "", "local clip used by the segment replay")
	flag.StringVar(&o.out, "out", "", "E2E artifact directory")
	flag.StringVar(&o.observations, "observations", "", "safe observations JSONL from the segment replay")
	flag.StringVar(&o.summaryContract, "summary-contract", "", "final Core summary JSON")
	flag.StringVar(&o.segmentManifest, "segment-manifest", "", "segment manifest used for continuity aggregation")
	flag.StringVar(&o.parity, "parity", "", "PyTorch/ONNX/CPU parity report")
	flag.StringVar(&o.fixtures, "fixtures", "testdata/vision-mlp-e2e", "declarative fixture directory")
	flag.Parse()
	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, "synora-vision-mlp-e2e:", err)
		os.Exit(1)
	}
}

func run(o options) error {
	if strings.TrimSpace(o.out) == "" || strings.TrimSpace(o.observations) == "" || strings.TrimSpace(o.summaryContract) == "" {
		return errors.New("--out, --observations and --summary-contract are required")
	}
	if err := os.MkdirAll(o.out, 0o750); err != nil {
		return err
	}
	if o.clip != "" {
		if info, err := os.Stat(o.clip); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("clip is not a regular local file: %s", o.clip)
		}
	}
	fixtureNames, err := loadFixtures(o.fixtures)
	if err != nil {
		return err
	}
	observations, err := readObservations(o.observations)
	if err != nil {
		return err
	}
	if len(observations) == 0 {
		return errors.New("segment replay produced no observations")
	}
	if strings.TrimSpace(o.segmentManifest) == "" {
		o.segmentManifest = filepath.Join(filepath.Dir(o.observations), "manifest.json")
	}
	segments, err := readStateFrameV5Segments(o.segmentManifest)
	if err != nil {
		return err
	}
	episodeID := observations[0].EpisodeID
	if episodeID == "" {
		return errors.New("segment observations have no episode id")
	}
	var candidate, confirmed *visionObservation
	for i := range observations {
		item := observations[i]
		if item.PriorityState == "candidate" && candidate == nil {
			copy := item
			candidate = &copy
		}
		if item.PriorityState == "confirmed" && confirmed == nil {
			copy := item
			confirmed = &copy
		}
	}
	if candidate == nil || confirmed == nil {
		return errors.New("segment replay did not produce P1 candidate followed by confirmed observation")
	}
	finalAt := confirmed.ObservedAt.Add(time.Second)
	if body, readErr := os.ReadFile(o.summaryContract); readErr != nil {
		return fmt.Errorf("read final Core summary: %w", readErr)
	} else {
		var summary struct {
			EpisodeID  string    `json:"episode_id"`
			ObservedAt time.Time `json:"observed_at"`
		}
		if json.Unmarshal(body, &summary) == nil {
			if summary.EpisodeID != "" {
				episodeID = summary.EpisodeID
			}
			if !summary.ObservedAt.IsZero() {
				finalAt = summary.ObservedAt
			}
		}
	}

	verification, verifyErr := cognitive.VerifyBundle(o.bundle, o.runtimeManifest)
	parityPassed, parityReason := readParity(o.parity)
	var backend *cognitive.CPUMLPBackend
	backendStatus := "unavailable"
	backendReason := ""
	if verifyErr != nil {
		backendReason = verifyErr.Error()
	} else if !parityPassed {
		backendReason = parityReason
	} else if strings.TrimSpace(o.modelDir) == "" {
		backendReason = "exported CPU/ONNX model directory is not available"
	} else {
		backend, verifyErr = cognitive.NewVerifiedCPUMLPBackend(o.modelDir, verification)
		if verifyErr != nil {
			backendReason = verifyErr.Error()
		} else {
			backendStatus = "loaded"
		}
	}
	if backendReason == "" && backendStatus != "loaded" {
		backendReason = "MLP unavailable"
	}

	inputs := []transitionInput{
		{Name: transitionInitial, EpisodeID: episodeID, At: candidate.ObservedAt.Add(-time.Second), Topology: candidate.TopologyClass, Trigger: candidate.Trigger},
		{Name: transitionCandidate, EpisodeID: episodeID, At: candidate.ObservedAt, Priority: candidate.PriorityHint, PriorityState: candidate.PriorityState, Topology: candidate.TopologyClass, Trigger: candidate.Trigger},
		{Name: transitionConfirmed, EpisodeID: episodeID, At: confirmed.ObservedAt, Priority: confirmed.PriorityHint, PriorityState: confirmed.PriorityState, Topology: confirmed.TopologyClass, Trigger: confirmed.Trigger},
		{Name: transitionFinal, EpisodeID: episodeID, At: finalAt, Priority: confirmed.PriorityHint, PriorityState: "confirmed", Topology: confirmed.TopologyClass, Trigger: confirmed.Trigger},
	}

	traces := make([]map[string]any, 0, len(inputs)+3)
	traces = append(traces, map[string]any{"schema": traceSchema, "stage": "discovery_episode_runtime", "episode_id": episodeID, "segment_simulation": true, "camera_transport_real": false})
	traces = append(traces, map[string]any{"schema": traceSchema, "stage": "core_event_store_topology_security", "security_mode": "armed_away", "known_residents_present": false, "topology_class": "protected_interior", "trigger": "motion"})
	transitions := make([]map[string]any, 0, len(inputs))
	teacherLines := make([]map[string]any, 0, len(inputs))
	v5Captures := make([]map[string]any, 0, len(inputs))
	latency := map[string]float64{cognitive.DangerHead: 0, cognitive.IncidentHead: 0, cognitive.TaskHead: 0, cognitive.ActionHead: 0}
	stateframeV4LatencyMS := float64(0)
	stateframeV5LatencyMS := float64(0)
	v5Encoder := cognitive.V5StateEncoder{}
	for index, input := range inputs {
		frame := frameFor(input, index)
		catalog := actionCatalog()
		ledger := ledgerFor(input.Name)
		v4Started := time.Now()
		cognitiveInput, buildErr := cognitive.BuildInput(context.Background(), "vision-mlp-"+input.Name, cognitive.Task{ID: "vision-mlp-" + input.Name, Kind: "vision_segment_event", RequestedCapabilities: []string{cognitive.CapabilityEventReasoning}, RequestedModalities: []string{cognitive.ModalityEvent, cognitive.ModalityStructured, cognitive.ModalityTopology}}, frame, catalog, cognitive.V4StateEncoder{})
		if buildErr != nil {
			return buildErr
		}
		stateframeV4LatencyMS += float64(time.Since(v4Started).Microseconds()) / 1000
		cognitiveInput.CreatedAt = input.At.UTC()
		cognitiveInput.ActionLedger = ledger
		v5Input := input
		if input.Name == transitionFinal && len(observations) > 0 {
			v5Input.At = observations[len(observations)-1].ObservedAt.Add(time.Second)
		}
		v5Frame := stateFrameV5For(v5Input, frame, observations, segments)
		v5Started := time.Now()
		v5Encoded, v5Err := v5Encoder.Encode(context.Background(), v5Frame)
		if v5Err != nil {
			return fmt.Errorf("stateframe v5 transition %s failed: %w", input.Name, v5Err)
		}
		v5LatencyMS := float64(time.Since(v5Started).Microseconds()) / 1000
		stateframeV5LatencyMS += v5LatencyMS
		v5Fingerprint, v5Err := v5Frame.Fingerprint()
		if v5Err != nil {
			return fmt.Errorf("stateframe v5 fingerprint %s failed: %w", input.Name, v5Err)
		}
		teacher := teacherFor(input)
		var mlp map[string]any
		safety := cognitive.SafetyDecision{AdvisoryOnly: true, AllowedActions: []string{}, BlockedActions: []cognitive.FilteredAction{}, ExecutorCalled: false}
		divergence := map[string]any{"kind": "unavailable", "severity": "review"}
		if backend != nil {
			started := time.Now()
			output, runErr := cognitive.NewScheduler(cognitive.RegistryWithBackend(backend)).Run(context.Background(), cognitiveInput)
			if runErr != nil {
				return fmt.Errorf("MLP transition %s failed: %w", input.Name, runErr)
			}
			headLatency := backend.HeadLatencyMS()
			if len(headLatency) == 0 {
				elapsed := float64(time.Since(started).Microseconds()) / 1000
				for _, head := range []string{cognitive.DangerHead, cognitive.IncidentHead, cognitive.TaskHead, cognitive.ActionHead} {
					latency[head] += elapsed / 4
				}
			} else {
				for head, value := range headLatency {
					latency[head] += value
				}
			}
			safety = cognitive.ApplySafetyGate(output, catalog)
			mlp = mlpView(output, backend)
			divergence = compareTeacher(teacher, output)
		} else {
			mlp = map[string]any{"status": "unavailable", "reason": backendReason, "provenance": "none"}
		}
		record := map[string]any{
			"episode_id": input.EpisodeID, "transition": input.Name, "state_encoder_schema": cognitive.StateEncoderSchemaVersion,
			"state_fingerprint": cognitiveInput.EncodedState.FrameChecksum, "vision_priority": priorityOrNone(input.Priority),
			"stateframe_v5_schema": cognitive.StateEncoderV5SchemaVersion, "stateframe_v5_dimension": cognitive.EncoderV5Size,
			"stateframe_v5_fingerprint": v5Fingerprint, "stateframe_v5_latency_ms": v5LatencyMS,
			"teacher": teacher, "mlp": mlp, "safety": safety, "divergence": divergence,
			"unrepresented_evidence": []string{"segment_index_and_track_continuity_not_projected_into_state_encoder_v4"},
		}
		transitions = append(transitions, record)
		teacherLines = append(teacherLines, map[string]any{"episode_id": input.EpisodeID, "transition": input.Name, "teacher": teacher, "mlp": mlp, "divergence": divergence})
		traces = append(traces, map[string]any{"schema": traceSchema, "stage": "state_frame", "transition": input.Name, "state_fingerprint": cognitiveInput.EncodedState.FrameChecksum, "state_encoder_schema": cognitive.StateEncoderSchemaVersion})
		traces = append(traces, map[string]any{"schema": traceSchema, "stage": "state_frame_v5_shadow", "transition": input.Name, "stateframe_v5_fingerprint": v5Fingerprint, "state_encoder_schema": cognitive.StateEncoderV5SchemaVersion, "state_encoder_dimension": cognitive.EncoderV5Size, "latency_ms": v5LatencyMS, "forwarded_to_mlp": false})
		v5Captures = append(v5Captures, map[string]any{
			"schema": cognitive.StateFrameV5CaptureSchema, "state_encoder_schema": cognitive.StateEncoderV5SchemaVersion, "state_encoder_version": cognitive.EncoderV5Version,
			"feature_dimension": cognitive.EncoderV5Size, "feature_order": cognitive.EncoderV5FeatureNames, "state": v5Frame.Normalized(),
			"vector": v5Encoded.Values, "v5_fingerprint": v5Fingerprint,
			"v4":              map[string]any{"schema": cognitive.StateEncoderSchemaVersion, "dimension": cognitive.EncoderV4Size, "frame_fingerprint": cognitiveInput.EncodedState.FrameChecksum},
			"vision_evidence": stateFrameV5Evidence(v5Frame), "teacher": teacher,
			"provenance": map[string]any{"source": "segmented_vision_replay", "replay_simulation": true, "physical_action_executed": false, "cognitive_mode": "advisory_shadow"},
		})
	}
	traces = append(traces, map[string]any{"schema": traceSchema, "stage": "safety_gate", "advisory_only": true, "physical_action_executed": false, "executor_called": false})
	if err := writeJSONL(filepath.Join(o.out, "trace.jsonl"), traces); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(o.out, "mlp_transitions.jsonl"), transitions); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(o.out, "teacher_vs_mlp.jsonl"), teacherLines); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(o.out, "stateframe-v5-capture.jsonl"), v5Captures); err != nil {
		return err
	}

	divergenceCounts := map[string]int{}
	for _, item := range transitions {
		if value, ok := item["divergence"].(map[string]any); ok {
			if kind, ok := value["kind"].(string); ok {
				divergenceCounts[kind]++
			}
		}
	}
	summary := map[string]any{
		"schema": "synora.vision-mlp-shadow-summary/v1", "mlp_loaded": backend != nil,
		"mlp_backend": map[bool]string{true: "cpu_json", false: "unavailable"}[backend != nil], "mlp_unavailable_reason": func() string {
			if backend == nil {
				return backendReason
			}
			return ""
		}(),
		"teacher_available": true, "cognitive_mode": "advisory_shadow", "physical_action_executed": false,
		"camera_transport_real": false, "segment_simulation": true, "episode_id": episodeID,
		"state_encoder_schema": cognitive.StateEncoderSchemaVersion, "state_encoder_size": cognitive.EncoderV4Size,
		"stateframe_v4_dimension": cognitive.EncoderV4Size, "stateframe_v5_schema": cognitive.StateEncoderV5SchemaVersion, "stateframe_v5_dimension": cognitive.EncoderV5Size,
		"stateframe_v4_latency_ms": stateframeV4LatencyMS, "stateframe_v5_latency_ms": stateframeV5LatencyMS, "stateframe_v5_forwarded_to_mlp": false,
		"stateframe_v5_capture": "stateframe-v5-capture.jsonl",
		"bundle_manifest_sha256": func() string {
			if verifyErr == nil {
				return verification.ManifestSHA256
			}
			return ""
		}(),
		"verified_artifact_sha256": func() map[string]string {
			result := map[string]string{}
			if verifyErr == nil {
				for _, name := range verification.HeadNames() {
					result[name] = verification.Runtime.Heads[name].SourceSHA256
				}
			}
			return result
		}(),
		"parity_passed": parityPassed, "parity_reason": parityReason, "transitions": len(transitions),
		"fixture_scenarios": fixtureNames, "divergence_counts": divergenceCounts, "latency_ms_by_head": latency,
	}
	if err := writeJSON(filepath.Join(o.out, "summary.json"), summary); err != nil {
		return err
	}
	if err := writeReport(filepath.Join(o.out, "report.md"), summary, transitions, backend != nil, backendReason, verification, fixtureNames); err != nil {
		return err
	}
	return nil
}

func readObservations(path string) ([]visionObservation, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var result []visionObservation
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item visionObservation
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, fmt.Errorf("decode safe observation: %w", err)
		}
		if item.EpisodeID == "" || item.ObservedAt.IsZero() || item.PriorityHint == "" {
			continue
		}
		result = append(result, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].ObservedAt.Before(result[j].ObservedAt) })
	return result, nil
}

func readParity(path string) (bool, string) {
	if strings.TrimSpace(path) == "" {
		return false, "parity report is unavailable"
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false, "parity report is unavailable"
	}
	var report struct {
		Passed  bool               `json:"passed"`
		Maximum map[string]float64 `json:"maximum_absolute_difference"`
	}
	if err := json.Unmarshal(body, &report); err != nil {
		return false, "parity report is invalid"
	}
	if !report.Passed {
		return false, "PyTorch/ONNX/CPU parity failed"
	}
	return true, "parity passed"
}

func frameFor(input transitionInput, index int) cognitive.StateFrame {
	current := (*cognitive.StateEvent)(nil)
	if index > 0 {
		current = &cognitive.StateEvent{ID: input.EpisodeID + "-" + input.Name, Type: "motion.person.detected", Source: "core.vision", NodeID: input.Topology, Priority: 5, Confidence: 0.98, Timestamp: input.At.UTC()}
	}
	facts := []string{"access.outside_only", "evidence.verification_missing"}
	if index >= 2 {
		facts = append(facts, "presence.interior_signal")
	}
	return cognitive.StateFrame{SchemaVersion: cognitive.StateFrameSchemaVersion, Revision: uint64(index + 1), CapturedAt: input.At.UTC(), CurrentEvent: current, System: cognitive.StateSystem{LastState: "armed_away", Armed: true}, Topology: []cognitive.StateNode{{ID: input.Topology, Type: input.Topology}}, Residents: []cognitive.StateResident{{ID: "resident-absent", Enabled: true, Trusted: true, State: "absent"}}, RecentEvents: []cognitive.StateEvent{}, SecurityMode: "armed_away", KnownResidentsPresent: false, KnownResidentCount: 1, ContextFacts: facts, Temporal: cognitive.EncoderV4TemporalContext{EvaluationTrigger: "event", SecondsSinceLastEvent: float32(index + 1), EventCount: index + 1}}
}

func actionCatalog() cognitive.ActionCatalog {
	values := []struct{ id, capability, risk string }{{"observation.record@event_zone", "observation.record", "low"}, {"observation.increase@event_zone", "observation.increase", "low"}, {"camera.record@event_zone", "camera.record", "low"}, {"light.activate@event_zone", "light.activate", "medium"}, {"notify.security@global", "notify.security", "high"}, {"incident.create@global", "incident.create", "teacher_gated"}}
	catalog := cognitive.ActionCatalog{SchemaVersion: cognitive.ActionCatalogSchemaVersion, Revision: 1}
	for _, value := range values {
		catalog.Actions = append(catalog.Actions, cognitive.ActionDescriptor{ID: value.id, Slot: value.id, Capability: value.capability, RiskClass: value.risk, Enabled: true, RequiresTeacherApproval: value.risk != "low"})
	}
	return catalog
}

func ledgerFor(transition string) cognitive.ActionLedgerSnapshot {
	ledger := cognitive.ActionLedgerSnapshot{SchemaVersion: cognitive.ActionLedgerSchemaVersion, IncidentOpen: transition != transitionInitial}
	if transition == transitionCandidate || transition == transitionConfirmed || transition == transitionFinal {
		ledger.RecentActions = append(ledger.RecentActions, cognitive.ActionLedgerEntry{Slot: "camera.record@event_zone", Status: "executed"})
	}
	if transition == transitionFinal {
		ledger.RecentActions = append(ledger.RecentActions, cognitive.ActionLedgerEntry{Slot: "observation.increase@event_zone", Status: "rejected"})
	}
	return ledger
}

func teacherFor(input transitionInput) map[string]any {
	level := "none"
	tags := []string{}
	tasks := []string{"monitor"}
	actions := []string{"observation.record@event_zone"}
	if input.Name != transitionInitial {
		level = "high"
		tags = []string{"interior_intrusion", "unknown_requires_verification"}
		tasks = []string{"collect_evidence", "deter_presence", "notify_security"}
		actions = []string{"observation.record@event_zone", "notify.security@global"}
	}
	return map[string]any{"source": "core_teacher_v1", "danger": level, "incident_tags": tags, "tasks": tasks, "actions": actions, "p0_floor_preserved": true}
}

func mlpView(output cognitive.CognitiveOutput, backend *cognitive.CPUMLPBackend) map[string]any {
	probabilities := map[string]float32{}
	for i, label := range backend.Danger.Labels {
		if i < len(output.DangerProbabilities) {
			probabilities[label] = output.DangerProbabilities[i]
		}
	}
	incident := []string{}
	for _, value := range output.IncidentTags {
		if value.Selected {
			incident = append(incident, value.Label)
		}
	}
	tasks := []string{}
	for _, value := range output.TaskScores {
		if value.Selected {
			tasks = append(tasks, value.Label)
		}
	}
	filtered := output.FilteredActions
	if filtered == nil {
		filtered = []cognitive.FilteredAction{}
	}
	return map[string]any{"status": "loaded", "provenance": "runtime_manifest", "manifest_sha256": backend.RuntimeManifestSHA256, "danger": map[string]any{"level": output.DangerLabel, "probabilities": probabilities}, "incident_tags": incident, "phase": output.IncidentPhase, "tasks": tasks, "proposed_actions": output.ProposedActionIDs, "filtered_actions": filtered}
}

func compareTeacher(teacher map[string]any, output cognitive.CognitiveOutput) map[string]any {
	if teacher["danger"] != output.DangerLabel {
		return map[string]any{"kind": "danger", "severity": "review"}
	}
	if !sameStringSet(teacherStrings(teacher["incident_tags"]), selectedIncident(output)) {
		return map[string]any{"kind": "incident", "severity": "informational"}
	}
	if !sameStringSet(teacherStrings(teacher["tasks"]), selectedTasks(output)) {
		return map[string]any{"kind": "task", "severity": "informational"}
	}
	if !sameStringSet(teacherStrings(teacher["actions"]), output.ProposedActionIDs) {
		return map[string]any{"kind": "action", "severity": "review"}
	}
	return map[string]any{"kind": "match", "severity": "none"}
}

func selectedIncident(output cognitive.CognitiveOutput) []string {
	result := []string{}
	for _, value := range output.IncidentTags {
		if value.Selected {
			result = append(result, value.Label)
		}
	}
	return result
}

func selectedTasks(output cognitive.CognitiveOutput) []string {
	result := []string{}
	for _, value := range output.TaskScores {
		if value.Selected {
			result = append(result, value.Label)
		}
	}
	return result
}

func teacherStrings(value any) []string {
	items, _ := value.([]string)
	return items
}

func sameStringSet(left, right []string) bool {
	leftCopy, rightCopy := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(leftCopy)
	sort.Strings(rightCopy)
	if len(leftCopy) != len(rightCopy) {
		return false
	}
	for i := range leftCopy {
		if leftCopy[i] != rightCopy[i] {
			return false
		}
	}
	return true
}

func priorityOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func loadFixtures(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		body, readErr := os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return nil, readErr
		}
		var value fixture
		if err := json.Unmarshal(body, &value); err != nil {
			return nil, fmt.Errorf("fixture %s: %w", entry.Name(), err)
		}
		if value.Schema != fixtureSchema || value.Name == "" || len(value.Expected) == 0 {
			return nil, fmt.Errorf("fixture %s is invalid", entry.Name())
		}
		seen[value.Name] = true
	}
	want := []string{"routine_baseline", "perimeter_presence", "p1_candidate_protected_interior", "p1_confirmed_protected_interior", "p0_life_safety_core", "unknown_topology", "action_unavailable", "action_already_ledgered", "mlp_bundle_unavailable"}
	for _, name := range want {
		if !seen[name] {
			return nil, fmt.Errorf("fixture %s is missing", name)
		}
	}
	sort.Strings(want)
	return want, nil
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	return os.WriteFile(path, body, 0o640)
}

func writeJSONL(path string, values []map[string]any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, value := range values {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(body))
		for _, token := range []string{"bbox", "embedding", "biometric", "face_image", ".mp4", "jpeg", "png"} {
			if strings.Contains(lower, token) {
				return errors.New(forbiddenTraceToken)
			}
		}
		if err := encoder.Encode(value); err != nil {
			return err
		}
	}
	return nil
}

func writeReport(path string, summary map[string]any, transitions []map[string]any, loaded bool, reason string, verification cognitive.BundleVerification, fixtures []string) error {
	var builder strings.Builder
	builder.WriteString("# Vision MLP advisory shadow E2E\n\n")
	fmt.Fprintf(&builder, "- MLP loaded: `%t`\n- MLP backend: `%v`\n- teacher available: `true`\n- cognitive mode: `advisory_shadow`\n- physical_action_executed: `false`\n- camera_transport_real: `false`\n- segment_simulation: `true`\n", loaded, summary["mlp_backend"])
	if !loaded {
		fmt.Fprintf(&builder, "- MLP unavailable reason: `%s`\n", reason)
	}
	if verification.ManifestSHA256 != "" {
		fmt.Fprintf(&builder, "- source manifest SHA-256: `%s`\n", verification.ManifestSHA256)
	}
	fmt.Fprintf(&builder, "- verified heads: `%v`\n- state encoder V4: `%s` (%d)\n- state encoder V5 shadow: `%s` (%d), forwarded to MLP: `%v`\n- parity: `%v`\n- fixtures: `%v`\n\n", verification.HeadNames(), cognitive.StateEncoderSchemaVersion, cognitive.EncoderV4Size, cognitive.StateEncoderV5SchemaVersion, cognitive.EncoderV5Size, summary["stateframe_v5_forwarded_to_mlp"], summary["parity_passed"], fixtures)
	fmt.Fprintf(&builder, "- latency by head (ms): `%v`\n- outputs recorded: `danger`, `incident`, `task`, `action`\n- action filtering sources: `Device Store`, `Action Ledger`, `Safety Gate`\n\n", summary["latency_ms_by_head"])
	builder.WriteString("## Teacher vs MLP transitions\n\n| transition | teacher danger | MLP danger/status | divergence |\n|---|---|---|---|\n")
	for _, value := range transitions {
		teacher := value["teacher"].(map[string]any)
		mlp := value["mlp"].(map[string]any)
		divergence := value["divergence"].(map[string]any)
		mlpDanger := fmt.Sprint(mlp["status"])
		if danger, ok := mlp["danger"].(map[string]any); ok {
			mlpDanger = fmt.Sprint(danger["level"])
		}
		fmt.Fprintf(&builder, "| `%v` | `%v` | `%s` | `%v` |\n", value["transition"], teacher["danger"], mlpDanger, divergence["kind"])
		if actions, ok := mlp["proposed_actions"]; ok {
			fmt.Fprintf(&builder, "  - proposed actions: `%v`; filtered by Device Store/Action Ledger: `%v`\n", actions, mlp["filtered_actions"])
		}
	}
	builder.WriteString("\nNo executor, device, notification, lock, light, siren or external API was called. Vision-specific segment/track evidence remains explicitly unrepresented by `state-encoder/v4`.\n")
	return os.WriteFile(path, []byte(builder.String()), 0o640)
}
