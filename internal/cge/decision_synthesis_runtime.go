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
	EventID             string    `json:"event_id" yaml:"event_id"`
	HistoricalState     string    `json:"historical_state" yaml:"historical_state"`
	CognitiveState      string    `json:"cognitive_state" yaml:"cognitive_state"`
	HistoricalActionIDs []string  `json:"historical_action_ids,omitempty" yaml:"historical_action_ids,omitempty"`
	CognitiveIntentIDs  []string  `json:"cognitive_intent_ids,omitempty" yaml:"cognitive_intent_ids,omitempty"`
	SameState           bool      `json:"same_state" yaml:"same_state"`
	SameRiskBand        bool      `json:"same_risk_band" yaml:"same_risk_band"`
	DivergenceCodes     []string  `json:"divergence_codes,omitempty" yaml:"divergence_codes,omitempty"`
	ComparedAt          time.Time `json:"compared_at" yaml:"compared_at"`
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
	input := CognitiveDecisionInput{EventID: observation.ID, ObservedEventType: observation.EventType, ChainID: observation.ChainID, SituationID: situationID, CognitiveState: cognitiveState, CoreRevision: workflowRevision, Target: DecisionTarget{Kind: DecisionTargetSystem, ID: "system"}, Confidence: 0.5, DangerScore: 0.5, EvidenceRefs: evidence, CreatedAt: now, ValidUntil: now.Add(5 * time.Minute)}
	chain, err := e.decisionSelector.SelectDecisionChain(ctx, input)
	if err != nil {
		if err != ErrNoDecisionChain {
			e.safeLog("decision_chain_selection_failed")
		}
		return
	}
	input.ChainID = chain.Reference.ChainID
	input.DangerScore, input.Confidence = chain.DangerScore, chain.Confidence
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
		comparison := compareAuthorityDecision(observation.ID, historical, decision, chain, now)
		if err := comparison.Validate(); err == nil {
			e.mu.Lock()
			e.authorityComparisons = append(e.authorityComparisons, comparison)
			if len(e.authorityComparisons) > 128 {
				e.authorityComparisons = e.authorityComparisons[len(e.authorityComparisons)-128:]
			}
			e.mu.Unlock()
		}
	}
}

func compareAuthorityDecision(eventID string, historical *decisioncomparison.HistoricalDecisionRef, decision DecisionEnvelope, chain CognitiveChainCandidate, at time.Time) AuthorityDecisionComparison {
	comparison := AuthorityDecisionComparison{EventID: eventID, HistoricalState: historical.CurrentStateCode, CognitiveState: chain.ExpectedState, HistoricalActionIDs: []string{historical.ID}, CognitiveIntentIDs: append([]string(nil), chain.ProposedActions...), ComparedAt: at}
	comparison.SameState = comparison.HistoricalState != "" && comparison.HistoricalState == comparison.CognitiveState
	historicalRisk := float64(historical.DecisionScorePermille) / 1000
	comparison.SameRiskBand = riskBand(historicalRisk) == riskBand(chain.DangerScore)
	if !comparison.SameState {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "state_divergence")
	}
	if !comparison.SameRiskBand {
		comparison.DivergenceCodes = append(comparison.DivergenceCodes, "risk_band_divergence")
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
