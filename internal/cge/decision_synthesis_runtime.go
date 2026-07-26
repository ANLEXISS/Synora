package cge

import (
	"context"
	"strings"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/decisioncomparison"
	contracts "synora/internal/engine/contracts"
)

// AuthorityDecisionComparison is a bounded diagnostic; it never changes the
// historical Core decision or grants execution authority to the CGE.
type AuthorityDecisionComparison struct {
	EventID                    string                    `json:"event_id" yaml:"event_id"`
	HistoricalState            string                    `json:"historical_state" yaml:"historical_state"`
	CognitiveState             string                    `json:"cognitive_state" yaml:"cognitive_state"`
	HistoricalActionIDs        []string                  `json:"historical_action_ids,omitempty" yaml:"historical_action_ids,omitempty"`
	CognitiveIntentIDs         []string                  `json:"cognitive_intent_ids,omitempty" yaml:"cognitive_intent_ids,omitempty"`
	SameState                  bool                      `json:"same_state" yaml:"same_state"`
	SameRiskBand               bool                      `json:"same_risk_band" yaml:"same_risk_band"`
	DivergenceCodes            []string                  `json:"divergence_codes,omitempty" yaml:"divergence_codes,omitempty"`
	ComparedAt                 time.Time                 `json:"compared_at" yaml:"compared_at"`
	HistoricalChainID          string                    `json:"historical_chain_id,omitempty" yaml:"historical_chain_id,omitempty"`
	CognitiveChainID           string                    `json:"cognitive_chain_id,omitempty" yaml:"cognitive_chain_id,omitempty"`
	SameChain                  bool                      `json:"same_chain" yaml:"same_chain"`
	HistoricalDecisionType     string                    `json:"historical_decision_type,omitempty" yaml:"historical_decision_type,omitempty"`
	CognitiveDecisionType      string                    `json:"cognitive_decision_type,omitempty" yaml:"cognitive_decision_type,omitempty"`
	HistoricalTarget           DecisionTarget            `json:"historical_target" yaml:"historical_target"`
	CognitiveTarget            DecisionTarget            `json:"cognitive_target" yaml:"cognitive_target"`
	SafetyStatus               SafetyStatus              `json:"safety_status" yaml:"safety_status"`
	CognitivePublicationStatus DecisionPublicationStatus `json:"cognitive_publication_status" yaml:"cognitive_publication_status"`
}

type AuthorityComparisonMetrics struct {
	TotalComparisons    uint64 `json:"total_comparisons" yaml:"total_comparisons"`
	SameState           uint64 `json:"same_state" yaml:"same_state"`
	SameRiskBand        uint64 `json:"same_risk_band" yaml:"same_risk_band"`
	SameChain           uint64 `json:"same_chain" yaml:"same_chain"`
	CGEAllowed          uint64 `json:"cge_allowed" yaml:"cge_allowed"`
	CGEDenied           uint64 `json:"cge_denied" yaml:"cge_denied"`
	NoCognitiveMatch    uint64 `json:"no_cognitive_match" yaml:"no_cognitive_match"`
	AmbiguousMatch      uint64 `json:"ambiguous_match" yaml:"ambiguous_match"`
	HistoricalOnlyMatch uint64 `json:"historical_only_match" yaml:"historical_only_match"`
	CognitiveOnlyMatch  uint64 `json:"cognitive_only_match" yaml:"cognitive_only_match"`
}

func (c AuthorityDecisionComparison) Validate() error {
	if strings.TrimSpace(c.EventID) == "" || strings.TrimSpace(c.CognitiveState) == "" || c.ComparedAt.IsZero() {
		return ErrInvalidChainCandidate
	}
	if len(c.HistoricalActionIDs) > 16 || len(c.CognitiveIntentIDs) > 16 || len(c.DivergenceCodes) > 16 {
		return ErrInvalidChainCandidate
	}
	return nil
}

func (e *ShadowEngine) SetContractChains(critical []contracts.CriticalSeed, behaviors []contracts.LearnedBehavior, sequences []contracts.LearnedSequence) {
	if e == nil {
		return
	}
	if selector, ok := e.decisionSelector.(*ContractChainSelector); ok {
		selector.SetContracts(critical, behaviors, sequences)
	}
}

// SetCatalogProvider installs the read-only dynamic catalog boundary. Every
// selection obtains a detached revisioned snapshot from it.
func (e *ShadowEngine) SetCatalogProvider(provider CognitiveChainCatalogProvider) {
	if e == nil {
		return
	}
	if selector, ok := e.decisionSelector.(*ContractChainSelector); ok {
		selector.SetCatalogProvider(provider)
	}
}

func (e *ShadowEngine) synthesizeDecision(ctx context.Context, observation chains.ObservationRef, historical *decisioncomparison.HistoricalDecisionRef) {
	if e == nil || e.authority == nil || e.decisionSelector == nil || e.snapshotProvider == nil {
		return
	}
	status := e.WorkflowStatus()
	projection := e.WorkflowProjection()
	var situationID, cognitiveState, situationFingerprint string
	var workflowRevision uint64
	if e.workflow != nil {
		if situation, ok := e.workflow.SituationForObservation(observation.ID); ok {
			situationID, cognitiveState, situationFingerprint, workflowRevision = situation.ID, string(situation.Phase), situation.Fingerprint, situation.WorkflowRevision
		}
	}
	if situationID == "" {
		for _, situation := range projection.Situations.Situations {
			if situation.WorkflowRevision >= workflowRevision {
				situationID, cognitiveState, situationFingerprint, workflowRevision = situation.ID, string(situation.Phase), situation.Fingerprint, situation.WorkflowRevision
			}
		}
	}
	if situationID == "" {
		situationID = observation.ID
	}
	if workflowRevision == 0 {
		workflowRevision = status.WorkflowRevision
	}
	if workflowRevision == 0 {
		workflowRevision = 1
	}
	evidence := []string{observation.ID}
	if situationFingerprint != "" {
		evidence = append(evidence, situationFingerprint)
	}
	now := observation.Timestamp.UTC()
	if now.IsZero() {
		now = e.shadowNow()
	}
	input := CognitiveDecisionInput{EventID: observation.ID, ObservedEventType: observation.EventType, HistoricalChainID: observation.ChainID, SituationID: situationID, CognitiveState: cognitiveState, CoreRevision: workflowRevision, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, Confidence: 0.5, DangerScore: 0.5, EvidenceRefs: evidence, CreatedAt: now, ValidUntil: now.Add(5 * time.Minute)}
	if e.workflow != nil {
		if refs, ok := e.workflow.ObservationsForObservation(observation.ID); ok {
			input.Situation.Observations = make([]CognitiveObservationSnapshot, 0, len(refs))
			for _, ref := range refs {
				input.Situation.Observations = append(input.Situation.Observations, CognitiveObservationSnapshot{ID: ref.EventID, EventType: ref.EventType, Timestamp: ref.ObservedAt, NodeID: ref.NodeID, ZoneID: ref.ZoneID, EntityID: ref.Subject.EntityID, SequenceKey: ref.SequenceKey, ClipID: ref.ClipID})
			}
		}
	}
	input.Situation.SituationID, input.Situation.EpisodeID, input.Situation.CurrentObservationID, input.Situation.CapturedAt = situationID, situationID, observation.ID, now
	chain, err := e.decisionSelector.SelectDecisionChain(ctx, input)
	if err != nil {
		e.mu.Lock()
		if err == ErrNoDecisionChain {
			e.authorityComparisonMetrics.NoCognitiveMatch++
			if historical != nil {
				e.authorityComparisonMetrics.HistoricalOnlyMatch++
			}
		}
		e.mu.Unlock()
		if err != ErrNoDecisionChain {
			e.safeLog("decision_chain_selection_failed")
		}
		return
	}
	input.CognitiveChainID = chain.Reference.ChainID
	input.DangerScore, input.Confidence = chain.DangerScore, chain.Confidence
	if e.targetResolver != nil {
		target, targetErr := e.targetResolver.ResolveTarget(ctx, input.Situation, chain)
		if targetErr != nil {
			e.mu.Lock()
			if targetErr == ErrAmbiguousTarget {
				e.authorityComparisonMetrics.AmbiguousMatch++
				if historical != nil {
					e.authorityComparisonMetrics.HistoricalOnlyMatch++
				}
			}
			e.mu.Unlock()
			e.safeLog("decision_target_resolution_failed")
			return
		}
		input.Target = target
	}
	decision, err := e.decisionSynthesizer.SynthesizeDecision(ctx, input, chain)
	if err != nil {
		e.safeLog("decision_synthesis_failed")
		return
	}
	snapshot, err := e.snapshotProvider.SnapshotForDecision(ctx, decision.Target)
	if err != nil {
		e.safeLog("decision_snapshot_failed")
		return
	}
	// Bind the synthesized intent to the detached Core revision captured by the
	// snapshot. Re-synthesis keeps the deterministic decision identity tied to
	// the exact state the Safety Kernel will evaluate.
	if snapshot.Revision != 0 && (input.CoreRevision != snapshot.Revision || input.TargetRevision != snapshot.Revision) {
		input.CoreRevision = snapshot.Revision
		input.TargetRevision = snapshot.Revision
		decision, err = e.decisionSynthesizer.SynthesizeDecision(ctx, input, chain)
		if err != nil {
			e.safeLog("decision_synthesis_failed")
			return
		}
	}
	publication, err := e.authority.PublishDecision(ctx, decision, snapshot)
	if err != nil {
		e.safeLog("decision_publication_failed")
		return
	}
	if publication.Status == DecisionPublishedAdvisory && e.decisionSink != nil {
		if err := e.decisionSink.PublishDecision(ctx, decision); err != nil {
			e.safeLog("decision_advisory_transport_failed")
		}
	}
	if historical != nil {
		comparison := compareAuthorityDecision(observation.ID, historical, input.HistoricalChainID, decision, chain, publication, now)
		if err := comparison.Validate(); err == nil {
			e.mu.Lock()
			e.authorityComparisons = append(e.authorityComparisons, comparison)
			if len(e.authorityComparisons) > 128 {
				e.authorityComparisons = e.authorityComparisons[len(e.authorityComparisons)-128:]
			}
			e.authorityComparisonMetrics.TotalComparisons++
			if comparison.SameState {
				e.authorityComparisonMetrics.SameState++
			}
			if comparison.SameRiskBand {
				e.authorityComparisonMetrics.SameRiskBand++
			}
			if comparison.SameChain {
				e.authorityComparisonMetrics.SameChain++
			}
			if publication.Verdict.Status == SafetyAllowed {
				e.authorityComparisonMetrics.CGEAllowed++
			} else {
				e.authorityComparisonMetrics.CGEDenied++
			}
			e.mu.Unlock()
		}
	} else {
		e.mu.Lock()
		e.authorityComparisonMetrics.CognitiveOnlyMatch++
		if publication.Verdict.Status == SafetyAllowed {
			e.authorityComparisonMetrics.CGEAllowed++
		} else {
			e.authorityComparisonMetrics.CGEDenied++
		}
		e.mu.Unlock()
	}
}

func compareAuthorityDecision(eventID string, historical *decisioncomparison.HistoricalDecisionRef, historicalChainID string, decision DecisionEnvelope, chain CognitiveChainCandidate, publication DecisionPublication, at time.Time) AuthorityDecisionComparison {
	historicalTarget := DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}
	if historical.HistoricalTargetKind != "" && historical.HistoricalTargetID != "" {
		historicalTarget = DecisionTarget{Kind: DecisionTargetKind(historical.HistoricalTargetKind), ID: historical.HistoricalTargetID}
	}
	comparison := AuthorityDecisionComparison{EventID: eventID, HistoricalState: historical.CurrentStateCode, CognitiveState: chain.ExpectedState, HistoricalActionIDs: []string{historical.ID}, CognitiveIntentIDs: append([]string(nil), chain.ProposedActions...), ComparedAt: at, HistoricalChainID: historicalChainID, CognitiveChainID: chain.Reference.ChainID, SameChain: historicalChainID != "" && historicalChainID == chain.Reference.ChainID, HistoricalDecisionType: historical.HistoricalDecisionType, CognitiveDecisionType: string(decision.DecisionType), HistoricalTarget: historicalTarget, CognitiveTarget: decision.Target}
	comparison.CognitivePublicationStatus = publication.Status
	comparison.SafetyStatus = publication.Verdict.Status
	comparison.SameState = comparison.HistoricalState != "" && comparison.HistoricalState == comparison.CognitiveState
	historicalRisk := float64(historical.DecisionScorePermille) / 1000
	comparison.SameRiskBand = riskBand(historicalRisk) == riskBand(chain.DangerScore)
	if !comparison.SameState {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "state_divergence")
	}
	if !comparison.SameRiskBand {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "risk_band_divergence")
	}
	if !comparison.SameChain {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "chain_divergence")
	}
	if decision.DecisionType == DecisionTypeChangeMode {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "cognitive_mode_intent")
	}
	return comparison
}

func riskBand(value float64) string {
	switch {
	case value >= .85:
		return "critical"
	case value >= .65:
		return "high"
	case value >= .4:
		return "medium"
	default:
		return "low"
	}
}
