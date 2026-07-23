package cognitivesituation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"synora/internal/cge/durableworkflow"
	"synora/internal/cge/episodes"
)

func Build(input BuildInput, policy Policy) (CognitiveSituation, error) {
	if err := validatePolicyOrError(policy); err != nil {
		return CognitiveSituation{}, err
	}
	if !validDepth(input.ExpectedDepth) || strings.TrimSpace(input.EpisodeID) == "" {
		return CognitiveSituation{}, ErrInvalidBuildInput
	}
	if err := durableworkflow.ValidateWorkflowState(input.Workflow); err != nil {
		return CognitiveSituation{}, fmt.Errorf("%w: %v", ErrWorkflowInvalid, err)
	}
	var state durableworkflow.EpisodeWorkflowState
	found := false
	for _, value := range input.Workflow.Episodes {
		if value.EpisodeID == input.EpisodeID {
			state = value.Clone()
			found = true
			break
		}
	}
	if !found {
		return CognitiveSituation{}, ErrEpisodeNotFound
	}

	situation := CognitiveSituation{
		ID:               "cognitive-situation-" + shortID(input.EpisodeID),
		EpisodeID:        input.EpisodeID,
		ExpectedDepth:    input.ExpectedDepth,
		WorkflowRevision: input.Workflow.Revision,
		WorkflowSequence: input.Workflow.LastSequence,
		WorkflowDigest:   input.Workflow.Digest,
		Markers: CognitiveSituationMarkers{
			NotADecision: true, NotAProbability: true, NotAuthorization: true,
			NotACommand: true, NotAnAction: true, NoSecurityMeaning: true,
			DerivedFromCommittedState: true,
		},
	}
	situation.SourceFingerprints = sourceFingerprints(state)
	situation.Knowledge = summarizeKnowledge(state, input.ExpectedDepth)
	situation.Hypotheses = summarizeHypotheses(state, policy)
	situation.Evidence = summarizeEvidence(state, policy)
	situation.Advisory = summarizeAdvisory(state, policy)
	situation.Capability = summarizeCapability(state, input.ExpectedDepth)
	situation.Authorization = summarizeAuthorization(state, input.ExpectedDepth)
	situation.Phase = determinePhase(situation, input.ExpectedDepth, policy)
	situation.RecommendationReadiness = determineReadiness(situation, input.ExpectedDepth, policy)
	if input.Previous != nil {
		situation.PreviousSituationID = input.Previous.ID
		situation.PreviousFingerprint = input.Previous.Fingerprint
	}
	situation.Fingerprint = situationFingerprint(situation)
	if input.Previous != nil && input.Previous.Fingerprint == situation.Fingerprint {
		situation.Revision = input.Previous.Revision
	} else if input.Previous != nil {
		situation.Revision = input.Previous.Revision + 1
	} else {
		situation.Revision = 1
	}
	situation.Fingerprint = situationFingerprint(situation)
	if err := situation.Validate(policy); err != nil {
		return CognitiveSituation{}, err
	}
	return situation, nil
}

func sourceFingerprints(state durableworkflow.EpisodeWorkflowState) SourceFingerprints {
	out := SourceFingerprints{}
	if state.Episode != nil {
		out.Episode = episodes.EpisodeFingerprint(*state.Episode)
		contexts := make([]string, 0)
		for _, observation := range state.Episode.Observations {
			if observation.ContextSnapshotFingerprint != "" {
				contexts = append(contexts, observation.ContextSnapshotFingerprint)
			}
		}
		if len(contexts) > 0 {
			sort.Strings(contexts)
			digest := sha256.Sum256([]byte(strings.Join(contexts, "\n")))
			out.Context = "core-context-chain:" + hex.EncodeToString(digest[:])
		}
	}
	if state.Facts != nil {
		out.Facts = state.Facts.Fingerprint
	}
	if state.Hypotheses != nil {
		out.Hypotheses = state.Hypotheses.Fingerprint
	}
	if state.Discrimination != nil {
		out.Discrimination = state.Discrimination.Fingerprint
	}
	for _, value := range state.AdvisoryRequests {
		if value.Fingerprint != "" {
			out.AdvisoryRequests = append(out.AdvisoryRequests, value.Fingerprint)
		}
	}
	for _, value := range state.CapabilityMappings {
		if value.Fingerprint != "" {
			out.CapabilityMappings = append(out.CapabilityMappings, value.Fingerprint)
		}
	}
	for _, value := range state.AuthorizationAssessments {
		if value.Fingerprint != "" {
			out.AuthorizationAssessments = append(out.AuthorizationAssessments, value.Fingerprint)
		}
	}
	sort.Strings(out.AdvisoryRequests)
	sort.Strings(out.CapabilityMappings)
	sort.Strings(out.AuthorizationAssessments)
	return out
}

func summarizeKnowledge(state durableworkflow.EpisodeWorkflowState, depth ExpectedPipelineDepth) KnowledgeSummary {
	layers := durableworkflow.Layers()
	out := KnowledgeSummary{LayerStates: make([]LayerKnowledgeState, 0, len(layers))}
	expected := 0
	fresh := 0
	for index, layer := range layers {
		want := expectedLayer(depth, layer)
		if want {
			expected++
		}
		value := state.Freshness[layer]
		present := layerPresent(state, layer)
		item := LayerKnowledgeState{Layer: layer, Expected: want, Freshness: value, Present: present}
		item.SourceFingerprint = sourceFingerprintFor(state, layer)
		if !want {
			item.Freshness = durableworkflow.FreshnessAbsent
			item.ReasonCodes = []string{"layer.not_configured"}
		} else {
			switch {
			case value == durableworkflow.FreshnessFresh && present:
				fresh++
				item.ReasonCodes = []string{"layer.fresh"}
			case value == durableworkflow.FreshnessFresh && !present:
				item.ReasonCodes = []string{"layer.upstream_missing"}
			case value == durableworkflow.FreshnessStale:
				item.ReasonCodes = []string{"layer.stale"}
			case value == durableworkflow.FreshnessInvalidated:
				item.ReasonCodes = []string{"layer.invalidated"}
			default:
				item.ReasonCodes = []string{"layer.absent"}
			}
		}
		_ = index
		out.LayerStates = append(out.LayerStates, item)
		if want && value == durableworkflow.FreshnessStale {
			out.StaleLayers++
		}
		if want && value == durableworkflow.FreshnessInvalidated {
			out.InvalidatedLayers++
		}
		if want && (!present || value == durableworkflow.FreshnessAbsent) {
			out.AbsentExpectedLayers++
		}
	}
	out.ExpectedLayers, out.FreshLayers = expected, fresh
	if expected > 0 {
		out.OverallCoveragePermille = fresh * 1000 / expected
	}
	if state.Facts != nil {
		for _, fact := range state.Facts.Facts {
			switch string(fact.Status) {
			case "unknown":
				out.UnknownFacts++
			case "asserted":
				out.AssertedFacts++
			case "conflicting":
				out.ConflictingFactCount++
			}
			if fact.Quality.Partial {
				out.PartialContext = true
			}
			if string(fact.Status) == "conflicting" {
				out.ConflictingFacts = true
			}
		}
		if len(state.Facts.Conflicts) > 0 {
			out.ConflictingFacts = true
		}
	}
	return out
}

func layerPresent(state durableworkflow.EpisodeWorkflowState, layer durableworkflow.LayerKind) bool {
	switch layer {
	case durableworkflow.LayerEpisode:
		return state.Episode != nil
	case durableworkflow.LayerSituationFacts:
		return state.Facts != nil
	case durableworkflow.LayerSituationHypotheses:
		return state.Hypotheses != nil
	case durableworkflow.LayerEvidenceDiscrimination:
		return state.Discrimination != nil
	case durableworkflow.LayerAdvisoryRequests:
		return state.Freshness[layer] != durableworkflow.FreshnessAbsent
	case durableworkflow.LayerCapabilityMapping:
		return state.Freshness[layer] != durableworkflow.FreshnessAbsent
	case durableworkflow.LayerAuthorizationBoundary:
		return state.Freshness[layer] != durableworkflow.FreshnessAbsent
	default:
		return false
	}
}

func sourceFingerprintFor(state durableworkflow.EpisodeWorkflowState, layer durableworkflow.LayerKind) string {
	switch layer {
	case durableworkflow.LayerEpisode:
		if state.Episode != nil {
			return episodes.EpisodeFingerprint(*state.Episode)
		}
	case durableworkflow.LayerSituationFacts:
		if state.Facts != nil {
			return state.Facts.Fingerprint
		}
	case durableworkflow.LayerSituationHypotheses:
		if state.Hypotheses != nil {
			return state.Hypotheses.Fingerprint
		}
	case durableworkflow.LayerEvidenceDiscrimination:
		if state.Discrimination != nil {
			return state.Discrimination.Fingerprint
		}
	}
	return ""
}

func summarizeHypotheses(state durableworkflow.EpisodeWorkflowState, policy Policy) HypothesisSummary {
	out := HypothesisSummary{}
	if state.Hypotheses == nil {
		return out
	}
	out.Available = true
	set := state.Hypotheses
	out.Ambiguous = set.Ambiguous
	out.LeadingHypothesisID = string(set.LeadingHypothesisID)
	out.LeadingMarginPermille = clamp(set.LeadingMarginPermille)
	hypotheses := set.Clone()
	sort.Slice(hypotheses.Hypotheses, func(i, j int) bool { return hypotheses.Hypotheses[i].ID < hypotheses.Hypotheses[j].ID })
	for index, value := range hypotheses.Hypotheses {
		if string(value.Status) == "invalidated" {
			continue
		}
		out.CandidateCount++
		switch string(value.Status) {
		case "supported":
			out.SupportedCount++
		case "weakened":
			out.WeakenedCount++
		case "contradicted":
			out.ContradictedCount++
		case "insufficient_information":
			out.InsufficientCount++
		}
		if string(value.Status) == "insufficient_information" {
			continue
		}
		out.Alternatives = append(out.Alternatives, HypothesisAlternative{ID: string(value.ID), Kind: string(value.Kind), Status: string(value.Status), PlausibilityPermille: clamp(value.PlausibilityPermille), CoveragePermille: clamp(value.CoveragePermille), Rank: index + 1})
		if value.ID == set.LeadingHypothesisID {
			out.LeadingHypothesisKind = string(value.Kind)
			out.LeadingPlausibilityPermille = clamp(value.PlausibilityPermille)
			out.LeadingCoveragePermille = clamp(value.CoveragePermille)
		}
	}
	if out.CandidateCount > 1 && out.LeadingHypothesisID == "" {
		out.Ambiguous = true
	}
	if len(out.Alternatives) > policy.MaxHypothesisAlternatives {
		out.Alternatives = out.Alternatives[:policy.MaxHypothesisAlternatives]
	}
	return out
}

func summarizeEvidence(state durableworkflow.EpisodeWorkflowState, policy Policy) EvidenceSummary {
	out := EvidenceSummary{}
	if state.Discrimination == nil {
		return out
	}
	out.Available = true
	out.CandidateCount = len(state.Discrimination.Candidates)
	out.Ambiguous = state.Discrimination.AmbiguityRelevant
	for _, value := range state.Discrimination.Candidates {
		if len(value.Discriminates) > 0 {
			out.DiscriminatingCandidateCount++
		}
		if value.RedundancyPermille > 0 {
			out.RedundantCandidateCount++
		}
		for _, code := range value.RequiredFactCodes {
			if len(out.MissingRequirementCodes) < policy.MaxReasonCodes {
				out.MissingRequirementCodes = append(out.MissingRequirementCodes, string(code))
			}
		}
	}
	if state.Discrimination.BestCandidateID != "" {
		out.BestCandidateID = string(state.Discrimination.BestCandidateID)
		for _, value := range state.Discrimination.Candidates {
			if value.ID == state.Discrimination.BestCandidateID {
				out.BestCandidateKind = string(value.Kind)
				out.BestUtilityPermille = clamp(value.UtilityPermille)
				out.BestDiscriminationPermille = clamp(value.DiscriminationPermille)
				out.BestCoverageGainPermille = clamp(value.CoverageGainPermille)
				break
			}
		}
	}
	out.MissingRequirementCodes = minReasonCodes(out.MissingRequirementCodes, policy.MaxReasonCodes)
	return out
}

func summarizeAdvisory(state durableworkflow.EpisodeWorkflowState, policy Policy) AdvisorySummary {
	out := AdvisorySummary{Available: state.Freshness[durableworkflow.LayerAdvisoryRequests] != durableworkflow.FreshnessAbsent}
	var preferredRank = int(^uint(0) >> 1)
	for _, value := range state.AdvisoryRequests {
		out.Total++
		switch string(value.Status) {
		case "proposed":
			out.Proposed++
			out.Active++
		case "acknowledged":
			out.Acknowledged++
			out.Active++
		case "deferred":
			out.Deferred++
			out.Active++
		case "suppressed":
			out.Suppressed++
		case "cancelled":
			out.Cancelled++
		case "satisfied":
			out.Satisfied++
		case "expired":
			out.Expired++
		case "invalidated":
			out.Invalidated++
		}
		if value.Flags.RequiresExternalMapping {
			out.ExternalMappingRequired = true
		}
		if value.Flags.RequiresExternalAuthorization {
			out.ExternalAuthorizationRequired = true
		}
		if out.Active > 0 && value.AdvisoryRank < preferredRank {
			preferredRank = value.AdvisoryRank
			out.PreferredRequestID = string(value.ID)
			out.PreferredCandidateKind = string(value.Kind)
		}
	}
	_ = policy
	return out
}

func summarizeCapability(state durableworkflow.EpisodeWorkflowState, depth ExpectedPipelineDepth) CapabilitySummary {
	out := CapabilitySummary{Configured: expectedLayer(depth, durableworkflow.LayerCapabilityMapping)}
	for _, assessment := range state.CapabilityMappings {
		out.AssessmentCount++
		if assessment.MappingAvailable {
			out.Available = true
		}
		if assessment.MappingAmbiguous {
			out.Ambiguous = true
		}
		if assessment.CapabilityUnavailable {
			out.Unavailable = true
		}
		for _, candidate := range assessment.Candidates {
			if candidate.Compatible {
				out.CompatibleCount++
			}
			if string(candidate.Status) == "compatible_degraded" {
				out.DegradedCount++
			}
			if string(candidate.Status) == "incompatible" {
				out.IncompatibleCount++
			}
		}
		if assessment.PreferredCandidateID != "" && out.PreferredMappingID == "" {
			out.PreferredMappingID = assessment.PreferredCandidateID
			for _, candidate := range assessment.Candidates {
				if candidate.ID == assessment.PreferredCandidateID {
					out.PreferredCapabilityKind = string(candidate.CapabilityKind)
					break
				}
			}
		}
	}
	return out
}

func summarizeAuthorization(state durableworkflow.EpisodeWorkflowState, depth ExpectedPipelineDepth) AuthorizationSummary {
	out := AuthorizationSummary{Configured: expectedLayer(depth, durableworkflow.LayerAuthorizationBoundary)}
	for _, assessment := range state.AuthorizationAssessments {
		out.AssessmentCount++
		out.EligibleCandidateCount += assessment.EligibleCandidateCount
		out.DeniedCandidateCount += assessment.DeniedCandidateCount
		out.ConfirmationRequiredCount += assessment.ConfirmationRequiredCount
		for _, candidate := range assessment.Candidates {
			switch string(candidate.Status) {
			case "deferred":
				out.DeferredCount++
			case "denied_by_default":
				out.DefaultDeniedCount++
			}
		}
		out.AuthorizationEligible = out.AuthorizationEligible || assessment.AuthorizationEligible
		out.AuthorizationAmbiguous = out.AuthorizationAmbiguous || assessment.AuthorizationAmbiguous
		out.ExternalConfirmationRequired = out.ExternalConfirmationRequired || assessment.ExternalConfirmationRequired
		out.DeniedByDefault = out.DeniedByDefault || assessment.DeniedByDefault
		if assessment.PreferredEligibleCandidateID != "" && out.PreferredEligibleCandidateID == "" {
			out.PreferredEligibleCandidateID = assessment.PreferredEligibleCandidateID
		}
	}
	return out
}

func determinePhase(s CognitiveSituation, depth ExpectedPipelineDepth, policy Policy) CognitivePhase {
	for _, item := range s.Knowledge.LayerStates {
		if item.Expected && item.Freshness == durableworkflow.FreshnessInvalidated {
			return PhaseInvalidated
		}
	}
	for _, item := range s.Knowledge.LayerStates {
		if item.Expected && item.Freshness == durableworkflow.FreshnessStale {
			return PhaseStale
		}
	}
	if s.Knowledge.AbsentExpectedLayers > 0 {
		return PhaseIncomplete
	}
	if s.Advisory.Active > 0 {
		return PhaseAwaitingEvidence
	}
	if s.Capability.Configured && s.Capability.Unavailable {
		return PhaseCapabilityUnavailable
	}
	if s.Authorization.Configured && (s.Authorization.ExternalConfirmationRequired || s.Authorization.DeferredCount > 0 || s.Authorization.DeniedByDefault) && !s.Authorization.AuthorizationEligible {
		return PhaseAuthorizationConstrained
	}
	if s.Hypotheses.Ambiguous {
		return PhaseAmbiguous
	}
	if s.Hypotheses.LeadingHypothesisID != "" && s.Hypotheses.LeadingCoveragePermille >= policy.MinLeadingCoveragePermille && s.Hypotheses.LeadingMarginPermille >= policy.MinLeadingMarginPermille {
		return PhaseCoherent
	}
	if s.Hypotheses.Available {
		return PhaseBuilding
	}
	return PhaseObserving
}

func determineReadiness(s CognitiveSituation, depth ExpectedPipelineDepth, policy Policy) RecommendationReadiness {
	out := RecommendationReadiness{Status: ReadinessNotReady}
	for _, layer := range durableworkflow.Layers() {
		if !expectedLayer(depth, layer) {
			continue
		}
		out.RequiredFreshLayers = append(out.RequiredFreshLayers, layer)
		for _, state := range s.Knowledge.LayerStates {
			if state.Layer == layer && state.Freshness != durableworkflow.FreshnessFresh {
				out.MissingFreshLayers = append(out.MissingFreshLayers, layer)
			}
		}
	}
	if len(out.MissingFreshLayers) > 0 {
		if s.Phase == PhaseStale {
			out.Status = ReadinessBlockedStaleness
		} else if s.Phase == PhaseInvalidated {
			out.Status = ReadinessBlockedInvalidState
		} else {
			out.Status = ReadinessBlockedInsufficientInformation
		}
		addReason(&out.BlockingReasonCodes, "required_layer_not_fresh", policy.MaxReasonCodes)
		out.Fingerprint = readinessFingerprint(out)
		return out
	}
	if s.Phase == PhaseCapabilityUnavailable {
		out.Status = ReadinessBlockedMissingCapability
		addReason(&out.BlockingReasonCodes, "capability_unavailable", policy.MaxReasonCodes)
	} else if s.Phase == PhaseAuthorizationConstrained {
		out.Status = ReadinessBlockedAuthorization
		addReason(&out.BlockingReasonCodes, "authorization_constrained", policy.MaxReasonCodes)
	} else if s.Phase == PhaseAmbiguous && !policy.AllowAmbiguousRecommendationReadiness {
		out.Status = ReadinessBlockedAmbiguity
		addReason(&out.BlockingReasonCodes, "hypotheses_ambiguous", policy.MaxReasonCodes)
	} else if depthIndex(depth) >= depthIndex(DepthEvidenceDiscrimination) && (s.Phase == PhaseAmbiguous || s.Evidence.CandidateCount > 0 || s.Hypotheses.InsufficientCount > 0) {
		out.Status = ReadinessObservationRecommendation
		out.Ready = true
		addReason(&out.SupportingReasonCodes, "evidence_context_consolidated", policy.MaxReasonCodes)
		if depthIndex(depth) >= depthIndex(DepthAdvisoryRequests) && s.Advisory.Active > 0 {
			addReason(&out.SupportingReasonCodes, "active_advisory_request", policy.MaxReasonCodes)
		}
	} else if depthIndex(depth) >= depthIndex(DepthSituationHypotheses) && s.Phase == PhaseCoherent && s.Knowledge.OverallCoveragePermille >= policy.MinKnowledgeCoveragePermille {
		out.Status = ReadinessCognitiveRecommendation
		out.Ready = true
		addReason(&out.SupportingReasonCodes, "coherent_hypothesis", policy.MaxReasonCodes)
	} else {
		addReason(&out.BlockingReasonCodes, "insufficient_consolidated_state", policy.MaxReasonCodes)
	}
	out.Fingerprint = readinessFingerprint(out)
	return out
}

func shortID(value string) string {
	return strings.TrimPrefix(fingerprint("cognitive-situation-id-v1:", value), "cognitive-situation-id-v1:")
}
