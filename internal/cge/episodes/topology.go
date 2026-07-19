package episodes

import (
	"fmt"
	"sort"

	cgecontext "synora/internal/cge/context"
)

type TopologyRelationship string

const (
	TopologySame        TopologyRelationship = "same"
	TopologyAdjacent    TopologyRelationship = "adjacent"
	TopologyReachable   TopologyRelationship = "reachable"
	TopologyUnreachable TopologyRelationship = "unreachable"
	TopologyUnknown     TopologyRelationship = "unknown"
)

type TopologyView interface {
	Relationship(fromNodeID, toNodeID string) TopologyRelationship
}

type MapTopology struct {
	Relationships map[string]TopologyRelationship
}

func (m MapTopology) Relationship(fromNodeID, toNodeID string) TopologyRelationship {
	if fromNodeID == "" || toNodeID == "" {
		return TopologyUnknown
	}
	if fromNodeID == toNodeID {
		return TopologySame
	}
	if value, ok := m.Relationships[fromNodeID+"\x00"+toNodeID]; ok {
		return value
	}
	if value, ok := m.Relationships[toNodeID+"\x00"+fromNodeID]; ok {
		return value
	}
	return TopologyUnknown
}

func (m MapTopology) Validate() error {
	keys := make([]string, 0, len(m.Relationships))
	for key, value := range m.Relationships {
		if key == "" || (value != TopologySame && value != TopologyAdjacent && value != TopologyReachable && value != TopologyUnreachable && value != TopologyUnknown) {
			return ErrInvalidTopology
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return nil
}

// ContextTopology adapts the existing detached CGE topology model without
// importing any runtime or security package.
type ContextTopology struct{ snapshot cgecontext.TopologySnapshot }

func NewContextTopology(snapshot cgecontext.TopologySnapshot) (ContextTopology, error) {
	if err := snapshot.Validate(); err != nil {
		return ContextTopology{}, fmt.Errorf("%w: %v", ErrInvalidTopology, err)
	}
	return ContextTopology{snapshot: snapshot.Clone()}, nil
}

func (t ContextTopology) Relationship(fromNodeID, toNodeID string) TopologyRelationship {
	if fromNodeID == "" || toNodeID == "" {
		return TopologyUnknown
	}
	if fromNodeID == toNodeID {
		return TopologySame
	}
	distance, status, err := cgecontext.ShortestPath(t.snapshot, fromNodeID, toNodeID)
	if err != nil {
		return TopologyUnknown
	}
	if status == cgecontext.DistanceUnreachable {
		return TopologyUnreachable
	}
	if status != cgecontext.DistanceReachable {
		return TopologyUnknown
	}
	if distance == 1 {
		return TopologyAdjacent
	}
	return TopologyReachable
}
