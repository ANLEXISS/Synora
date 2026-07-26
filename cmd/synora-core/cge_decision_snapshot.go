package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"synora/internal/cge"
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
	revision := p.app.coreRevision.Load()
	if revision == 0 {
		revision = 1
	}
	system := p.app.state.SystemState()
	if err := target.Validate(); err != nil {
		return cge.OperationalSnapshot{}, err
	}
	exists := false
	switch target.Kind {
	case cge.DecisionTargetSystem:
		exists = target.ID == "system"
	case cge.DecisionTargetNode:
		_, exists = p.app.state.NodeState(target.ID)
	case cge.DecisionTargetDevice:
		_, exists = p.app.state.DeviceState(target.ID)
	case cge.DecisionTargetResident:
		p.app.mu.RLock()
		_, exists = p.app.residents[target.ID]
		p.app.mu.RUnlock()
	case cge.DecisionTargetZone:
		exists = false
	}
	usedKeys, conflictingIDs, err := p.decisionContext(ctx, target, now)
	if err != nil {
		return cge.OperationalSnapshot{}, err
	}
	return cge.OperationalSnapshot{
		CapturedAt: now, FreshUntil: now.Add(5 * time.Second), Revision: revision,
		AuthorityMode:       p.app.cognitiveAuthorityMode(),
		Targets:             []cge.OperationalTarget{{Target: target, Exists: exists, Authorized: exists && !system.Degraded, PhysicalLimit: 100, CurrentRevision: revision}},
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
	seenKeys := make(map[string]struct{}, len(records))
	seenConflicts := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.Envelope.IdempotencyKey != "" {
			if _, seen := seenKeys[record.Envelope.IdempotencyKey]; !seen && len(keys) < 64 {
				seenKeys[record.Envelope.IdempotencyKey] = struct{}{}
				keys = append(keys, record.Envelope.IdempotencyKey)
			}
		}
		if record.Status != cge.DecisionPublishedShadow && record.Status != cge.DecisionPublishedAdvisory && record.Status != cge.DecisionPublishedAuthoritative {
			continue
		}
		if !record.Envelope.ValidUntil.After(now) || record.Envelope.Target.Kind != target.Kind || record.Envelope.Target.ID != target.ID {
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
