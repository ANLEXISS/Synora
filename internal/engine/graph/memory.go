package graph

import (
	"sync"
	"time"

	"synora/internal/engine/contracts"
)

const (
	SequenceBreakThreshold = 2 * time.Minute
)

type GraphMemory struct {
	graph *contracts.BehaviorGraph

	lastNodes map[string]*contracts.BehaviorNode

	mu sync.RWMutex
}

func NewGraphMemory() *GraphMemory {

	return &GraphMemory{
		graph: &contracts.BehaviorGraph{
			GraphID: "house_main",

			Roots: make(
				[]*contracts.BehaviorNode,
				0,
			),

			Version: 1,

			LastUpdate: time.Now(),
		},

		lastNodes: make(
			map[string]*contracts.BehaviorNode,
		),
	}
}

func SequenceKey(
	event *contracts.Event,
) string {

	if event == nil {
		return ":"
	}

	return string(
		event.SubjectType,
	) + ":" +
		event.SubjectID
}

func createNodeFromEvent(
	event *contracts.Event,
) *contracts.BehaviorNode {
	context := make(map[string]any)
	for key, value := range event.Metadata {
		context[key] = value
	}

	return &contracts.BehaviorNode{
		Event: event.Type,

		SubjectType: event.SubjectType,

		SubjectID: event.SubjectID,

		TargetType: event.TargetType,

		TargetID: event.TargetID,

		TopologyNode: event.TopologyNode,

		Weight: calculateWeight(1),

		Count: 1,

		LastSeen: event.Timestamp,

		Context: context,

		Children: make(
			[]*contracts.BehaviorNode,
			0,
		),
	}
}

func isSameNode(
	node *contracts.BehaviorNode,
	event *contracts.Event,
) bool {

	if node.Event != event.Type {
		return false
	}

	if node.SubjectType != event.SubjectType {
		return false
	}

	if node.TargetType != event.TargetType {
		return false
	}

	if node.TopologyNode != event.TopologyNode {
		return false
	}

	// identité connue
	if node.SubjectID != "" &&
		event.SubjectID != "" &&
		node.SubjectID != event.SubjectID {

		return false
	}

	if node.TargetID != "" &&
		event.TargetID != "" &&
		node.TargetID != event.TargetID {

		return false
	}

	return true
}

func updateAverageDelta(
	node *contracts.BehaviorNode,
	deltaMs int64,
) {

	if node.AvgDeltaMs == 0 {

		node.AvgDeltaMs =
			deltaMs

		return
	}

	node.AvgDeltaMs =
		(node.AvgDeltaMs + deltaMs) / 2
}

func (g *GraphMemory) LearnEvent(
	event *contracts.Event,
) {
	if event == nil {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	graph := g.graph

	if graph == nil {

		graph = &contracts.BehaviorGraph{
			GraphID: "house_main",

			Roots: make(
				[]*contracts.BehaviorNode,
				0,
			),

			Version: 1,

			LastUpdate: time.Now(),
		}

		g.graph = graph
	}

	sequenceKey :=
		SequenceKey(
			event,
		)

	previousNode, hasPrevious :=
		g.lastNodes[sequenceKey]

	// ---------------------------------------------------------------------
	// BREAK DETECTION
	// ---------------------------------------------------------------------

	if hasPrevious {

		delta :=
			event.Timestamp.Sub(
				previousNode.LastSeen,
			)

		if delta >
			SequenceBreakThreshold {

			delete(
				g.lastNodes,
				sequenceKey,
			)

			hasPrevious = false
			previousNode = nil
		}
	}

	// ---------------------------------------------------------------------
	// ROOT NODE
	// ---------------------------------------------------------------------

	if !hasPrevious {

		for _, root := range graph.Roots {

			if isSameNode(
				root,
				event,
			) {

				root.Count++

				root.LastSeen =
					event.Timestamp

				root.Weight =
					calculateWeight(
						int(root.Count),
					)

				g.lastNodes[sequenceKey] =
					root

				graph.Version++
				graph.LastUpdate =
					time.Now()

				return
			}
		}

		root :=
			createNodeFromEvent(
				event,
			)

		graph.Roots =
			append(
				graph.Roots,
				root,
			)

		g.lastNodes[sequenceKey] =
			root

		graph.Version++
		graph.LastUpdate =
			time.Now()

		return
	}

	// ---------------------------------------------------------------------
	// CHILD SEARCH
	// ---------------------------------------------------------------------

	for _, child := range previousNode.Children {

		if isSameNode(
			child,
			event,
		) {

			child.Count++

			delta :=
				event.Timestamp.Sub(
					previousNode.LastSeen,
				)

			updateAverageDelta(
				child,
				delta.Milliseconds(),
			)

			child.LastSeen =
				event.Timestamp

			child.Weight =
				calculateWeight(
					int(child.Count),
				)
			if child.Context == nil {
				child.Context = make(map[string]any)
			}
			child.Context["novel_transition"] = false

			g.lastNodes[sequenceKey] =
				child

			graph.Version++
			graph.LastUpdate =
				time.Now()

			return
		}
	}

	// ---------------------------------------------------------------------
	// CREATE NEW BRANCH
	// ---------------------------------------------------------------------

	delta :=
		event.Timestamp.Sub(
			previousNode.LastSeen,
		)

	node :=
		createNodeFromEvent(
			event,
		)

	node.AvgDeltaMs =
		delta.Milliseconds()
	if node.Context == nil {
		node.Context = make(map[string]any)
	}
	node.Context["novel_transition"] = true
	node.Context["transition_ms"] = delta.Milliseconds()
	node.Context["previous_topology_node"] = previousNode.TopologyNode

	previousNode.Children =
		append(
			previousNode.Children,
			node,
		)

	g.lastNodes[sequenceKey] =
		node

	graph.Version++
	graph.LastUpdate =
		time.Now()
}

func findMatchingRoot(
	graph *contracts.BehaviorGraph,
	event *contracts.Event,
) *contracts.BehaviorNode {

	for _, root := range graph.Roots {

		if root.Event == event.Type &&
			root.TopologyNode == event.TopologyNode {

			return root
		}
	}

	return nil
}

func (g *GraphMemory) GetGraph() *contracts.BehaviorGraph {

	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.graph
}

func (g *GraphMemory) Clear() {

	g.mu.Lock()
	defer g.mu.Unlock()

	g.graph = &contracts.BehaviorGraph{
		GraphID: "house_main",

		Roots: make(
			[]*contracts.BehaviorNode,
			0,
		),

		Version: 1,

		LastUpdate: time.Now(),
	}

	g.lastNodes =
		make(
			map[string]*contracts.BehaviorNode,
		)
}
