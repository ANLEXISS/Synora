package chains

import (
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
)

func TestObservationContextValidationAndClone(t *testing.T) {
	at := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	frame, err := cgecontext.ResolveFrame(cgecontext.ResolveInput{ObservationID: "context-observation", ObservedAt: at, NodeID: "room", Timezone: "UTC", Topology: cgecontext.TopologySnapshot{Revision: "r1", CapturedAt: at, Nodes: []cgecontext.Node{{ID: "room", Kind: cgecontext.NodeRoom}}}})
	if err != nil {
		t.Fatal(err)
	}
	observation := ObservationRef{ID: "context-observation", EventType: "vision.identity", Timestamp: at, NodeID: "room", Context: &frame}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := observation.Clone()
	if clone.Context == observation.Context {
		t.Fatal("observation clone aliases context")
	}
	clone.Context.NodeID = "changed"
	if observation.Context.NodeID == clone.Context.NodeID {
		t.Fatal("context clone aliases frame fields")
	}
	bad := observation
	bad.Context = &frame
	bad.Context.ObservationID = "other"
	if err := bad.Validate(); err == nil {
		t.Fatal("mismatched context identity accepted")
	}
}

func TestLegacyObservationWithoutContextRemainsValid(t *testing.T) {
	observation := ObservationRef{ID: "legacy", EventType: "vision.motion", Timestamp: time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
	if observation.Clone().Context != nil {
		t.Fatal("legacy clone gained context")
	}
}
