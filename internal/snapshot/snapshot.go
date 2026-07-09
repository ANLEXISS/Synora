package snapshot

import (
	"sort"
	"sync"
	"time"

	"synora/internal/automation"
	"synora/internal/device"
	"synora/internal/engine"
	"synora/internal/event"
	"synora/internal/state"
	"synora/internal/topology"
	"synora/pkg/contract"
)

type Metrics interface {
	Snapshot(*state.Store) map[string]any
}

type Builder struct {
	Mu         *sync.RWMutex
	State      *state.Store
	Devices    *device.Registry
	Topology   *topology.Topology
	Residents  map[string]*topology.Resident
	Automation *automation.Engine
	Events     *event.Store
	Metrics    Metrics
}

func (b *Builder) SetTopology(value *topology.Topology) {
	if b.Mu != nil {
		b.Mu.Lock()
		defer b.Mu.Unlock()
	}
	b.Topology = value
}

func (b *Builder) LegacySnapshot() *contract.Snapshot {
	return &contract.Snapshot{
		Structure: contract.StructureSnapshot{
			Topology: b.TopologyTreeViews(),
			Devices:  b.DeviceViews(),
		},
		Residents: contract.ResidentsSnapshot{
			Residents: b.ResidentViews(),
		},
	}
}

func (b *Builder) CoreState() map[string]any {
	return map[string]any{
		"nodes":       b.TopologyTreeViews(),
		"devices":     b.DeviceViews(),
		"device":      b.DeviceViews(),
		"residents":   b.ResidentViews(),
		"automations": b.automationList(),
		"automation":  b.automationList(),
		"events":      b.eventList(),
		"event":       b.eventList(),
		"system":      b.State.SystemState(),
		"metrics":     b.metricsSnapshot(),
		"state_store": map[string]any{
			"devices":        b.State.Snapshot("devices"),
			"device":         b.State.Snapshot("devices"),
			"cameras":        b.State.Snapshot("cameras"),
			"nodes":          b.State.Snapshot("nodes"),
			"tracks":         b.State.Snapshot("tracks"),
			"clusters":       b.State.Snapshot("clusters"),
			"identities":     b.State.Snapshot("identities"),
			"presence":       b.State.Snapshot("presence"),
			"clips":          b.State.Snapshot("clips"),
			"validations":    b.State.Snapshot("validations"),
			"action_results": b.State.Snapshot("action_results"),
			"system":         b.State.SystemState(),
		},
	}
}

func (b *Builder) StatePayload() map[string]any {
	return map[string]any{
		"system":         b.State.SystemState(),
		"metrics":        b.metricsSnapshot(),
		"nodes":          b.State.Snapshot("nodes"),
		"devices":        b.State.Snapshot("devices"),
		"cameras":        b.State.Snapshot("cameras"),
		"presence":       b.State.Snapshot("presence"),
		"tracks":         b.State.Snapshot("tracks"),
		"clusters":       b.State.Snapshot("clusters"),
		"clips":          b.State.Snapshot("clips"),
		"identities":     b.State.Snapshot("identities"),
		"validations":    b.State.Snapshot("validations"),
		"action_results": b.State.Snapshot("action_results"),
		"topology":       b.TopologyTreeViews(),
		"residents":      b.ResidentViews(),
	}
}

func (b *Builder) DeviceViews() []map[string]any {
	items := b.Devices.Ordered()
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		current, _ := b.State.DeviceState(item.ID)
		lastSeen := time.Time{}
		online := false
		if current != nil {
			lastSeen = current.LastSeen
			online = current.Online
		}
		out = append(out, map[string]any{
			"ID":       item.ID,
			"Type":     item.Type,
			"Role":     item.Role,
			"Room":     item.Room,
			"NodeID":   item.NodeID,
			"Online":   online,
			"LastSeen": lastSeen,
		})
	}
	return out
}

func (b *Builder) ResidentViews() []map[string]any {
	if b.Mu != nil {
		b.Mu.RLock()
		defer b.Mu.RUnlock()
	}
	ids := make([]string, 0, len(b.Residents))
	for id := range b.Residents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		resident := b.Residents[id]
		presence, _ := b.State.PresenceState(id)
		identity, _ := b.State.Identity(id)
		stateValue := engine.StateAbsent
		nodeID := ""
		lastSeen := time.Time{}
		confidence := 0.0
		if presence != nil {
			stateValue = presence.State
			nodeID = presence.Location
			lastSeen = presence.LastSeen
			confidence = presence.Confidence
		} else if identity != nil {
			stateValue = identity.State
			nodeID = identity.LastNodeID
			lastSeen = identity.LastSeen
			confidence = identity.Confidence
		}
		out = append(out, map[string]any{
			"id":         resident.ID,
			"name":       resident.Name,
			"role":       resident.Role,
			"admin":      resident.Admin,
			"node_id":    nodeID,
			"last_seen":  lastSeen,
			"state":      stateValue,
			"confidence": confidence,
		})
	}
	return out
}

func (b *Builder) TopologyTreeViews() []map[string]any {
	if b.Mu != nil {
		b.Mu.RLock()
		defer b.Mu.RUnlock()
	}
	if b.Topology == nil {
		return []map[string]any{}
	}
	roots := make([]*topology.Node, 0)
	for _, node := range b.Topology.Nodes {
		if node != nil && node.Parent == nil {
			roots = append(roots, node)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	out := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		out = append(out, b.nodeView(root))
	}
	return out
}

func (b *Builder) nodeView(node *topology.Node) map[string]any {
	children := make([]*topology.Node, 0, len(node.Children))
	children = append(children, node.Children...)
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	childViews := make([]map[string]any, 0, len(children))
	for _, child := range children {
		childViews = append(childViews, b.nodeView(child))
	}
	dynamic := 0.0
	if nodeState, ok := b.State.NodeState(node.ID); ok && nodeState != nil {
		dynamic = nodeState.DangerScore
	}
	return map[string]any{
		"id":            node.ID,
		"name":          node.Name,
		"type":          node.Type,
		"dynamic_score": dynamic,
		"connect":       node.Connect,
		"children":      childViews,
	}
}

func (b *Builder) metricsSnapshot() map[string]any {
	if b.Metrics == nil {
		return map[string]any{}
	}
	return b.Metrics.Snapshot(b.State)
}

func (b *Builder) automationList() []automation.Rule {
	if b.Automation == nil {
		return nil
	}
	return b.Automation.List()
}

func (b *Builder) eventList() []*contract.Event {
	if b.Events == nil {
		return nil
	}
	return b.Events.List()
}
