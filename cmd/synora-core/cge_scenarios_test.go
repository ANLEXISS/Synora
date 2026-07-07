package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"synora/internal/automation"
	"synora/pkg/contract"
)

type scenarioCore struct {
	app *coreApp
	bus *memoryCoreBus
}

func newTestCore(t *testing.T) *scenarioCore {
	t.Helper()
	app, bus := newTestCoreApp(t)
	return &scenarioCore{app: app, bus: bus}
}

func sendTestEvent(t *testing.T, core *scenarioCore, eventType string, deviceID string, identity string, at time.Time, payload map[string]any) *contract.Event {
	t.Helper()
	if payload == nil {
		payload = map[string]any{}
	}
	if identity != "" {
		payload["identity"] = identity
	}
	event := &contract.Event{
		ID:         fmt.Sprintf("evt-%s-%s-%d", eventType, deviceID, at.UnixNano()),
		Type:       eventType,
		Source:     "vision-worker",
		Timestamp:  at,
		DeviceID:   deviceID,
		Identity:   identity,
		Confidence: 0.90,
		Payload:    payload,
	}
	if eventType == contract.EventDeviceOffline {
		event.Source = "discovery"
	}
	if confidence, ok := payload["confidence"].(float64); ok {
		event.Confidence = confidence
	}

	core.app.processEvent(event)
	return event
}

func snapshot(t *testing.T, core *scenarioCore) contract.PublicSnapshot {
	t.Helper()
	public := contract.PublicSnapshotFromCoreState(core.app.snapshotBuilder.CoreState())
	t.Logf("public snapshot: %+v", public)
	return public
}

func findResident(public contract.PublicSnapshot, id string) map[string]any {
	return findByID(public.Residents, id)
}

func findDevice(public contract.PublicSnapshot, id string) map[string]any {
	return findByID(public.Devices, id)
}

func findClip(public contract.PublicSnapshot, id string) map[string]any {
	return findByID(public.Clips, id)
}

func assertNoInternalSnapshotKeys(t *testing.T, public contract.PublicSnapshot) {
	t.Helper()
	data, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public snapshot: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("unmarshal public snapshot: %v", err)
	}
	for _, key := range []string{"state_store", "device", "event", "automation"} {
		if _, ok := root[key]; ok {
			t.Fatalf("public snapshot exposes internal or legacy key %q in %s", key, string(data))
		}
	}
	for _, key := range []string{"devices", "events", "automations"} {
		if _, ok := root[key]; !ok {
			t.Fatalf("public snapshot missing public key %q in %s", key, string(data))
		}
	}
	if string(data) == "" || containsZeroTimestamp(data) {
		t.Fatalf("public snapshot exposes zero timestamp in %s", string(data))
	}
}

func containsZeroTimestamp(data []byte) bool {
	return strings.Contains(string(data), "0001-01-01T00:00:00Z")
}

func assertEventVisible(t *testing.T, public contract.PublicSnapshot, id string, eventType string) map[string]any {
	t.Helper()
	eventView := findByID(public.Events, id)
	if eventView == nil {
		t.Fatalf("event %s missing from snapshot events: %#v", id, public.Events)
	}
	if eventView["type"] != eventType {
		t.Fatalf("event %s type mismatch: %#v", id, eventView)
	}
	return eventView
}

func collectActionRequests(t *testing.T, core *scenarioCore) []contract.ActionRequest {
	t.Helper()
	messages := core.bus.messagesOfType(contract.EventActionRequest)
	out := make([]contract.ActionRequest, 0, len(messages))
	for _, msg := range messages {
		var request contract.ActionRequest
		if err := json.Unmarshal(msg.Payload, &request); err != nil {
			t.Fatalf("unmarshal action request: %v", err)
		}
		out = append(out, request)
	}
	return out
}

func collectDecisions(t *testing.T, core *scenarioCore) []contract.Decision {
	t.Helper()
	messages := core.bus.messagesOfType("engine.decision")
	out := make([]contract.Decision, 0, len(messages))
	for _, msg := range messages {
		var decision contract.Decision
		if err := json.Unmarshal(msg.Payload, &decision); err != nil {
			t.Fatalf("unmarshal decision: %v", err)
		}
		out = append(out, decision)
	}
	return out
}

func lastDecision(t *testing.T, core *scenarioCore) contract.Decision {
	t.Helper()
	decisions := collectDecisions(t, core)
	if len(decisions) == 0 {
		t.Fatalf("expected at least one decision")
	}
	return decisions[len(decisions)-1]
}

func TestCGEScenarioKnownResidentDetected(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)

	public := snapshot(t, core)
	resident := findResident(public, "alexis")
	if resident == nil || resident["state"] != "present" || resident["last_seen"] == nil {
		t.Fatalf("alexis should be present with last_seen: %#v", resident)
	}
	if core.app.state.SystemState().IntrusionActive {
		t.Fatalf("known resident should not trigger intrusion: %#v", core.app.state.SystemState())
	}
	assertEventVisible(t, public, event.ID, contract.EventVisionIdentity)
}

func TestCGEScenarioKnownResidentMoves(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	second := sendTestEvent(t, core, contract.EventVisionIdentity, "cam_02", "alexis", scenarioTime(30*time.Second), nil)

	public := snapshot(t, core)
	resident := findResident(public, "alexis")
	if resident == nil || resident["node_id"] != "salon" || resident["state"] != "present" {
		t.Fatalf("alexis should end in salon: %#v", resident)
	}
	assertEventVisible(t, public, second.ID, contract.EventVisionIdentity)
}

func TestCGEScenarioTwoResidentsDetected(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_02", "carole", scenarioTime(5*time.Second), nil)

	public := snapshot(t, core)
	for _, id := range []string{"alexis", "carole"} {
		resident := findResident(public, id)
		if resident == nil || resident["state"] != "present" || resident["last_seen"] == nil {
			t.Fatalf("%s should have distinct presence: %#v", id, resident)
		}
	}
}

func TestCGEScenarioUnknownDetected(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionUnknown, "cam_01", "", scenarioTime(0), map[string]any{"confidence": 0.72})

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionUnknown)
	decision := lastDecision(t, core)
	if decision.EventID != event.ID || decision.NodeID != "entry" {
		t.Fatalf("unexpected unknown decision: %#v", decision)
	}
	state := core.app.state.SystemState().LastState
	if state != "suspicious" && state != "intrusion" && state != "activity" {
		t.Fatalf("unknown should produce non-idle coherent state, got %q", state)
	}
}

func TestCGEScenarioUnknownThenResident(t *testing.T) {
	core := newTestCore(t)
	unknown := sendTestEvent(t, core, contract.EventVisionUnknown, "cam_01", "", scenarioTime(0), nil)
	known := sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(10*time.Second), nil)

	public := snapshot(t, core)
	assertEventVisible(t, public, unknown.ID, contract.EventVisionUnknown)
	assertEventVisible(t, public, known.ID, contract.EventVisionIdentity)
	resident := findResident(public, "alexis")
	if resident == nil || resident["state"] != "present" {
		t.Fatalf("known resident should be recognized after unknown: %#v", resident)
	}
	t.Logf("state after unknown then resident: %#v", core.app.state.SystemState())
}

func TestCGEScenarioUncertainIdentityWithBestMatch(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionUncertain, "cam_01", "", scenarioTime(0), map[string]any{
		"best_match": "alexis",
		"confidence": 0.42,
	})

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionUncertain)
	decision := lastDecision(t, core)
	if decision.EventID != event.ID || decision.Type == "" {
		t.Fatalf("uncertain event should produce coherent decision: %#v", decision)
	}
}

func TestCGEScenarioMotionWithoutIdentity(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionMotion, "cam_02", "", scenarioTime(0), map[string]any{"motion": true})

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionMotion)
	device := findDevice(public, "cam_02")
	if device == nil || device["last_seen"] == nil || device["online"] != true {
		t.Fatalf("cam_02 should be online with last_seen: %#v", device)
	}
}

func TestCGEScenarioSensorNoise(t *testing.T) {
	core := newTestCore(t)
	for i := 0; i < 6; i++ {
		sendTestEvent(t, core, contract.EventVisionMotion, "cam_02", "", scenarioTime(time.Duration(i)*time.Second), map[string]any{"motion": true})
	}

	public := snapshot(t, core)
	if len(public.Events) != 6 {
		t.Fatalf("expected six motion events retained, got %d", len(public.Events))
	}
	if len(collectActionRequests(t, core)) != 0 {
		t.Fatalf("sensor noise should not dispatch action without automation")
	}
	if core.app.state.Size() > 15 {
		t.Fatalf("sensor noise should not explode state store, size=%d snapshot=%#v", core.app.state.Size(), public)
	}
}

func TestCGEScenarioCameraOffline(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventDeviceOffline, "cam_03", "", scenarioTime(0), nil)

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventDeviceOffline)
	device := findDevice(public, "cam_03")
	if device == nil || device["online"] != false || device["last_seen"] == nil {
		t.Fatalf("cam_03 should be offline with last_seen: %#v", device)
	}
}

func TestCGEScenarioCameraReturnsOnline(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventDeviceOffline, "cam_03", "", scenarioTime(0), nil)
	motion := sendTestEvent(t, core, contract.EventVisionMotion, "cam_03", "", scenarioTime(20*time.Second), nil)

	public := snapshot(t, core)
	assertEventVisible(t, public, motion.ID, contract.EventVisionMotion)
	device := findDevice(public, "cam_03")
	if device == nil || device["online"] != true || device["last_seen"] == nil {
		t.Fatalf("cam_03 should return online after motion: %#v", device)
	}
}

func TestCGEScenarioWeaponDetected(t *testing.T) {
	core := newTestCore(t)
	if err := core.app.automation.Add(automation.Rule{
		ID:        "weapon-critical-action",
		EventType: contract.EventVisionWeapon,
		Actions:   []contract.Action{{Device: "siren", Command: "on"}},
	}); err != nil {
		t.Fatalf("add weapon automation: %v", err)
	}
	event := sendTestEvent(t, core, contract.EventVisionWeapon, "cam_01", "", scenarioTime(0), map[string]any{"weapon_type": "knife"})

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionWeapon)
	decision := lastDecision(t, core)
	if decision.Priority < contract.PriorityHigh || core.app.state.SystemState().LastState == "idle" {
		t.Fatalf("weapon should produce high severity non-idle decision=%#v system=%#v", decision, core.app.state.SystemState())
	}
	actions := collectActionRequests(t, core)
	if len(actions) != 1 || actions[0].Action.Device != "siren" {
		t.Fatalf("weapon automation should dispatch critical action: %#v", actions)
	}
}

func TestCGEScenarioFallDetected(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionFall, "cam_02", "", scenarioTime(0), nil)

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionFall)
	decision := lastDecision(t, core)
	if decision.Priority < contract.PriorityHigh || core.app.state.SystemState().LastState == "idle" {
		t.Fatalf("fall should produce high severity non-idle decision=%#v system=%#v", decision, core.app.state.SystemState())
	}
}

func TestCGEScenarioCameraTamper(t *testing.T) {
	core := newTestCore(t)
	event := sendTestEvent(t, core, contract.EventVisionTamper, "cam_04", "", scenarioTime(0), map[string]any{"tamper": true})

	public := snapshot(t, core)
	assertEventVisible(t, public, event.ID, contract.EventVisionTamper)
	device := findDevice(public, "cam_04")
	decision := lastDecision(t, core)
	if device == nil || device["last_seen"] == nil || decision.Priority < contract.PriorityHigh || core.app.state.SystemState().LastState == "idle" {
		t.Fatalf("tamper should affect cam_04 and non-idle state device=%#v decision=%#v system=%#v", device, decision, core.app.state.SystemState())
	}
}

func TestCGEScenarioNormalResidentSequence(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_02", "alexis", scenarioTime(30*time.Second), nil)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(60*time.Second), nil)

	public := snapshot(t, core)
	resident := findResident(public, "alexis")
	if resident == nil || resident["node_id"] != "entry" || resident["state"] != "present" {
		t.Fatalf("alexis should finish back at entry: %#v", resident)
	}
	if core.app.state.SystemState().IntrusionActive {
		t.Fatalf("normal resident sequence should not trigger intrusion: %#v", core.app.state.SystemState())
	}
}

func TestCGEScenarioImpossibleSequence(t *testing.T) {
	core := newTestCore(t)
	first := sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	second := sendTestEvent(t, core, contract.EventVisionIdentity, "cam_05", "alexis", scenarioTime(500*time.Millisecond), nil)

	public := snapshot(t, core)
	assertEventVisible(t, public, first.ID, contract.EventVisionIdentity)
	assertEventVisible(t, public, second.ID, contract.EventVisionIdentity)
	decision := lastDecision(t, core)
	if decision.EventID != second.ID || decision.Type == "" {
		t.Fatalf("impossible sequence should remain observable: %#v", decision)
	}
}

func TestCGEScenarioValidationRequiredForDoubtfulChain(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	uncertain := sendTestEvent(t, core, contract.EventVisionUncertain, "cam_05", "", scenarioTime(500*time.Millisecond), map[string]any{
		"best_match": "alexis",
		"confidence": 0.45,
	})

	public := snapshot(t, core)
	eventView := assertEventVisible(t, public, uncertain.ID, contract.EventVisionUncertain)
	if eventView["validation_required"] != true {
		t.Fatalf("doubtful chain should be exposed as validation_required: %#v", eventView)
	}

	decision := lastDecision(t, core)
	if !decision.ValidationRequired || decision.ValidationReason == "" {
		t.Fatalf("decision should require validation: %#v", decision)
	}
	if decision.SequenceKey != "resident:alexis" || !decision.GraphUsed {
		t.Fatalf("decision should keep CGE sequence trace: %#v", decision)
	}
}

func TestCGEScenarioLinkedClipPropagatesToPublicSnapshot(t *testing.T) {
	core := newTestCore(t)
	clipEvent := sendTestEvent(t, core, contract.EventVisionUnknown, "cam_01", "", scenarioTime(0), map[string]any{
		"clip_id":   "clip_test_001",
		"clip_path": "/var/lib/synora/clips/cam_01/clip_test_001.mp4",
		"camera_id": "cam_01",
	})

	public := snapshot(t, core)
	clip := findClip(public, "clip_test_001")
	if clip == nil {
		t.Fatalf("linked clip missing from public snapshot: %#v", public.Clips)
	}
	if clip["event_id"] != clipEvent.ID || clip["camera_id"] != "cam_01" {
		t.Fatalf("clip should keep event and camera link: %#v", clip)
	}

	eventView := assertEventVisible(t, public, clipEvent.ID, contract.EventVisionUnknown)
	if eventView["clip_id"] != "clip_test_001" {
		t.Fatalf("event snapshot should keep clip_id link: %#v", eventView)
	}

	decision := lastDecision(t, core)
	if decision.ClipID != "clip_test_001" {
		t.Fatalf("decision should keep clip_id link: %#v", decision)
	}
}

func TestCGEScenarioMultipleUnknowns(t *testing.T) {
	core := newTestCore(t)
	first := sendTestEvent(t, core, contract.EventVisionUnknown, "cam_01", "", scenarioTime(0), nil)
	firstDecision := lastDecision(t, core)
	second := sendTestEvent(t, core, contract.EventVisionUnknown, "cam_02", "", scenarioTime(5*time.Second), nil)
	secondDecision := lastDecision(t, core)

	public := snapshot(t, core)
	assertEventVisible(t, public, first.ID, contract.EventVisionUnknown)
	assertEventVisible(t, public, second.ID, contract.EventVisionUnknown)
	t.Logf("multiple unknown scores: first=%0.3f second=%0.3f", firstDecision.EffectiveScore, secondDecision.EffectiveScore)
	if secondDecision.EventID != second.ID || secondDecision.Type == "" {
		t.Fatalf("second unknown should produce observable decision: %#v", secondDecision)
	}
}

func TestCGEScenarioNightAutomation(t *testing.T) {
	core := newTestCore(t)
	core.app.automation.Now = func() time.Time { return scenarioClock(23, 30) }
	addNightAutomation(t, core)

	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioClock(23, 30), nil)

	actions := collectActionRequests(t, core)
	if len(actions) != 1 || actions[0].Action.Device != "light_entree" {
		t.Fatalf("night automation should dispatch light action: %#v", actions)
	}
}

func TestCGEScenarioAutomationOutsideSchedule(t *testing.T) {
	core := newTestCore(t)
	core.app.automation.Now = func() time.Time { return scenarioClock(12, 0) }
	addNightAutomation(t, core)

	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioClock(12, 0), nil)

	if actions := collectActionRequests(t, core); len(actions) != 0 {
		t.Fatalf("daytime event should not dispatch night automation: %#v", actions)
	}
}

func TestCGEScenarioCleanSnapshotAfterSeveralEvents(t *testing.T) {
	core := newTestCore(t)
	sendTestEvent(t, core, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)
	sendTestEvent(t, core, contract.EventVisionUnknown, "cam_02", "", scenarioTime(5*time.Second), nil)
	sendTestEvent(t, core, contract.EventDeviceOffline, "cam_03", "", scenarioTime(10*time.Second), nil)

	public := snapshot(t, core)
	assertNoInternalSnapshotKeys(t, public)
	device := findDevice(public, "cam_04")
	if device == nil || device["last_seen"] != nil {
		t.Fatalf("untouched device should expose zero last_seen as null: %#v", device)
	}
}

func TestCGEScenarioStabilityAcrossIndependentCores(t *testing.T) {
	first := newTestCore(t)
	sendTestEvent(t, first, contract.EventVisionIdentity, "cam_01", "alexis", scenarioTime(0), nil)

	second := newTestCore(t)
	public := snapshot(t, second)
	resident := findResident(public, "alexis")
	if resident == nil {
		t.Fatalf("alexis should exist in independent core snapshot")
	}
	if resident["state"] == "present" || resident["last_seen"] != nil {
		t.Fatalf("state leaked from previous scenario into independent core: %#v", resident)
	}
	if len(public.Events) != 0 || len(collectActionRequests(t, second)) != 0 {
		t.Fatalf("events/actions leaked into independent core snapshot=%#v actions=%#v", public, collectActionRequests(t, second))
	}
}

func addNightAutomation(t *testing.T, core *scenarioCore) {
	t.Helper()
	if err := core.app.automation.Add(automation.Rule{
		ID:        "night-identity-light",
		EventType: contract.EventVisionIdentity,
		Schedule:  &automation.Schedule{Start: "23:00", End: "06:00"},
		Actions:   []contract.Action{{Device: "light_entree", Command: "on"}},
	}); err != nil {
		t.Fatalf("add night automation: %v", err)
	}
}

func scenarioTime(offset time.Duration) time.Time {
	return time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC).Add(offset)
}

func scenarioClock(hour int, minute int) time.Time {
	return time.Date(2026, 7, 6, hour, minute, 0, 0, time.UTC)
}
