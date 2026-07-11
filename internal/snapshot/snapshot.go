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

type CGEInspector interface {
	CGEInspection() map[string]any
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
	CGE        CGEInspector
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
	cge := b.cgeInspection()
	cge["danger_assessments"] = b.DangerAssessmentViews()
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
		"cge":         cge,
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
			"events":         b.State.Snapshot("events"),
			"validations":    b.State.Snapshot("validations"),
			"action_results": b.State.Snapshot("action_results"),
			"danger":         b.DangerAssessmentViews(),
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
		"danger":         b.DangerAssessmentViews(),
		"topology":       b.TopologyTreeViews(),
		"residents":      b.ResidentViews(),
	}
}

func (b *Builder) DangerAssessmentViews() []map[string]any {
	if b == nil || b.State == nil {
		return []map[string]any{}
	}
	items := b.State.DangerAssessmentsList()
	out := make([]map[string]any, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		out = append(out, compactDangerAssessment(items[i]))
	}
	return out
}

func (b *Builder) DangerAssessmentView(id string) (map[string]any, bool) {
	if b == nil || b.State == nil {
		return nil, false
	}
	for _, item := range b.State.DangerAssessmentsList() {
		if item.ID != id {
			continue
		}
		view := compactDangerAssessment(item)
		view["title"] = item.Title
		view["explanation"] = item.Explanation
		view["reasons"] = limitedStrings(item.Reasons, 20)
		view["evidence"] = limitedStrings(item.Evidence, 20)
		view["recommended_actions"] = compactDangerActions(item.RecommendedSystemActions, 10)
		return view, true
	}
	return nil, false
}

func compactDangerAssessment(item contract.DangerAssessment) map[string]any {
	lastSeen := item.LastSeen
	if lastSeen.IsZero() {
		lastSeen = item.CreatedAt
	}
	return map[string]any{
		"id":                       item.ID,
		"event_type":               item.EventType,
		"danger_score":             item.Score,
		"risk_level":               dangerRiskLevel(item),
		"expected_state":           dangerExpectedState(item),
		"created_at":               item.CreatedAt,
		"last_seen":                lastSeen,
		"evidence_count":           len(item.Evidence),
		"recommended_action_count": len(item.RecommendedSystemActions),
		"matched_seed_id":          item.MatchedSeedID,
		"requires_validation":      item.ValidationRequired,
	}
}

func dangerRiskLevel(item contract.DangerAssessment) string {
	if item.RiskLevel != "" {
		return item.RiskLevel
	}
	switch {
	case item.Level >= 5:
		return "critical"
	case item.Level >= 4:
		return "high"
	case item.Level >= 3:
		return "medium_high"
	case item.Level >= 2:
		return "medium"
	default:
		return "low"
	}
}

func dangerExpectedState(item contract.DangerAssessment) string {
	if item.ExpectedState != "" {
		return item.ExpectedState
	}
	switch {
	case item.Level >= 5:
		return "intrusion"
	case item.Level >= 3:
		return "suspicious"
	default:
		return "activity"
	}
}

func limitedStrings(items []string, limit int) []string {
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return append([]string(nil), items...)
}

func compactDangerActions(items []contract.SystemActionRecommendation, limit int) []map[string]any {
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"type": item.Type, "priority": item.Priority, "reason": item.Reason,
			"target": item.Target, "dry_run": item.DryRun, "simulated": item.Simulated,
		})
	}
	return out
}

func (b *Builder) DeviceViews() []map[string]any {
	items := b.Devices.Ordered()
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		view := item.PublicView()
		current, _ := b.State.DeviceState(item.ID)
		lastSeen := time.Time{}
		online := false
		if current != nil {
			lastSeen = current.LastSeen
			online = current.Online
		}
		out = append(out, map[string]any{
			"id":           view.ID,
			"name":         view.Name,
			"type":         view.Type,
			"role":         view.Role,
			"node_id":      view.NodeID,
			"zone_role":    view.ZoneRole,
			"room_name":    view.RoomName,
			"enabled":      view.Enabled,
			"trusted":      view.Trusted,
			"capabilities": view.Capabilities,
			"config":       view.Config,
			"metadata":     view.Metadata,
			"created_at":   view.CreatedAt,
			"updated_at":   view.UpdatedAt,
			"deleted_at":   view.DeletedAt,
			"online":       online,
			"last_seen":    lastSeen,
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
		if resident == nil {
			continue
		}
		view := resident.PublicView()
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
			if lastSeen.IsZero() && identity != nil {
				lastSeen = identity.LastSeen
			}
		} else if identity != nil {
			stateValue = identity.State
			nodeID = identity.LastNodeID
			lastSeen = identity.LastSeen
			confidence = identity.Confidence
		}
		if lastSeen.IsZero() && resident.Presence != nil && resident.Presence.LastSeen != 0 {
			lastSeen = time.UnixMilli(resident.Presence.LastSeen).UTC()
		}
		out = append(out, map[string]any{
			"id":           view.ID,
			"name":         view.Name,
			"display_name": view.DisplayName,
			"role":         view.Role,
			"admin":        view.Admin,
			"enabled":      view.Enabled,
			"trusted":      view.Trusted,
			"metadata":     view.Metadata,
			"created_at":   view.CreatedAt,
			"updated_at":   view.UpdatedAt,
			"deleted_at":   view.DeletedAt,
			"node_id":      nodeID,
			"last_seen":    lastSeen,
			"state":        stateValue,
			"confidence":   confidence,
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

func (b *Builder) cgeInspection() map[string]any {
	if b.CGE == nil {
		return map[string]any{
			"stats":             map[string]any{},
			"sequences":         []any{},
			"transitions":       []any{},
			"learned_behaviors": []any{},
		}
	}
	return b.CGE.CGEInspection()
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
