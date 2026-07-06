package automation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLegacyAutomationScalarTriggerAndTimeCondition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "automations.yaml")
	data := []byte(`automations:
  - id: entrance_activity_night
    trigger: vision.identity
    conditions:
      - type: time
        from: "23:00"
        to: "06:00"
    actions:
      - device: light_entree
        command: on
`)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	rules, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	rule := rules[0]
	if rule.EventType != "vision.identity" {
		t.Fatalf("expected trigger to map to EventType, got %q", rule.EventType)
	}
	if rule.Schedule == nil || rule.Schedule.Start != "23:00" || rule.Schedule.End != "06:00" {
		t.Fatalf("expected time condition to map to schedule, got %#v", rule.Schedule)
	}
	if len(rule.Actions) != 1 || rule.Actions[0].Device != "light_entree" || rule.Actions[0].Command != "on" {
		t.Fatalf("unexpected actions: %#v", rule.Actions)
	}
}
