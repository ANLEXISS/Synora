package graph

import (
	"testing"
	"time"

	"synora/internal/engine/contracts"
)

func TestSequenceKeyUsedForLastNodeLookup(t *testing.T) {
	memory := NewGraphMemory()
	event := &contracts.Event{
		ID:           "evt_1",
		Type:         "vision.id.seen",
		SubjectType:  contracts.SubjectResident,
		SubjectID:    "alexis",
		TargetType:   contracts.SubjectDevice,
		TargetID:     "cam_01",
		TopologyNode: "zoneA.L0.entree",
		Timestamp:    time.Now().UTC(),
	}

	memory.LearnEvent(event)

	if _, ok := memory.GetLastNode(SequenceKey(event)); !ok {
		t.Fatal("last node should be addressable with the same sequence key used by LearnEvent")
	}
}
