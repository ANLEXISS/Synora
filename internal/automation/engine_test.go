package automation

import (
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestEvaluateRequestsIgnoresDisabledAutomation(t *testing.T) {
	engine := NewEngine(t.TempDir() + "/automations.yaml")
	mustAddRule(t, engine, Rule{
		ID:        "disabled",
		Enabled:   false,
		EventType: contract.EventVisionMotion,
		Actions:   []AutomationAction{testAction("a1")},
	})

	if got := engine.EvaluateRequests(testEvent(), testDecision()); len(got) != 0 {
		t.Fatalf("disabled automation should not trigger: %#v", got)
	}
}

func TestEvaluateRequestsConditionLogicAllAndAny(t *testing.T) {
	all := NewEngine(t.TempDir() + "/all.yaml")
	mustAddRule(t, all, Rule{
		ID:             "all",
		Enabled:        true,
		EventType:      contract.EventVisionMotion,
		ConditionLogic: "all",
		Conditions: []Condition{
			{Field: "node", Op: "==", Value: "entry"},
			{Field: "score", Op: ">=", Value: 0.8},
		},
		Actions: []AutomationAction{testAction("a1")},
	})
	if got := all.EvaluateRequests(testEvent(), testDecision()); len(got) != 1 {
		t.Fatalf("all conditions should match, got %#v", got)
	}

	any := NewEngine(t.TempDir() + "/any.yaml")
	mustAddRule(t, any, Rule{
		ID:             "any",
		Enabled:        true,
		EventType:      contract.EventVisionMotion,
		ConditionLogic: "any",
		Conditions: []Condition{
			{Field: "node", Op: "==", Value: "garage"},
			{Field: "payload.tags", Op: "contains", Value: "person"},
		},
		Actions: []AutomationAction{testAction("a1")},
	})
	if got := any.EvaluateRequests(testEvent(), testDecision()); len(got) != 1 {
		t.Fatalf("any condition should match, got %#v", got)
	}
}

func TestEvaluateRequestsConditionOperators(t *testing.T) {
	tests := []Condition{
		{Field: "node", Op: "==", Value: "entry"},
		{Field: "node", Op: "!=", Value: "garage"},
		{Field: "score", Op: ">", Value: 0.7},
		{Field: "score", Op: ">=", Value: 0.8},
		{Field: "score", Op: "<", Value: 0.9},
		{Field: "score", Op: "<=", Value: 0.8},
		{Field: "payload.tags", Op: "contains", Value: "person"},
		{Field: "clip_id", Op: "exists"},
		{Field: "payload.missing", Op: "not_exists"},
	}

	for _, condition := range tests {
		engine := NewEngine(t.TempDir() + "/operators.yaml")
		mustAddRule(t, engine, Rule{
			ID:         condition.Op + "-" + condition.Field,
			Enabled:    true,
			EventType:  contract.EventVisionMotion,
			Conditions: []Condition{condition},
			Actions:    []AutomationAction{testAction("a1")},
		})
		if got := engine.EvaluateRequests(testEvent(), testDecision()); len(got) != 1 {
			t.Fatalf("condition %#v should match, got %#v", condition, got)
		}
	}
}

func TestEvaluateRequestsManualRiskWithoutConditionMatches(t *testing.T) {
	engine := NewEngine(t.TempDir() + "/manual-risk.yaml")
	mustAddRule(t, engine, Rule{
		ID:        "manual-any-danger",
		Enabled:   true,
		EventType: contract.EventManualRisk,
		Actions:   []AutomationAction{testAction("push")},
	})

	if got := engine.EvaluateRequests(manualRiskEvent("high"), manualRiskDecision("high")); len(got) != 1 {
		t.Fatalf("manual risk without condition should match, got %#v", got)
	}
}

func TestEvaluateRequestsManualRiskDangerLevelConditions(t *testing.T) {
	tests := []struct {
		name      string
		field     string
		op        string
		expected  string
		level     string
		wantMatch bool
	}{
		{name: "high above medium", field: "danger.level", op: ">", expected: "medium", level: "high", wantMatch: true},
		{name: "low not above medium", field: "danger.level", op: ">", expected: "medium", level: "low", wantMatch: false},
		{name: "high at least high", field: "danger.level", op: ">=", expected: "high", level: "high", wantMatch: true},
		{name: "critical at least high", field: "danger.level", op: ">=", expected: "high", level: "critical", wantMatch: true},
		{name: "critical is not high", field: "danger.level", op: "==", expected: "high", level: "critical", wantMatch: false},
		{name: "danger level alias", field: "danger_level", op: ">", expected: "medium", level: "high", wantMatch: true},
		{name: "system state alias", field: "system.state", op: "==", expected: "suspicious", level: "high", wantMatch: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := NewEngine(t.TempDir() + "/condition.yaml")
			mustAddRule(t, engine, Rule{
				ID:        "manual-" + test.name,
				Enabled:   true,
				EventType: contract.EventManualRisk,
				Conditions: []Condition{{
					Field: test.field, Op: test.op, Value: test.expected,
				}},
				Actions: []AutomationAction{testAction("push")},
			})

			got := engine.EvaluateRequests(manualRiskEvent(test.level), manualRiskDecision(test.level))
			if matched := len(got) == 1; matched != test.wantMatch {
				t.Fatalf("condition field=%s op=%s expected=%s level=%s matched=%t, want %t", test.field, test.op, test.expected, test.level, matched, test.wantMatch)
			}
		})
	}
}

func TestCompareDangerLevelUsesBusinessOrder(t *testing.T) {
	for _, test := range []struct {
		actual, op, expected string
		want                 bool
	}{
		{actual: "none", op: "<", expected: "low", want: true},
		{actual: "medium", op: ">", expected: "high", want: false},
		{actual: "high", op: "!=", expected: "critical", want: true},
		{actual: "critical", op: ">", expected: "high", want: true},
		{actual: "high", op: "<=", expected: "high", want: true},
		{actual: "medium_high", op: ">", expected: "medium", want: true},
		{actual: "medium_high", op: ">=", expected: "high", want: false},
		{actual: "high", op: ">", expected: "medium_high", want: true},
		{actual: "medium_high", op: "==", expected: "medium_high", want: true},
	} {
		if got := compareDangerLevel(test.op, test.actual, true, test.expected); got != test.want {
			t.Fatalf("compareDangerLevel(%q, %q, %q)=%t, want %t", test.actual, test.op, test.expected, got, test.want)
		}
	}
}

func TestEvaluateRequestsSecurityContextAliases(t *testing.T) {
	for _, field := range []string{"security.mode", "security_mode", "mode"} {
		engine := NewEngine(t.TempDir() + "/security.yaml")
		mustAddRule(t, engine, Rule{ID: field, Enabled: true, EventType: contract.EventManualRisk, Conditions: []Condition{{Field: field, Op: "==", Value: "high_security"}}, Actions: []AutomationAction{testAction("a1")}})
		event := manualRiskEvent("high")
		event.Payload["security"] = map[string]any{"mode": "high_security", "armed": true}
		if got := engine.EvaluateRequests(event, manualRiskDecision("high")); len(got) != 1 {
			t.Fatalf("security mode field %q did not match: %#v", field, got)
		}
	}
	for _, test := range []struct {
		field string
		value any
	}{
		{field: "security.armed", value: true}, {field: "occupancy.expected", value: "empty"}, {field: "manual_risk.active", value: true},
	} {
		engine := NewEngine(t.TempDir() + "/security-alias.yaml")
		mustAddRule(t, engine, Rule{ID: test.field, Enabled: true, EventType: contract.EventManualRisk, Conditions: []Condition{{Field: test.field, Op: "==", Value: test.value}}, Actions: []AutomationAction{testAction("a1")}})
		event := manualRiskEvent("high")
		event.Payload["security"] = map[string]any{"armed": true}
		event.Payload["occupancy"] = map[string]any{"expected": "empty"}
		event.Payload["manual_risk"] = map[string]any{"active": true}
		if got := engine.EvaluateRequests(event, manualRiskDecision("high")); len(got) != 1 {
			t.Fatalf("security field %q did not match: %#v", test.field, got)
		}
	}
}

func TestEvaluateRequestsHighSecurityManualRiskContext(t *testing.T) {
	engine := NewEngine(t.TempDir() + "/high-security.yaml")
	mustAddRule(t, engine, Rule{
		ID: "high-security-risk", Enabled: true, EventType: contract.EventManualRisk,
		Conditions: []Condition{
			{Field: "security.mode", Op: "==", Value: "high_security"},
			{Field: "manual_risk.active", Op: "==", Value: true},
			{Field: "danger.level", Op: ">=", Value: "high"},
		}, Actions: []AutomationAction{testAction("a1")},
	})
	event := manualRiskEvent("high")
	event.Payload["security"] = map[string]any{"mode": "high_security", "armed": true}
	event.Payload["manual_risk"] = map[string]any{"active": true}
	if got := engine.EvaluateRequests(event, manualRiskDecision("high")); len(got) != 1 {
		t.Fatalf("high security context should match: %#v", got)
	}
	event.Payload["security"] = map[string]any{"mode": "home", "armed": false}
	if got := engine.EvaluateRequests(event, manualRiskDecision("high")); len(got) != 0 {
		t.Fatalf("home must not match high_security rule: %#v", got)
	}
}

func TestEvaluateRequestsCooldownBlocksSecondTrigger(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(t.TempDir() + "/cooldown.yaml")
	engine.Now = func() time.Time { return now }
	mustAddRule(t, engine, Rule{
		ID:         "cooldown",
		Enabled:    true,
		EventType:  contract.EventVisionMotion,
		CooldownMs: 1000,
		Actions:    []AutomationAction{testAction("a1")},
	})

	if got := engine.EvaluateRequests(testEvent(), testDecision()); len(got) != 1 {
		t.Fatalf("first trigger should dispatch one request, got %#v", got)
	}
	if got := engine.EvaluateRequests(testEvent(), testDecision()); len(got) != 0 {
		t.Fatalf("cooldown should block second trigger, got %#v", got)
	}
}

func TestEvaluateRequestsMultipleActionsAndContext(t *testing.T) {
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	engine := NewEngine(t.TempDir() + "/multi.yaml")
	engine.Now = func() time.Time { return now }
	mustAddRule(t, engine, Rule{
		ID:        "multi",
		Enabled:   true,
		EventType: contract.EventVisionMotion,
		Actions: []AutomationAction{
			{ID: "second", Enabled: true, Type: "mqtt.publish", Target: "topic/b", Order: 2, Data: map[string]any{"payload": "b"}},
			{ID: "first", Enabled: true, Type: "device.command", Target: "light-1", Order: 1, Data: map[string]any{"command": "on"}, TimeoutMs: 250, RetryCount: 2},
		},
	})

	got := engine.EvaluateRequests(testEvent(), testDecision())
	if len(got) != 2 {
		t.Fatalf("expected two action requests, got %#v", got)
	}
	if got[0].ActionID != "first" || got[1].ActionID != "second" {
		t.Fatalf("actions should be ordered by order: %#v", got)
	}
	if got[0].AutomationID != "multi" ||
		got[0].SourceEventID != "evt-1" ||
		got[0].DecisionID != "dec-1" ||
		got[0].ClipID != "clip-decision" ||
		got[0].NodeID != "entry" ||
		got[0].DeviceID != "cam-1" ||
		got[0].TimeoutMs != 250 ||
		got[0].RetryCount != 2 {
		t.Fatalf("context not propagated: %#v", got[0])
	}
}

func TestEvaluateRequestsPropagatesSimulationMetadata(t *testing.T) {
	engine := NewEngine(t.TempDir() + "/sim.yaml")
	mustAddRule(t, engine, Rule{
		ID:        "sim",
		Enabled:   true,
		EventType: contract.EventVisionMotion,
		Actions:   []AutomationAction{testAction("a1")},
	})
	event := testEvent()
	event.Payload["metadata"] = map[string]any{
		"simulated":   true,
		"test_run_id": "sim-1",
		"dry_run":     true,
	}

	got := engine.EvaluateRequests(event, testDecision())
	if len(got) != 1 {
		t.Fatalf("expected one request, got %#v", got)
	}
	if got[0].Metadata["simulated"] != true || got[0].Metadata["test_run_id"] != "sim-1" || got[0].Metadata["dry_run"] != true {
		t.Fatalf("simulation metadata should propagate: %#v", got[0].Metadata)
	}
}

func mustAddRule(t *testing.T, engine *Engine, rule Rule) {
	t.Helper()
	if err := engine.Add(rule); err != nil {
		t.Fatalf("add rule: %v", err)
	}
}

func testAction(id string) AutomationAction {
	return AutomationAction{
		ID:      id,
		Enabled: true,
		Type:    "device.command",
		Target:  "light-1",
		Data:    map[string]any{"command": "on"},
	}
}

func testEvent() *contract.Event {
	return &contract.Event{
		ID:       "evt-1",
		Type:     contract.EventVisionMotion,
		DeviceID: "cam-1",
		NodeID:   "entry",
		ClipID:   "clip-event",
		Payload: map[string]any{
			"tags": []any{"motion", "person"},
		},
	}
}

func testDecision() *contract.Decision {
	return &contract.Decision{
		ID:             "dec-1",
		Type:           "motion.present",
		EventID:        "evt-1",
		State:          "active",
		NodeID:         "entry",
		ClipID:         "clip-decision",
		EffectiveScore: 0.8,
	}
}

func manualRiskEvent(level string) *contract.Event {
	return &contract.Event{
		ID:      "manual-" + level,
		Type:    contract.EventManualRisk,
		Source:  "admin",
		Payload: map[string]any{"danger_level": level, "danger_source": "manual"},
	}
}

func manualRiskDecision(level string) *contract.Decision {
	state := "activity"
	score := 0.5
	if level == "high" {
		state, score = "suspicious", 0.75
	}
	if level == "critical" {
		state, score = "intrusion", 0.95
	}
	return &contract.Decision{
		Type:           "engine.decision",
		State:          state,
		DangerLevel:    level,
		DangerScore:    score,
		DangerSource:   "manual",
		EffectiveScore: score,
	}
}
