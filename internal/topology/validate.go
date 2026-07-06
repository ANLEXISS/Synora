package topology

import (
	"fmt"

)

func (t *Topology) Validate() error {

	for id, node := range t.Nodes {

		// rooms must have connections
		if node.Type == NodeRoom && len(node.Neighbors) == 0 {
			return fmt.Errorf("room %s has no connections", id)
		}

		for _, n := range node.Neighbors {

			// self connect
			if n.ID == id {
				return fmt.Errorf("node %s connects to itself", id)
			}

			// reciprocal connection
			if !hasNeighbor(n, node) {
				return fmt.Errorf("connection not reciprocal: %s -> %s", id, n.ID)
			}
		}
	}

	return nil
}

func hasNeighbor(a, target *Node) bool {

	for _, n := range a.Neighbors {
		if n.ID == target.ID {
			return true
		}
	}

	return false
}

func containsString(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

func (t *Topology) BuildGraph() {

	for _, node := range t.Nodes {

		node.Neighbors = nil

		for _, conn := range node.Connect {

			if target, ok := t.Nodes[conn]; ok {
				node.Neighbors = append(node.Neighbors, target)
			}
		}
	}
}
