package context

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type SchemaVersion uint16

const (
	SchemaVersionV1      SchemaVersion = 1
	SchemaVersionCurrent               = SchemaVersionV1
)

func (v SchemaVersion) String() string {
	if v == SchemaVersionV1 {
		return "context-v1"
	}
	return fmt.Sprintf("context-v%d", v)
}

type NodeKind string

const (
	NodeUnknown  NodeKind = "unknown"
	NodeRoom     NodeKind = "room"
	NodeCorridor NodeKind = "corridor"
	NodeEntrance NodeKind = "entrance"
	NodeExit     NodeKind = "exit"
	NodeStairs   NodeKind = "stairs"
	NodeGarage   NodeKind = "garage"
	NodeGarden   NodeKind = "garden"
	NodeExterior NodeKind = "exterior"
)

type TraversalKind string

const (
	TraversalUnknown  TraversalKind = "unknown"
	TraversalWalk     TraversalKind = "walk"
	TraversalDoor     TraversalKind = "door"
	TraversalStairs   TraversalKind = "stairs"
	TraversalExterior TraversalKind = "exterior_transition"
)

type Node struct {
	ID         string   `json:"id"`
	ParentID   string   `json:"parent_id,omitempty"`
	ZoneID     string   `json:"zone_id,omitempty"`
	Kind       NodeKind `json:"kind"`
	EntryPoint bool     `json:"entry_point,omitempty"`
	Exterior   bool     `json:"exterior,omitempty"`
}

type Edge struct {
	From          string        `json:"from"`
	To            string        `json:"to"`
	Directed      bool          `json:"directed,omitempty"`
	TraversalKind TraversalKind `json:"traversal_kind"`
}

type TopologySnapshot struct {
	Revision   string    `json:"revision"`
	CapturedAt time.Time `json:"captured_at"`
	Nodes      []Node    `json:"nodes"`
	Edges      []Edge    `json:"edges,omitempty"`
}

const maxContextIdentifier = 256

func validNodeKind(value NodeKind) bool {
	switch value {
	case NodeUnknown, NodeRoom, NodeCorridor, NodeEntrance, NodeExit, NodeStairs, NodeGarage, NodeGarden, NodeExterior:
		return true
	}
	return false
}
func validTraversal(value TraversalKind) bool {
	switch value {
	case TraversalUnknown, TraversalWalk, TraversalDoor, TraversalStairs, TraversalExterior:
		return true
	}
	return false
}
func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= maxContextIdentifier && strings.TrimSpace(value) == value
}

func (t TopologySnapshot) Validate() error {
	if strings.TrimSpace(t.Revision) == "" || len(t.Revision) > maxContextIdentifier {
		return fmt.Errorf("%w: revision", ErrInvalidTopology)
	}
	if t.CapturedAt.IsZero() {
		return fmt.Errorf("%w: captured_at", ErrInvalidTopology)
	}
	byID := make(map[string]Node, len(t.Nodes))
	lastID := ""
	for _, node := range t.Nodes {
		if !validIdentifier(node.ID) {
			return fmt.Errorf("%w: node id", ErrInvalidTopology)
		}
		if _, ok := byID[node.ID]; ok {
			return fmt.Errorf("%w: duplicate node %q", ErrInvalidTopology, node.ID)
		}
		if lastID != "" && node.ID <= lastID {
			return fmt.Errorf("%w: nodes are not in canonical order", ErrInvalidTopology)
		}
		lastID = node.ID
		if !validNodeKind(node.Kind) || (node.ParentID != "" && !validIdentifier(node.ParentID)) || (node.ZoneID != "" && !validIdentifier(node.ZoneID)) {
			return fmt.Errorf("%w: node %q", ErrInvalidTopology, node.ID)
		}
		byID[node.ID] = node
	}
	for _, node := range t.Nodes {
		if node.ParentID != "" {
			if _, ok := byID[node.ParentID]; !ok {
				return fmt.Errorf("%w: parent %q", ErrInvalidTopology, node.ParentID)
			}
		}
	}
	for _, node := range t.Nodes {
		seen := map[string]bool{}
		for current := node; current.ParentID != ""; {
			if seen[current.ID] {
				return fmt.Errorf("%w: parent cycle at %q", ErrInvalidTopology, node.ID)
			}
			seen[current.ID] = true
			parent, ok := byID[current.ParentID]
			if !ok {
				break
			}
			current = parent
		}
	}
	seenEdges := make(map[string]struct{}, len(t.Edges))
	for _, edge := range t.Edges {
		if !validIdentifier(edge.From) || !validIdentifier(edge.To) || edge.From == edge.To || !validTraversal(edge.TraversalKind) {
			return fmt.Errorf("%w: edge", ErrInvalidTopology)
		}
		if _, ok := byID[edge.From]; !ok {
			return fmt.Errorf("%w: edge from %q", ErrInvalidTopology, edge.From)
		}
		if _, ok := byID[edge.To]; !ok {
			return fmt.Errorf("%w: edge to %q", ErrInvalidTopology, edge.To)
		}
		key := fmt.Sprintf("%s\x00%s\x00%t\x00%s", edge.From, edge.To, edge.Directed, edge.TraversalKind)
		if _, ok := seenEdges[key]; ok {
			return fmt.Errorf("%w: duplicate edge", ErrInvalidTopology)
		}
		seenEdges[key] = struct{}{}
	}
	for i := 1; i < len(t.Edges); i++ {
		if edgeKey(t.Edges[i-1]) >= edgeKey(t.Edges[i]) {
			return fmt.Errorf("%w: edges are not in canonical order", ErrInvalidTopology)
		}
	}
	return nil
}

func edgeKey(edge Edge) string {
	return fmt.Sprintf("%s\x00%s\x00%t\x00%s", edge.From, edge.To, edge.Directed, edge.TraversalKind)
}

func (t TopologySnapshot) Clone() TopologySnapshot {
	t.Nodes = append([]Node(nil), t.Nodes...)
	t.Edges = append([]Edge(nil), t.Edges...)
	return t
}

func CanonicalTopology(t TopologySnapshot) TopologySnapshot {
	t = t.Clone()
	sort.Slice(t.Nodes, func(i, j int) bool { return t.Nodes[i].ID < t.Nodes[j].ID })
	sort.Slice(t.Edges, func(i, j int) bool { return edgeKey(t.Edges[i]) < edgeKey(t.Edges[j]) })
	return t
}
