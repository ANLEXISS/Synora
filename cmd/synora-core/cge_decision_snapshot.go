package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge"
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
		if p.app.device != nil {
			if _, ok := p.app.device.Get(target.ID); ok {
				exists = true
			}
		}
	case cge.DecisionTargetResident:
		_, configured := p.app.residents[target.ID]
		exists = exists || configured
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
	return cge.OperationalSnapshot{
		CapturedAt: now, FreshUntil: now.Add(5 * time.Second), Revision: revision,
		AuthorityMode:       p.app.cognitiveAuthorityMode(),
		Targets:             []cge.OperationalTarget{{Target: target, Exists: exists, Authorized: false, PhysicalLimit: 0, CurrentRevision: revision, Authorization: cge.OperationalAuthorization{Known: false, Authorized: false}, PhysicalLimits: cge.OperationalPhysicalLimits{Known: false}}},
		UsedIdempotencyKeys: usedKeys, ConflictingDecisionIDs: conflictingIDs,
		CurrentSystemState: system.LastState,
		SecurityMode:       string(system.Security.Mode),
	}, nil
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
	return s.bus.Send(contract.Message{ID: decision.DecisionID, Version: cge.DecisionEnvelopeSchemaVersion, Type: "cge.decision.advisory", Kind: contract.KindEvent, Source: "core", Target: "core", Timestamp: decision.CreatedAt, Priority: decision.Priority, CorrelationID: decision.SituationID, Payload: payload})
}
