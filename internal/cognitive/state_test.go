package cognitive

import (
	"context"
	"testing"
	"time"
)

func TestStateFrameNormalizationAndEncodingAreDeterministic(t *testing.T) {
	first := StateFrame{
		SchemaVersion: StateFrameSchemaVersion,
		Revision:      7,
		CapturedAt:    time.Unix(10, 0).UTC(),
		Devices:       []StateDevice{{ID: "z"}, {ID: "a"}},
		Topology:      []StateNode{{ID: "room", ConnectedIDs: []string{"z", "a"}}},
		RecentEvents:  []StateEvent{{ID: "b", Timestamp: time.Unix(2, 0)}, {ID: "a", Timestamp: time.Unix(1, 0)}},
	}
	second := first
	second.Devices = []StateDevice{{ID: "a"}, {ID: "z"}}
	second.RecentEvents = []StateEvent{{ID: "a", Timestamp: time.Unix(1, 0)}, {ID: "b", Timestamp: time.Unix(2, 0)}}
	second.Topology = []StateNode{{ID: "room", ConnectedIDs: []string{"a", "z"}}}
	encoder := DeterministicStateEncoder{}
	left, err := encoder.Encode(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := encoder.Encode(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if string(left.FrameChecksum) != string(right.FrameChecksum) || len(left.Values) != len(right.Values) {
		t.Fatalf("encoding is not deterministic: %#v %#v", left, right)
	}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestActionSelectionMustReferenceEnabledCatalog(t *testing.T) {
	catalog := ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion, Actions: []ActionDescriptor{{ID: "alert", Capability: "alert", RiskClass: "high", Enabled: true}}}
	output := CognitiveOutput{RequestID: "request", TaskID: "task", AdvisoryOnly: true, SelectedActionIDs: []string{"unknown"}}
	if err := output.ValidateActionSelection(catalog); err == nil {
		t.Fatal("expected unknown action selection to be rejected")
	}
}
