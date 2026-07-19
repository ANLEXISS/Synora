package hypotheses

import (
	"fmt"
	"sort"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
)

// FromAmbiguousAssociation converts the retained plausible candidates of an
// ambiguous association plan into an open hypothesis set. It never mutates
// the plan or any chain.
func FromAmbiguousAssociation(plan association.Plan, createdAt time.Time, mutation chains.MutationContext) (*HypothesisSet, error) {
	if plan.Decision != association.DecisionAmbiguous {
		return nil, hypothesisError(ErrAssociationNotAmbiguous, FamilyAssociation, "", "association_conversion", 0, 0, nil)
	}
	if err := plan.Validate(); err != nil {
		return nil, hypothesisError(ErrInvalidHypothesis, FamilyAssociation, "", "association_conversion", 0, 0, err)
	}
	if len(plan.RankedCandidates) < 2 {
		return nil, hypothesisError(ErrInsufficientHypothesisAlternatives, FamilyAssociation, "", "association_conversion", 0, 0, nil)
	}
	eligible := make([]association.CandidateScore, 0, len(plan.RankedCandidates))
	for _, candidate := range plan.RankedCandidates {
		if candidate.Eligible {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) < 2 {
		return nil, hypothesisError(ErrInsufficientHypothesisAlternatives, FamilyAssociation, "", "association_conversion", 0, 0, nil)
	}
	subject := Subject{ObservationID: plan.Observation.ID}
	provenance := Provenance{Source: string(FamilyAssociation), PolicyNamespace: associationNamespace, PolicyVersion: plan.PolicyVersion, PlannedOrEvaluatedAt: plan.PlannedAt}
	setID, err := DeriveAssociationSetID(subject.ObservationID, plan.PolicyVersion)
	if err != nil {
		return nil, err
	}
	alternatives := make([]Alternative, 0, len(eligible))
	for rank, candidate := range eligible {
		facts := make([]FactReference, 0, len(candidate.Facts))
		for _, fact := range candidate.Facts {
			facts = append(facts, FactReference{Code: fact.Code, Side: "compatibility", Score: fact.Score, ObservationIDs: []string{plan.Observation.ID}})
		}
		alternative := Alternative{Kind: AlternativeAttachExisting, ChainID: candidate.ChainID, SourceRevision: candidate.SourceRevision, Score: candidate.Score, Rank: rank + 1, ReasonCode: nonEmptyReasonCode(plan.ReasonCode, "association.candidate"), Facts: facts, ResolutionEffect: &ResolutionEffect{Kind: ResolutionEffectAttachObservation, AttachObservation: &AttachObservationEffect{ChainID: candidate.ChainID, SourceRevision: candidate.SourceRevision, Observation: plan.Observation}}}
		alternative.ID = deriveAlternativeID(setID, alternative)
		alternatives = append(alternatives, alternative)
	}
	return openHypothesis(setID, FamilyAssociation, subject, alternatives, provenance, nonEmptyReasonCode(plan.ReasonCode, "association.ambiguous"), boundedReason(plan.Reason, "association ambiguity"), createdAt, mutation)
}

// FromAmbiguousEvidence converts only directions represented by positive
// evidence facts into alternatives. It does not create or apply a
// contribution.
func FromAmbiguousEvidence(evaluation evidence.EvidenceEvaluation, createdAt time.Time, mutation chains.MutationContext) (*HypothesisSet, error) {
	if evaluation.Decision != evidence.DecisionAmbiguous {
		return nil, hypothesisError(ErrEvidenceNotAmbiguous, FamilyEvidence, "", "evidence_conversion", 0, 0, nil)
	}
	if err := validateEvidenceEvaluationForConversion(evaluation); err != nil {
		return nil, err
	}
	if err := evaluation.ResolutionValues.Validate(); err != nil {
		return nil, hypothesisError(ErrInvalidHypothesis, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	subject := Subject{ObservationID: evaluation.TargetObservationID, ChainID: evaluation.ChainID, EvidenceFingerprint: evaluation.EvidenceFingerprint}
	setID, err := DeriveEvidenceSetID(subject.ChainID, subject.ObservationID, subject.EvidenceFingerprint)
	if err != nil {
		return nil, err
	}
	kindFacts := map[AlternativeKind][]FactReference{
		AlternativeSupport: {}, AlternativeContradiction: {}, AlternativeNeutral: {}, AlternativeInsufficient: {},
	}
	for _, fact := range evaluation.Facts {
		ref := FactReference{Code: fact.Code, Side: string(fact.Side), Score: fact.Score, ObservationIDs: append([]string(nil), fact.ObservationIDs...)}
		switch fact.Side {
		case evidence.EvidenceSupport:
			if fact.Score > 0 {
				kindFacts[AlternativeSupport] = append(kindFacts[AlternativeSupport], ref)
			}
		case evidence.EvidenceContradiction:
			if fact.Score > 0 {
				kindFacts[AlternativeContradiction] = append(kindFacts[AlternativeContradiction], ref)
			}
		case evidence.EvidenceNeutral:
			if isPlausibleNeutralFact(fact.Code) {
				kindFacts[AlternativeNeutral] = append(kindFacts[AlternativeNeutral], ref)
			}
		}
	}
	if len(kindFacts[AlternativeSupport]) == 0 && len(kindFacts[AlternativeContradiction]) == 0 {
		for _, fact := range evaluation.Facts {
			if fact.Code == "context.empty" || fact.Code == "type.unknown" || fact.Code == "type.uncertain" {
				kindFacts[AlternativeInsufficient] = append(kindFacts[AlternativeInsufficient], FactReference{Code: fact.Code, Side: string(fact.Side), Score: fact.Score, ObservationIDs: append([]string(nil), fact.ObservationIDs...)})
			}
		}
	}
	order := []AlternativeKind{AlternativeSupport, AlternativeContradiction, AlternativeNeutral, AlternativeInsufficient}
	alternatives := make([]Alternative, 0, len(order))
	for _, kind := range order {
		facts := kindFacts[kind]
		if len(facts) == 0 {
			continue
		}
		score := int64(0)
		for _, fact := range facts {
			if fact.Score > 0 {
				score += fact.Score
			}
		}
		if kind == AlternativeSupport {
			score = evaluation.SupportScore
		}
		if kind == AlternativeContradiction {
			score = evaluation.ContradictionScore
		}
		alternative := Alternative{Kind: kind, ChainID: evaluation.ChainID, SourceRevision: evaluation.SourceRevision, Score: score, Rank: len(alternatives) + 1, ReasonCode: fmt.Sprintf("evidence.ambiguous.%s", kind), Facts: cloneFacts(facts), EvidenceFingerprint: evaluation.EvidenceFingerprint}
		switch kind {
		case AlternativeSupport, AlternativeContradiction, AlternativeNeutral:
			contributionKind := chains.ContributionNeutral
			value := evaluation.ResolutionValues.NeutralValue
			switch kind {
			case AlternativeSupport:
				contributionKind, value = chains.ContributionSupport, evaluation.ResolutionValues.SupportValue
			case AlternativeContradiction:
				contributionKind, value = chains.ContributionContradiction, evaluation.ResolutionValues.ContradictionValue
			}
			contributionID := evidence.ContributionIDForFingerprint(evaluation.EvidenceFingerprint)
			observationIDs := sortObservationIDs(append([]string{evaluation.TargetObservationID}, evaluation.ContextObservationIDs...))
			alternative.ContributionID = contributionID
			alternative.ResolutionEffect = &ResolutionEffect{Kind: ResolutionEffectAddContribution, AddContribution: &AddContributionEffect{ChainID: evaluation.ChainID, SourceRevision: evaluation.SourceRevision, ContributionTemplate: ContributionTemplate{ID: contributionID, Source: evidence.ContributionSource(evaluation.PolicyVersion), Kind: contributionKind, Value: value, ObservationIDs: observationIDs, ReasonCode: fmt.Sprintf("evidence.ambiguous.%s", kind)}}}
		case AlternativeInsufficient:
			alternative.ResolutionEffect = &ResolutionEffect{Kind: ResolutionEffectNoChain, NoChainEffect: &NoChainEffect{ReasonCode: "evidence.insufficient"}}
		}
		alternative.ID = deriveAlternativeID(setID, alternative)
		alternatives = append(alternatives, alternative)
	}
	if len(alternatives) < 2 {
		return nil, hypothesisError(ErrInsufficientHypothesisAlternatives, FamilyEvidence, setID, "evidence_conversion", 0, 0, nil)
	}
	// The evaluator already supplies deterministic facts; sorting here makes
	// conversion independent of the caller's slice order for equal kinds.
	sort.SliceStable(alternatives, func(i, j int) bool { return alternatives[i].Rank < alternatives[j].Rank })
	provenance := Provenance{Source: string(FamilyEvidence), PolicyNamespace: evaluation.PolicyNamespace, PolicyVersion: evaluation.PolicyVersion, PlannedOrEvaluatedAt: evaluation.EvaluatedAt, SourceRevision: evaluation.SourceRevision}
	return openHypothesis(setID, FamilyEvidence, subject, alternatives, provenance, nonEmptyReasonCode(evaluation.ReasonCode, "evidence.ambiguous"), boundedReason(evaluation.Reason, "evidence ambiguity"), createdAt, mutation)
}

func validateEvidenceEvaluationForConversion(evaluation evidence.EvidenceEvaluation) error {
	if _, err := chains.NewChainID(string(evaluation.ChainID)); err != nil {
		return hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	if evaluation.SourceRevision == 0 || evaluation.EvaluatedAt.IsZero() {
		return hypothesisError(ErrInvalidHypothesis, FamilyEvidence, "", "evidence_conversion", 0, 0, fmt.Errorf("evaluation revision or timestamp is invalid"))
	}
	if err := validText(evaluation.TargetObservationID, "target observation id", true, 256); err != nil {
		return hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	if err := validText(evaluation.EvidenceFingerprint, "evidence fingerprint", true, 256); err != nil {
		return hypothesisError(ErrInvalidHypothesisSubject, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	if err := validText(evaluation.PolicyNamespace, "policy namespace", true, 128); err != nil {
		return hypothesisError(ErrInvalidHypothesisProvenance, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	if err := validText(evaluation.PolicyVersion, "policy version", true, 128); err != nil {
		return hypothesisError(ErrInvalidHypothesisProvenance, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
	}
	if evaluation.SupportScore < 0 || evaluation.ContradictionScore < 0 || evaluation.DecisionMargin < 0 {
		return hypothesisError(ErrInvalidHypothesis, FamilyEvidence, "", "evidence_conversion", 0, 0, fmt.Errorf("evaluation scores are invalid"))
	}
	seenContext := make(map[string]struct{}, len(evaluation.ContextObservationIDs))
	for _, id := range evaluation.ContextObservationIDs {
		if err := validText(id, "context observation id", true, 256); err != nil {
			return hypothesisError(ErrInvalidHypothesis, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
		}
		if _, ok := seenContext[id]; ok {
			return hypothesisError(ErrInvalidHypothesis, FamilyEvidence, "", "evidence_conversion", 0, 0, fmt.Errorf("duplicate context observation id"))
		}
		seenContext[id] = struct{}{}
	}
	for _, fact := range evaluation.Facts {
		if err := fact.Validate(); err != nil {
			return hypothesisError(ErrInvalidHypothesisAlternative, FamilyEvidence, "", "evidence_conversion", 0, 0, err)
		}
	}
	return nil
}

func isPlausibleNeutralFact(code string) bool {
	switch code {
	case "type.unknown", "type.uncertain", "context.empty", "context.mixed_entities":
		return true
	default:
		return false
	}
}

func nonEmptyReasonCode(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if len([]rune(value)) > 64 {
		return string([]rune(value)[:64])
	}
	return value
}

func boundedReason(value, fallback string) string {
	if value == "" {
		return fallback
	}
	if len([]rune(value)) > 256 {
		return string([]rune(value)[:256])
	}
	return value
}
