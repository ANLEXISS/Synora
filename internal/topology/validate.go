package topology

import (
	"sort"
	"strings"

	"synora/pkg/contract"
)

func (t *Topology) Validate() error {
	if t == nil {
		return contract.NewAPIError(contract.ErrorValidationFailed, "topology is required")
	}
	roomCount := 0
	for key, node := range t.Nodes {
		if node == nil {
			return contract.NewAPIError(contract.ErrorValidationFailed, "topology node %q is null", key)
		}
		if strings.TrimSpace(node.ID) == "" || node.ID != key {
			return contract.NewAPIError(contract.ErrorValidationFailed, "topology node key %q does not match id %q", key, node.ID)
		}
		if !validNodeType(node.Type) {
			return contract.NewAPIError(contract.ErrorValidationFailed, "unsupported topology node type %q", node.Type)
		}
		if node.Type == NodeRoom {
			roomCount++
		}
		if node.Parent != nil {
			parent, ok := t.Nodes[node.Parent.ID]
			if !ok || parent != node.Parent || parent == node {
				return contract.NewAPIError(contract.ErrorValidationFailed, "invalid parent for topology node %q", node.ID)
			}
		}
		seen := map[string]struct{}{}
		for _, id := range node.Connect {
			id = strings.TrimSpace(id)
			if id == node.ID {
				return contract.NewAPIError(contract.ErrorValidationFailed, "topology node %q connects to itself", node.ID)
			}
			if _, ok := t.Nodes[id]; !ok {
				return contract.NewAPIError(contract.ErrorValidationFailed, "topology link %q -> %q references a missing node", node.ID, id)
			}
			if _, duplicate := seen[id]; duplicate {
				return contract.NewAPIError(contract.ErrorValidationFailed, "duplicate topology link %q -> %q", node.ID, id)
			}
			seen[id] = struct{}{}
		}
	}
	for _, node := range t.Nodes {
		if err := validateParentChain(node); err != nil {
			return err
		}
		if roomCount > 1 && node.Type == NodeRoom && len(node.Connect) == 0 {
			return contract.NewAPIError(contract.ErrorValidationFailed, "room %q has no topology links", node.ID)
		}
		for _, neighbor := range node.Neighbors {
			if neighbor == nil || !hasNeighbor(neighbor, node) {
				return contract.NewAPIError(contract.ErrorValidationFailed, "topology connection is not reciprocal for %q", node.ID)
			}
		}
	}
	if t.RootID != "" {
		if _, ok := t.Nodes[t.RootID]; !ok {
			return contract.NewAPIError(contract.ErrorValidationFailed, "topology root_id %q does not exist", t.RootID)
		}
	}
	if t.HouseID != "" {
		if _, ok := t.Nodes[t.HouseID]; !ok {
			return contract.NewAPIError(contract.ErrorValidationFailed, "topology house_id %q does not exist", t.HouseID)
		}
	}
	return nil
}

func validNodeType(value NodeType) bool {
	switch value {
	case NodeRoot, NodeHouse, NodeZone, NodeFloor, NodeRoom, NodeDevice, NodeUnlocated:
		return true
	default:
		return false
	}
}

func validateParentChain(node *Node) error {
	seen := map[string]struct{}{}
	for current := node; current != nil; current = current.Parent {
		if _, exists := seen[current.ID]; exists {
			return contract.NewAPIError(contract.ErrorValidationFailed, "topology parent cycle contains %q", current.ID)
		}
		seen[current.ID] = struct{}{}
	}
	return nil
}

func hasNeighbor(a, target *Node) bool {
	if a == nil || target == nil {
		return false
	}
	for _, node := range a.Neighbors {
		if node != nil && node.ID == target.ID {
			return true
		}
	}
	return false
}

func containsString(list []string, id string) bool {
	for _, value := range list {
		if value == id {
			return true
		}
	}
	return false
}

// BuildGraph canonicalizes links as undirected edges. Legacy topology files
// historically treated room connections as undirected even when only one side
// declared the link.
func (t *Topology) BuildGraph() {
	if t == nil {
		return
	}
	for _, node := range t.Nodes {
		node.Neighbors = nil
	}
	for _, node := range t.Nodes {
		for _, targetID := range append([]string(nil), node.Connect...) {
			target, ok := t.Nodes[targetID]
			if !ok || target == node {
				continue
			}
			link(node, target)
			if !containsString(target.Connect, node.ID) {
				target.Connect = append(target.Connect, node.ID)
			}
		}
	}
	for _, node := range t.Nodes {
		sort.Strings(node.Connect)
	}
}
