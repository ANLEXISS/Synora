package graph

import (
	"sort"

	"synora/internal/engine/contracts"
)

func (g *GraphMemory) GetNextProbableNodes(
	sequenceKey string,
) []*contracts.BehaviorNode {

	g.mu.RLock()
	defer g.mu.RUnlock()

	lastNode, ok :=
		g.lastNodes[sequenceKey]

	// ---------------------------------------------------------------------
	// ROOT FALLBACK
	// ---------------------------------------------------------------------

	if !ok {

		if g.graph == nil {
			return nil
		}

		if len(g.graph.Roots) == 0 {
			return nil
		}

		nodes :=
			make(
				[]*contracts.BehaviorNode,
				len(g.graph.Roots),
			)

		copy(
			nodes,
			g.graph.Roots,
		)

		sort.Slice(
			nodes,
			func(i, j int) bool {

				return nodes[i].Weight >
					nodes[j].Weight
			},
		)

		return nodes
	}

	// ---------------------------------------------------------------------
	// CHILDREN
	// ---------------------------------------------------------------------

	if len(lastNode.Children) == 0 {
		return nil
	}

	nodes :=
		make(
			[]*contracts.BehaviorNode,
			len(lastNode.Children),
		)

	copy(
		nodes,
		lastNode.Children,
	)

	sort.Slice(
		nodes,
		func(i, j int) bool {

			return nodes[i].Weight >
				nodes[j].Weight
		},
	)

	return nodes
}

func (g *GraphMemory) GetMostProbableNextNode(
	sequenceKey string,
) (
	*contracts.BehaviorNode,
	bool,
) {

	nodes :=
		g.GetNextProbableNodes(
			sequenceKey,
		)

	if len(nodes) == 0 {

		return nil, false
	}

	return nodes[0], true
}

func (g *GraphMemory) GetLastNode(
	sequenceKey string,
) (
	*contracts.BehaviorNode,
	bool,
) {

	g.mu.RLock()
	defer g.mu.RUnlock()

	node, ok :=
		g.lastNodes[sequenceKey]

	return node, ok
}

func (g *GraphMemory) ResetLastNode(
	sequenceKey string,
) {

	g.mu.Lock()
	defer g.mu.Unlock()

	delete(
		g.lastNodes,
		sequenceKey,
	)
}
