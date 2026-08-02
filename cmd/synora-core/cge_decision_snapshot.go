package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"synora/internal/cge"
	"synora/internal/cge/durableids"
	"synora/internal/topology"
	"synora/pkg/contract"
)

// coreOperationalSnapshotProvider copies only bounded operational facts. No
// StateStore pointer or mutable Core collection crosses into CGE.
type coreOperationalSnapshotProvider struct{ app *coreApp }

func (p *coreOperationalSnapshotProvider) SnapshotForDecision(ctx context.Context, target cge.DecisionTarget) (cge.OperationalSnapshot, error) {
	if p == nil || p.app == nil || p.app.state == nil {
		return cge.OperationalSnapshot{}, fmt.Errorf("core operational snapshot unavailable")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return cge.OperationalSnapshot{}, ctx.Err()
		default:
		}
	}
	now := time.Now().UTC()
	if err := target.Validate(); err != nil {
		return cge.OperationalSnapshot{}, err
	}
	// Keep Core-owned topology/resident lookups behind the Core lock while the
	// StateStore portion is captured atomically by DecisionSnapshot.
	p.app.mu.RLock()
	revision, system, exists := p.app.state.DecisionSnapshot(string(target.Kind), target.ID)
	switch target.Kind {
	case cge.DecisionTargetNode:
		if p.app.topology != nil {
			if node, ok := p.app.topology.Nodes[target.ID]; ok && node != nil {
				exists = true
			}
		}
	case cge.DecisionTargetDevice:
		if p.protectedDevice(target.ID) {
			exists = true
		}
	case cge.DecisionTargetResident:
		for id := range p.app.residents {
			if durableids.ProtectRaw(durableids.KindEntity, id) == target.ID {
				exists = true
				break
			}
		}
	case cge.DecisionTargetZone:
		if p.app.topology != nil {
			if node, ok := p.app.topology.Nodes[target.ID]; ok && node != nil && node.Type == topology.NodeZone {
				exists = true
			}
		}
	}
	p.app.mu.RUnlock()
	if revision == 0 {
		return cge.OperationalSnapshot{}, fmt.Errorf("core operational revision unavailable")
	}
	usedKeys, conflictingIDs, err := p.decisionContext(ctx, target, now)
	if err != nil {
		return cge.OperationalSnapshot{}, err
	}
	policyRevision := uint64(0)
	if p.app.policy != nil {
		policyRevision = p.app.policy.Revision()
	}
	grantSnapshot := p.app.executionGrants.Clone()
	if grantSnapshot.SchemaVersion == "" {
		grantSnapshot = cge.EmptyGrantSnapshot(now)
	}
	grantSnapshot = p.protectConfiguredGrantTargets(grantSnapshot)
	return cge.OperationalSnapshot{
		CapturedAt: now, FreshUntil: now.Add(5 * time.Second), Revision: revision, PolicyRevision: policyRevision,
		GrantSnapshot:       grantSnapshot,
		AuthorityMode:       p.app.cognitiveAuthorityMode(),
		Targets:             p.operationalTargets(target, revision, exists),
		UsedIdempotencyKeys: usedKeys, ConflictingDecisionIDs: conflictingIDs,
		CurrentSystemState: system.LastState,
		SecurityMode:       string(system.Security.Mode),
	}, nil
}

func (p *coreOperationalSnapshotProvider) protectConfiguredGrantTargets(snapshot cge.GrantSnapshot) cge.GrantSnapshot {
	if p == nil || p.app == nil || p.app.device == nil {
		return snapshot
	}
	for index := range snapshot.Grants {
		grant := &snapshot.Grants[index]
		if grant.Target.Kind != cge.DecisionTargetDevice {
			continue
		}
		protected := false
		for rawID := range p.app.device.List() {
			if durableids.ProtectRaw(durableids.KindDevice, rawID) == grant.Target.ID {
				protected = true
				break
			}
		}
		if !protected {
			grant.Target.ID = durableids.ProtectRaw(durableids.KindDevice, grant.Target.ID)
			grant.Fingerprint = cge.ConfiguredExecutionCapabilityGrantFingerprint(*grant)
		}
	}
	snapshot.Fingerprint = cge.GrantSnapshotFingerprint(snapshot)
	return snapshot
}

func (p *coreOperationalSnapshotProvider) operationalTargets(requested cge.DecisionTarget, revision uint64, requestedExists bool) []cge.OperationalTarget {
	if p == nil || p.app == nil {
		return nil
	}
	topologyCount := 0
	if p.app.topology != nil {
		topologyCount = len(p.app.topology.Nodes)
	}
	deviceCount := 0
	if p.app.device != nil {
		deviceCount = len(p.app.device.List())
	}
	result := make([]cge.OperationalTarget, 0, 1+len(p.app.residents)+topologyCount+deviceCount)
	seen := make(map[string]struct{})
	appendTarget := func(value cge.OperationalTarget) {
		key := string(value.Target.Kind) + "\x00" + value.Target.ID
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	base := func(target cge.DecisionTarget, exists bool) cge.OperationalTarget {
		return cge.OperationalTarget{Target: target, Exists: exists, CurrentRevision: revision, Authorization: cge.OperationalAuthorization{Known: false, Authorized: false}, PhysicalLimits: cge.OperationalPhysicalLimits{Known: false}}
	}

	appendTarget(base(cge.DecisionTarget{Kind: cge.DecisionTargetSystem, ID: "system"}, true))
	if p.app.topology != nil {
		ids := make([]string, 0, len(p.app.topology.Nodes))
		for id := range p.app.topology.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			node := p.app.topology.Nodes[id]
			if node == nil {
				continue
			}
			appendTarget(cge.OperationalTarget{Target: cge.DecisionTarget{Kind: cge.DecisionTargetNode, ID: id}, Exists: true, CurrentRevision: revision, NodeID: id, ZoneID: zoneAncestor(node), Authorization: cge.OperationalAuthorization{Known: false}, PhysicalLimits: cge.OperationalPhysicalLimits{Known: false}})
			if node.Type == topology.NodeZone {
				appendTarget(cge.OperationalTarget{Target: cge.DecisionTarget{Kind: cge.DecisionTargetZone, ID: id}, Exists: true, CurrentRevision: revision, NodeID: id, ZoneID: id, Authorization: cge.OperationalAuthorization{Known: false}, PhysicalLimits: cge.OperationalPhysicalLimits{Known: false}})
			}
		}
	}
	if p.app.device != nil {
		devices := p.app.device.List()
		ids := make([]string, 0, len(devices))
		for id := range devices {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			device := devices[id]
			if device == nil || device.DeletedAt != nil {
				continue
			}
			appendTarget(cge.OperationalTarget{Target: cge.DecisionTarget{Kind: cge.DecisionTargetDevice, ID: durableids.ProtectRaw(durableids.KindDevice, id)}, Exists: device.Enabled, CurrentRevision: revision, Capabilities: canonicalOperationalCapabilities(device.Capabilities), NodeID: device.NodeID, ZoneID: p.zoneForNode(device.NodeID), Authorization: cge.OperationalAuthorization{Known: false}, PhysicalLimits: cge.OperationalPhysicalLimits{Known: false}})
		}
	}
	if p.app.residents != nil {
		ids := make([]string, 0, len(p.app.residents))
		for id := range p.app.residents {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			appendTarget(base(cge.DecisionTarget{Kind: cge.DecisionTargetResident, ID: durableids.ProtectRaw(durableids.KindEntity, id)}, true))
		}
	}
	appendTarget(base(requested, requestedExists))
	sort.Slice(result, func(i, j int) bool {
		if result[i].Target.Kind != result[j].Target.Kind {
			return result[i].Target.Kind < result[j].Target.Kind
		}
		return result[i].Target.ID < result[j].Target.ID
	})
	return result
}

func canonicalOperationalCapabilities(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "-", "_")))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (p *coreOperationalSnapshotProvider) zoneForNode(nodeID string) string {
	if p == nil || p.app == nil || p.app.topology == nil {
		return ""
	}
	node := p.app.topology.Nodes[nodeID]
	for node != nil {
		if node.Type == topology.NodeZone {
			return node.ID
		}
		node = node.Parent
	}
	return ""
}

func zoneAncestor(node *topology.Node) string {
	for node != nil {
		if node.Type == topology.NodeZone {
			return node.ID
		}
		node = node.Parent
	}
	return ""
}

// protectedDevice resolves the CGE-safe device reference against the detached
// Core registry without exposing the raw identifier across the boundary.
func (p *coreOperationalSnapshotProvider) protectedDevice(targetID string) bool {
	if p == nil || p.app == nil || p.app.device == nil || targetID == "" {
		return false
	}
	for id := range p.app.device.List() {
		if durableids.ProtectRaw(durableids.KindDevice, id) == targetID {
			return true
		}
	}
	return false
}

func (p *coreOperationalSnapshotProvider) decisionContext(ctx context.Context, target cge.DecisionTarget, now time.Time) ([]string, []string, error) {
	shadow, ok := p.app.cognitive.(*cge.ShadowEngine)
	if !ok || shadow == nil {
		return nil, nil, nil
	}
	records, err := shadow.Decisions(ctx)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]string, 0, len(records))
	conflicts := make([]string, 0, len(records))
	latest := make(map[string]cge.DecisionRecord, len(records))
	for _, record := range records {
		latest[record.Envelope.DecisionID] = record
	}
	seenKeys := make(map[string]struct{}, len(records))
	seenConflicts := make(map[string]struct{}, len(records))
	for _, record := range latest {
		if record.Envelope.IdempotencyKey != "" {
			if _, seen := seenKeys[record.Envelope.IdempotencyKey]; !seen && len(keys) < 64 {
				seenKeys[record.Envelope.IdempotencyKey] = struct{}{}
				keys = append(keys, record.Envelope.IdempotencyKey)
			}
		}
		if record.Status != cge.DecisionPublishedAuthoritative || record.ExecutionRequest == nil || record.ExecutionLease == nil {
			continue
		}
		lease := record.ExecutionLease
		if lease.ValidUntil.Before(now) || (lease.Status != cge.ExecutionLeasePlanned && lease.Status != cge.ExecutionLeaseDispatched && lease.Status != cge.ExecutionLeaseRunning) || !targetsOverlap(lease.Target, target) || !intentIncompatible(record.Envelope, target) {
			continue
		}
		if _, seen := seenConflicts[record.Envelope.DecisionID]; !seen && len(conflicts) < 32 {
			seenConflicts[record.Envelope.DecisionID] = struct{}{}
			conflicts = append(conflicts, record.Envelope.DecisionID)
		}
	}
	sort.Strings(keys)
	sort.Strings(conflicts)
	return keys, conflicts, nil
}

func targetsOverlap(left, right cge.DecisionTarget) bool {
	return left.Kind == cge.DecisionTargetSystem || right.Kind == cge.DecisionTargetSystem || (left.Kind == right.Kind && left.ID == right.ID)
}

func intentIncompatible(record cge.DecisionEnvelope, target cge.DecisionTarget) bool {
	if record.Target.Kind != target.Kind || record.Target.ID != target.ID {
		return true
	}
	return record.DecisionType != cge.DecisionTypeObserve
}

func (a *coreApp) cognitiveAuthorityMode() cge.AuthorityMode {
	if shadow, ok := a.cognitive.(*cge.ShadowEngine); ok && shadow != nil {
		return shadow.AuthorityMode()
	}
	return cge.AuthorityModeShadow
}

type coreDecisionPublicationSink struct{ bus coreBus }

func (s *coreDecisionPublicationSink) PublishDecision(ctx context.Context, decision cge.DecisionEnvelope) error {
	if s == nil || s.bus == nil {
		return fmt.Errorf("decision publication bus unavailable")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	payload, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	return s.bus.Send(contract.Message{ID: decision.DecisionID, Version: cge.DecisionEnvelopeSchemaVersion, Type: "cge.decision.advisory", Kind: contract.KindEvent, Source: "synora-core", Target: "synora-core", Timestamp: decision.CreatedAt, Priority: decision.Priority, CorrelationID: decision.SituationID, Payload: payload})
}
