package automation

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"synora/pkg/contract"
)

func TestAutomationCRUDIsTransactionalAndRejectsDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automations.yaml")
	engine := NewEngine(path)
	created, err := engine.Create(Rule{
		ID: "lights_on", Enabled: true,
		Trigger: Trigger{EventType: contract.EventVisionMotion, NodeID: "entry"},
		Actions: []AutomationAction{{ID: "light", Enabled: true, Type: "device.command", Target: "light_1", Data: map[string]any{"command": "on"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Trigger.NodeID != "entry" {
		t.Fatalf("created=%#v", created)
	}
	if _, err := engine.Create(created); contract.APIErrorCode(err) != contract.ErrorDuplicateID {
		t.Fatalf("duplicate error=%v", err)
	}
	name := "Lights on updated"
	disabled := false
	updated, err := engine.Patch(created.ID, contract.AutomationPatch{Name: &name, Enabled: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.Enabled {
		t.Fatalf("updated=%#v", updated)
	}
	deleted, err := engine.SoftDelete(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.DeletedAt == nil || deleted.Status != contract.AutomationStatusDisabled {
		t.Fatalf("deleted=%#v", deleted)
	}
	backups, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "automations.*.yaml"))
	if len(backups) < 2 {
		t.Fatalf("expected backups for durable patches, got %v", backups)
	}
}

func TestAutomationCriticalActionDefaultsAndSafety(t *testing.T) {
	var draft Rule
	if err := json.Unmarshal([]byte(`{
		"id":"door_draft","trigger":{"event_type":"security.intrusion"},
		"actions":[{"type":"door.unlock","enabled":true}]
	}`), &draft); err != nil {
		t.Fatal(err)
	}
	if draft.Enabled {
		t.Fatalf("critical automation should default disabled: %#v", draft)
	}
	engine := NewEngine(filepath.Join(t.TempDir(), "automations.yaml"))
	created, err := engine.Create(draft)
	if err != nil {
		t.Fatal(err)
	}
	if created.Enabled || !created.RequiresValidation {
		t.Fatalf("critical draft=%#v", created)
	}

	enabled := true
	if _, err := engine.Patch(created.ID, contract.AutomationPatch{Enabled: &enabled}); contract.APIErrorCode(err) != contract.ErrorUnsafeAutomation {
		t.Fatalf("unapproved door unlock error=%v code=%s", err, contract.APIErrorCode(err))
	}
	if current, _ := engine.Get(created.ID); current.Enabled {
		t.Fatal("failed unsafe patch changed live automation")
	}

	approved := contract.AutomationStatusApproved
	updated, err := engine.Patch(created.ID, contract.AutomationPatch{Status: &approved, Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Enabled || !updated.RequiresValidation {
		t.Fatalf("approved critical automation=%#v", updated)
	}

	_, err = engine.Create(Rule{
		ID: "emergency", Trigger: Trigger{EventType: "security.intrusion"},
		Actions: []AutomationAction{{Type: "emergency_call", Enabled: true}},
	})
	if contract.APIErrorCode(err) != contract.ErrorForbiddenAction {
		t.Fatalf("emergency action error=%v code=%s", err, contract.APIErrorCode(err))
	}
}

func TestAutomationTopologyDetachUsesOneCommittedReplacement(t *testing.T) {
	engine := NewEngine(filepath.Join(t.TempDir(), "automations.yaml"))
	for _, rule := range []Rule{
		{ID: "missing", Enabled: true, Trigger: Trigger{EventType: "motion", NodeID: "removed"}, Actions: []AutomationAction{{Type: "notify", Enabled: true}}},
		{ID: "kept", Enabled: true, Trigger: Trigger{EventType: "motion", NodeID: "entry"}, Actions: []AutomationAction{{Type: "notify", Enabled: true}}},
	} {
		if _, err := engine.Create(rule); err != nil {
			t.Fatal(err)
		}
	}
	items, err := engine.DisableMissingTopologyNodes(map[string]bool{"entry": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%#v", items)
	}
	missing, _ := engine.Get("missing")
	kept, _ := engine.Get("kept")
	if missing.Enabled || missing.ConfigError != topologyNodeMissing || !kept.Enabled || kept.ConfigError != "" {
		t.Fatalf("missing=%#v kept=%#v", missing, kept)
	}
}

func TestAutomationWriteFailureRollsBackMemory(t *testing.T) {
	engine := NewEngine(t.TempDir())
	_, err := engine.Create(Rule{
		ID: "rollback", Enabled: true, Trigger: Trigger{EventType: "motion"},
		Actions: []AutomationAction{{Type: "notify", Enabled: true}},
	})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if _, exists := engine.Get("rollback"); exists {
		t.Fatal("failed write changed live automation store")
	}
}
